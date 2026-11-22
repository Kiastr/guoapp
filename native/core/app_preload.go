package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type nativePreloadKey struct{}

const nativePreloadLimit = 4 << 20

// nativePreloadBudget 预加载的时间预算（两条线路共享，不因切线路而翻倍）。
// [+204] 6s → 10s：给需代理访问的慢源留出两次探测的余量。
const nativePreloadBudget = 10 * time.Second

// nativePreloadFirstByteLimit 首字节（首个分片/播放列表）超过这个时长即判定
// 「该线路慢/不通」，切下一条再试。判定放在预加载窗口里，是为了让试错不占用
// 播放窗口——切线路本身是本地操作（nativeNextPlayback 只递增 index），
// 不产生新的解析请求。
// [+204] 1.5s → 3s：黄豆等源在代理/慢网络下首字节常超 1.5s，原阈值把所有线路
// 都判成 unverified，导致播放层 watchdog 走最短档（3s）在首帧前就误杀慢线。
// 放宽到 3s 后，正常慢线能被判为 verified，播放层改用更长的首帧等待。
const nativePreloadFirstByteLimit = 3 * time.Second

// nativePrefetchResult 一次预取的结果。
type nativePrefetchResult struct {
	Bytes       int64
	FirstByteMS int64
	OK          bool
}

func (engine *nativeEngine) nativePreload(ctx context.Context, input nativeInput) (nativePlan, error) {
	ctx = context.WithValue(ctx, backgroundCatalogKey{}, true)
	ctx = context.WithValue(ctx, nativePreloadKey{}, true)
	plan, err := engine.nativeResolve(ctx, input)
	if err != nil || plan.Local {
		return plan, err
	}
	engine.mu.Lock()
	choice, exists := engine.playbacks[plan.Session]
	stream := engine.stream
	engine.mu.Unlock()
	if exists && stream != nil && choice.streamSession != "" {
		deadline := time.Now().Add(nativePreloadBudget)
		plan, _ = engine.nativePreloadWarm(ctx, stream, plan, choice, input.Quality, deadline)
	}
	if err := ctx.Err(); err != nil {
		engine.nativeReleasePlayback(plan.Session)
		return nativePlan{}, err
	}
	return plan, nil
}

// nativePreloadWarm 预取并判断线路可用性。
//
// 【判据分两级，只有「完全取不到数据」才算失败】
//   - result.OK（取到了字节）→ 线路可用，只是首字节可能慢；标记 Verified 与否
//     只影响播放层首帧看门狗的档位（10s / 6s），不影响线路取舍。
//   - !result.OK（一个字节都没取到）→ 真失败：回流健康度，并本地切下一条线再探。
//
// 【为什么不把「慢」当失败】预加载的首字节阈值（3s）比正式播放严格得多，
// 若按它记账，连续几集预加载都慢就会把 drpys 累计到 3 次熔断 3 分钟，
// 正式播放被迫走兜底线——预加载反而把好线记死。慢线的最终判决应交给
// 播放层的首帧看门狗，预加载不越权。
//
// 【顺带减少请求】慢线不再触发第二次探活，说明预加载通常在 1 次请求内收敛；
// 只有完全失败才会换线再探一次。全程串行、共享同一时间预算。
func (engine *nativeEngine) nativePreloadWarm(
	ctx context.Context,
	stream *nativeStreamServer,
	plan nativePlan,
	choice nativePlaybackChoice,
	quality int,
	deadline time.Time,
) (nativePlan, nativePlaybackChoice) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return plan, choice
	}
	warming, cancel := context.WithTimeout(ctx, remaining)
	result := stream.nativePrefetch(warming, choice.streamSession, plan.URL, quality)
	cancel()
	plan.PrefetchedBytes = result.Bytes
	plan.Verified = result.OK && result.FirstByteMS > 0 && result.FirstByteMS <= nativePreloadFirstByteLimit.Milliseconds()
	// 【回流】只回流「一个字节都没取到」的失败。取到数据但首字节慢（>3s）
	// 不记账：慢线仍然能播，只是首帧来得晚；把它记成 network 失败会让
	// 连续几集预加载把整条线路熔断，正式播放也被迫改走兜底。
	if !result.OK && choice.route != "" {
		hongguoRoutesHealth.noteFail(choice.route, time.Now(), hongguoFailNetwork)
	}
	// 首探已取到数据就到此为止：慢线也已把数据写进会话缓存，再探一条
	// 只会多一次请求（加重风控），换来的只是「可能更快」的不确定收益。
	if result.OK || choice.index+1 >= len(choice.media) {
		return plan, choice
	}
	if time.Until(deadline) <= 0 {
		return plan, choice
	}
	// 下一条线路：本地切换，零解析请求。
	// 【为什么原会话要等到确认 next 可用后才释放】提前释放会让「两条线都取不到
	// 数据」的分支返回一个已失效的本地代理地址——Dart 侧照样 open 它，结果首帧
	// 等到看门狗超时才切线路，比老实重新解析慢得多。先留着它，失败时才有退路。
	previousSession := plan.Session
	next, err := engine.nativeNextPlayback(ctx, previousSession)
	if err != nil {
		return plan, choice
	}
	engine.mu.Lock()
	nextChoice, exists := engine.playbacks[next.Session]
	engine.mu.Unlock()
	if !exists || nextChoice.streamSession == "" {
		engine.nativeReleasePlayback(next.Session)
		return plan, choice
	}
	warming, cancel = context.WithTimeout(ctx, time.Until(deadline))
	nextResult := stream.nativePrefetch(warming, nextChoice.streamSession, next.URL, quality)
	cancel()
	next.PrefetchedBytes = nextResult.Bytes
	next.Verified = nextResult.OK && nextResult.FirstByteMS > 0 && nextResult.FirstByteMS <= nativePreloadFirstByteLimit.Milliseconds()
	if !nextResult.OK {
		// 两条线都一个字节没取到：保留原线路。原地址来自一次成功的解析，
		// 比「同样取不到数据的备选」更可信，且它的会话刚刚才被验证仍然有效。
		engine.nativeReleasePlayback(next.Session)
		return plan, choice
	}
	engine.nativeReleasePlayback(previousSession)
	return next, nextChoice
}

func (stream *nativeStreamServer) nativePrefetch(ctx context.Context, token, entry string, quality int) nativePrefetchResult {
	stream.mu.Lock()
	session := stream.sessions[token]
	stream.mu.Unlock()
	if session == nil {
		return nativePrefetchResult{}
	}
	ctx, cancel := context.WithCancel(providerMediaContext(ctx, session.credentials))
	defer cancel()
	stop := context.AfterFunc(session.ctx, cancel)
	defer stop()
	remaining, requests := int64(nativePreloadLimit), 0
	visited := map[string]bool{}
	started := time.Now()
	var firstByte time.Duration
	var warm func(string, int) bool
	warm = func(local string, depth int) bool {
		if ctx.Err() != nil || remaining <= 0 || requests >= 8 || depth > 2 || visited[local] {
			return false
		}
		visited[local] = true
		parsed, err := url.Parse(local)
		if err != nil || !strings.HasPrefix(local, stream.address+"/"+token+"/") {
			return false
		}
		id := strings.TrimPrefix(parsed.Path, "/"+token+"/")
		session.mu.Lock()
		asset, exists := session.assets[id]
		session.mu.Unlock()
		if !exists {
			return false
		}
		playlist := strings.Contains(asset.contentType, "mpegurl")
		if len(asset.data) == 0 {
			requests++
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.address, nil)
			if err != nil {
				return false
			}
			request.Header.Set("Referer", session.referer)
			if !playlist {
				request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", remaining-1))
			}
			response, err := stream.downloader.doMediaRequest(request)
			if err != nil {
				return false
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, remaining+1))
			response.Body.Close()
			if readErr != nil || len(body) == 0 || int64(len(body)) > remaining ||
				(response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent) {
				return false
			}
			if response.Request != nil && response.Request.URL != nil {
				asset.address = response.Request.URL.String()
			}
			asset.contentType = response.Header.Get("Content-Type")
			asset.etag, asset.modified = response.Header.Get("ETag"), response.Header.Get("Last-Modified")
			playlist = playlist || strings.Contains(strings.ToLower(asset.contentType), "mpegurl") || bytes.HasPrefix(bytes.TrimSpace(body), []byte("#EXTM3U"))
			asset.data = body
			asset.total = int64(len(body))
			if response.StatusCode == http.StatusPartialContent {
				start, end, total, valid := nativeContentRange(response.Header.Get("Content-Range"))
				if !valid || start != 0 || end+1 != int64(len(body)) {
					return false
				}
				asset.total = total
			}
			if asset.total > int64(len(body)) && asset.modified == "" && (asset.etag == "" || strings.HasPrefix(asset.etag, "W/")) {
				return false
			}
			if playlist {
				asset.contentType = "application/vnd.apple.mpegurl"
			}
			remaining -= int64(len(body))
			if firstByte <= 0 {
				// 首个分片/播放列表到手的时间，作为该线路「快不快」的判据。
				firstByte = time.Since(started)
			}
			session.mu.Lock()
			session.assets[id] = asset
			session.mu.Unlock()
		}
		if !playlist {
			return true
		}
		lines, _, master := nativeHLSSelect(strings.Split(string(asset.data), "\n"), quality)
		rewritten, err := stream.nativeRewrite(token, session, strings.Join(lines, "\n"), asset.address)
		if err != nil {
			return false
		}
		segments := 0
		seconds := 0.0
		for _, raw := range strings.Split(rewritten, "\n") {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "#EXT-X-BYTERANGE:") {
				break
			}
			if strings.HasPrefix(line, "#EXTINF:") {
				value := strings.SplitN(strings.TrimPrefix(line, "#EXTINF:"), ",", 2)[0]
				duration, _ := strconv.ParseFloat(value, 64)
				seconds += duration
			}
			if strings.HasPrefix(line, "#EXT-X-KEY:") || strings.HasPrefix(line, "#EXT-X-MAP:") {
				for _, match := range nativePlaylistURI.FindAllStringSubmatch(line, -1) {
					if !strings.HasPrefix(match[1], "data:") && !warm(match[1], depth+1) {
						return false
					}
				}
			}
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if !warm(line, depth+1) {
				break
			}
			segments++
			if master || segments >= 2 || seconds >= 8 {
				break
			}
		}
		return true
	}
	warm(entry, 0)
	fetched := nativePreloadLimit - remaining
	// 向上取整到毫秒：本机代理下首字节可能不足 1ms，取 0 会被误判成"没测到"。
	firstMS := int64((firstByte + time.Millisecond - 1) / time.Millisecond)
	return nativePrefetchResult{
		Bytes:       fetched,
		FirstByteMS: firstMS,
		OK:          fetched > 0,
	}
}

var nativeContentRangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

func nativeContentRange(value string) (int64, int64, int64, bool) {
	match := nativeContentRangePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, 0, 0, false
	}
	start, first := strconv.ParseInt(match[1], 10, 64)
	end, second := strconv.ParseInt(match[2], 10, 64)
	total, third := strconv.ParseInt(match[3], 10, 64)
	return start, end, total, first == nil && second == nil && third == nil && start >= 0 && end >= start && total > end
}

func (stream *nativeStreamServer) nativeServePrefix(ctx context.Context, writer http.ResponseWriter, request *http.Request, session *nativeStreamSession, asset nativeStreamAsset) bool {
	start, end := int64(0), asset.total-1
	ranged := request.Header.Get("Range") != ""
	if ranged {
		value := request.Header.Get("Range")
		if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
			return false
		}
		parts := strings.SplitN(strings.TrimPrefix(value, "bytes="), "-", 2)
		if len(parts) != 2 {
			return false
		}
		var err error
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || start < 0 || start >= int64(len(asset.data)) {
			return false
		}
		if parts[1] != "" {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil || end < start {
				return false
			}
			end = min(end, asset.total-1)
		}
	}
	validator := asset.etag
	if validator == "" || strings.HasPrefix(validator, "W/") {
		validator = asset.modified
	}
	if value := request.Header.Get("If-Range"); value != "" && value != validator {
		return false
	}
	var response *http.Response
	if end >= int64(len(asset.data)) && request.Method != http.MethodHead {
		upstream, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.address, nil)
		if err != nil || validator == "" {
			return false
		}
		upstream.Header.Set("Referer", session.referer)
		upstream.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", len(asset.data), end))
		upstream.Header.Set("If-Range", validator)
		response, err = stream.nativeRequest(upstream)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		a, b, total, valid := nativeContentRange(response.Header.Get("Content-Range"))
		if response.StatusCode != http.StatusPartialContent || !valid || a != int64(len(asset.data)) || b != end || total != asset.total {
			session.invalidate(asset.address)
			return false
		}
		if validator == asset.etag && response.Header.Get("ETag") != validator ||
			validator == asset.modified && response.Header.Get("Last-Modified") != validator {
			session.invalidate(asset.address)
			return false
		}
	}
	writer.Header().Set("Content-Type", asset.contentType)
	writer.Header().Set("Accept-Ranges", "bytes")
	writer.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if asset.etag != "" {
		writer.Header().Set("ETag", asset.etag)
	}
	if asset.modified != "" {
		writer.Header().Set("Last-Modified", asset.modified)
	}
	if ranged {
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, asset.total))
		writer.WriteHeader(http.StatusPartialContent)
	}
	if request.Method == http.MethodHead {
		return true
	}
	_, err := writer.Write(asset.data[start:min(end+1, int64(len(asset.data)))])
	if err == nil && response != nil {
		_, _ = io.CopyN(writer, response.Body, end-int64(len(asset.data))+1)
	}
	return true
}

func (session *nativeStreamSession) invalidate(address string) {
	session.mu.Lock()
	defer session.mu.Unlock()
	for id, asset := range session.assets {
		if asset.address == address {
			asset.data, asset.total, asset.etag, asset.modified = nil, 0, "", ""
			session.assets[id] = asset
		}
	}
}
