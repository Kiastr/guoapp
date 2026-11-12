package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const hongguoAppBaseURL = "https://api5-normal-sinfonlineb.fqnovel.com"

const hongguoAppUserAgent = "com.phoenix.read/73532 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3; Cronet/TTNetVersion:04657795 2026-01-23 QuicVersion:c67e9834 2025-09-08)"

// hongguoAnonIdentityKey 在 context 里携带取流专用的匿名注册身份。
//
// 【为什么用 context 而不是改函数签名】hongguoAppRequest 是目录/详情/搜索/弹幕/
// 取流共用的核心函数，加参数会波及十余处调用；而「用注册身份」只对取流线路成立
// （主页接口恰恰相反：实测用本地随机身份，病灶在 branch 不在身份）。
// 用 context 值可以只让取流链路生效，其余路径零改动。
type hongguoAnonIdentityKey struct{}

// hongguoPlaybackSignKey 标记「本次 hongguoAppRequest 是播放模型请求」。
//
// 【为什么单独加一个 key】hongguoAppRequest 被目录/详情/搜索/弹幕/取流共用，
// 其中只有播放模型接口从 2026-11-11 起校验真 551 签名，其余接口仍用原有签名
// （上游 6.9 明确「只调整模型签名族；搜索/目录/账号签名保留」）。
// 已有 hongguoAnonIdentityKey 表达的是「注入注册身份」，语义不同，不复用它来
// 判断签名方式，避免以后给别的链路也注入身份时误换签名。
type hongguoPlaybackSignKey struct{}

type hongguoCatalogCursor struct {
	Offset        int       `json:"offset"`
	SessionID     string    `json:"sessionId,omitempty"`
	LastID        string    `json:"lastId,omitempty"`
	PageSignature string    `json:"pageSignature,omitempty"`
	Initialized   bool      `json:"initialized"`
	Exhausted     bool      `json:"exhausted"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type hongguoCatalogState struct {
	Version   int                             `json:"version"`
	DeviceID  string                          `json:"deviceId"`
	InstallID string                          `json:"installId"`
	Feeds     map[string]hongguoCatalogCursor `json:"feeds"`
}

type hongguoAppClient struct {
	mu             sync.Mutex
	catalogMu      sync.Mutex
	baseURL        string
	state          hongguoCatalogState
	details        map[string]hongguoDetailEntry
	pending        map[string]*hongguoDetailCall
	searches       map[string]hongguoSearchEntry
	searchPending  map[string]*hongguoSearchCall
	suggestions    map[string]hongguoSuggestionEntry
	suggestPending map[string]*hongguoSuggestionCall
	danmaku        map[string]hongguoDanmakuCacheEntry
	danmakuPending map[string]*hongguoDanmakuCall
}

func (downloader *Downloader) hongguoClient() *hongguoAppClient {
	downloader.hongguoOnce.Do(func() {
		downloader.hongguo = &hongguoAppClient{
			baseURL: hongguoAppBaseURL,
			state:   hongguoCatalogState{Version: 1, DeviceID: newHongguoDeviceID(), InstallID: newHongguoDeviceID(), Feeds: map[string]hongguoCatalogCursor{}},
			details: map[string]hongguoDetailEntry{}, pending: map[string]*hongguoDetailCall{},
			searches: map[string]hongguoSearchEntry{}, searchPending: map[string]*hongguoSearchCall{},
			suggestions: map[string]hongguoSuggestionEntry{}, suggestPending: map[string]*hongguoSuggestionCall{},
		}
	})
	return downloader.hongguo
}

func cloneHongguoCatalogState(state *hongguoCatalogState) *hongguoCatalogState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.Feeds = make(map[string]hongguoCatalogCursor, len(state.Feeds))
	for name, cursor := range state.Feeds {
		cloned.Feeds[name] = cursor
	}
	return &cloned
}

func (downloader *Downloader) restoreHongguoCatalog(state *hongguoCatalogState) {
	if state == nil || state.Version != 1 || !hongguoNumericID.MatchString(state.DeviceID) || !hongguoNumericID.MatchString(state.InstallID) {
		return
	}
	cloned := cloneHongguoCatalogState(state)
	for name, cursor := range cloned.Feeds {
		known := false
		for _, genre := range hongguoAppGenres {
			known = known || name == genre.key || name == "category:"+genre.key
		}
		if !known || !cursor.Initialized || cursor.Offset < 0 || cursor.Offset > 1_000_000 || len(cursor.SessionID) > 4096 || len(cursor.LastID) > 256 || len(cursor.PageSignature) > 64 {
			delete(cloned.Feeds, name)
		}
	}
	client := downloader.hongguoClient()
	client.catalogMu.Lock()
	defer client.catalogMu.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.state.Feeds) == 0 {
		client.state = *cloned
	}
}

func (downloader *Downloader) hongguoCatalogSnapshot() *hongguoCatalogState {
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	return cloneHongguoCatalogState(&client.state)
}

func hongguoCatalogInitialized(state *hongguoCatalogState) bool {
	if state != nil && state.Version == 1 {
		for _, cursor := range state.Feeds {
			if cursor.Initialized {
				return true
			}
		}
	}
	return false
}

func hongguoCatalogHasMore(state *hongguoCatalogState) bool {
	for _, genre := range hongguoAppGenres {
		if state == nil || !state.Feeds[genre.key].Exhausted {
			return true
		}
	}
	return false
}

func (downloader *Downloader) hongguoAppRequest(ctx context.Context, method, path string, extra url.Values, payload any) (map[string]any, error) {
	client := downloader.hongguoClient()
	client.mu.Lock()
	base, deviceID, installID := client.baseURL, client.state.DeviceID, client.state.InstallID
	client.mu.Unlock()
	// 取流链路经 context 注入注册身份：覆盖掉本地随机 device_id/iid。
	if identity, ok := ctx.Value(hongguoAnonIdentityKey{}).(hongguoAnonDevice); ok {
		deviceID, installID = identity.DeviceID, identity.InstallID
	}
	query := url.Values{
		"aid": {"8662"}, "app_name": {"novelread"}, "version_code": {"73532"}, "version_name": {"7.3.5.32"},
		"manifest_version_code": {"73532"}, "update_version_code": {"73532"}, "channel": {"update_64"},
		"device_platform": {"android"}, "os": {"android"}, "ssmix": {"a"}, "device_type": {"25053RT47C"},
		"device_brand": {"Redmi"}, "language": {"zh"}, "os_api": {"36"}, "os_version": {"16"},
		"resolution": {"1280*2772"}, "dpi": {"520"}, "ac": {"wifi"}, "device_id": {deviceID}, "iid": {installID},
	}
	for key, values := range extra {
		query[key] = append([]string(nil), values...)
	}
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	attempts := downloader.cfg.Retries
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 3 {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("User-Agent", hongguoAppUserAgent)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-XS-From-Web", "0")
		request.Header.Set("Sdk-Version", "2")
		comment, _ := ctx.Value(hongguoCommentKey{}).(bool)
		var commentNonce hongguoCommentNonce
		if comment {
			commentNonce, err = newHongguoCommentNonce()
			if err != nil {
				return nil, errors.New("无法准备红果文字请求")
			}
			request.Header.Set("Comment-Source", "601")
			request.Header.Set("Server-Channel", "1000")
		}
		if payload != nil {
			request.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		// 播放模型请求专用标记：由 hongguoAppVideoModelAt 经 context 注入。
		// 只有它换成真 551 签名，目录/详情/搜索仍走 signHongguoRequest。
		playback, _ := ctx.Value(hongguoPlaybackSignKey{}).(bool)
		response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, func(prepared *http.Request) {
			now := time.Now()
			if comment {
				query.Set("_rticket", strconv.FormatInt(now.UnixMilli(), 10))
				prepared.URL.RawQuery = query.Encode()
				signHongguoCommentRequest(prepared, commentNonce, now)
			} else {
				// 【branch 抖动】服务端按 hashF13 把请求分到三个桶，**只有 branch 0
				// 返回数据**，1 和 2 一律 HTTP 200 空 body。这是「主页接口也返回空」
				// 的直接原因，与签名、UA、设备画像都无关（这些变量单独替换均不影响
				// 结果，唯有「不筛 branch」能让空响应复现）。
				//
				// 抖的是 _rticket：它同时进 query 与 x-ss-req-ticket，服务端按最终
				// URL 分桶，因此必须在签名**之前**定稿。khronos 取与签名同源的时间
				// 值（signHongguoRequest 内部用 now.Unix()）。
				khronos := uint32(now.Unix())
				ticket := now.UnixMilli()
				for offset := int64(0); offset < hongguoBranchAttempts; offset++ {
					query.Set("_rticket", strconv.FormatInt(ticket+offset, 10))
					prepared.URL.RawQuery = query.Encode()
					if hgDrpyBranchOf(prepared.URL.String(), body, khronos) == 0 {
						break
					}
				}
				if playback && hongguoPlaybackRealSignature {
					// 【真签名接入（+186）】branch 抖动后最终 query 已定稿，按这份
					// RawQuery 生成 551 Argus/Ladon 真头；x-khronos 取 _rticket/1000
					// 与 URL 同源（上游明确要求，否则签名与 query 对不上）。
					// 重试/换号都会重新进入本回调，签名随之重算，不会复用旧串。
					finalTicket := hongguoPlaybackTicketFromURL(prepared.URL.String())
					hongguoPlaybackApplyHeaders(prepared, body, finalTicket/1000, finalTicket)
				} else {
					signHongguoRequest(prepared, body, now)
				}
			}
		})
		if err != nil {
			lastErr = err
			var backoff *requestBackoff
			if errors.As(err, &backoff) || ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("红果 App 接口 HTTP %d", response.StatusCode)
			if response.StatusCode >= 400 && response.StatusCode < 500 {
				return nil, lastErr
			}
			continue
		}
		if len(content) == 0 {
			// 空 body 是设备级风控的匿名拒绝信号，交给 hongguoAnonPlayback 换号重试；
			// 与「响应格式异常」区分开，否则会被归类成 parse 故障拿 30 分钟冷却。
			return nil, errHongguoEmptyBody
		}
		if len(content) > providerMaxBodyBytes {
			return nil, errors.New("红果 App 接口未返回有效数据")
		}
		var result map[string]any
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil || result == nil {
			return nil, errors.New("红果 App 接口返回格式异常")
		}
		code := firstNonEmpty(mapString(result, "code", "Code", "status_code"), mapString(nestedMap(result, "BaseResp"), "StatusCode"))
		if code != "" && code != "0" {
			return nil, fmt.Errorf("红果 App 接口暂不可用（%s）", truncate(code, 20))
		}
		return result, nil
	}
	return nil, lastErr
}

// hongguoAppVideoModelAt 用指定设备身份请求取流端点。
//
// 身份经 context 注入 hongguoAppRequest：把 query 里的本地随机 device_id/iid 换成
// device_register 得到的匿名身份（服务端对取流端点校验设备身份，随机值必空）。
// path 参数化只为给将来多端点留口子，当前只有 hongguoAppPlaybackPath 一个取值。
func (downloader *Downloader) hongguoAppVideoModelAt(ctx context.Context, path string, payload any, identity hongguoAnonDevice) (map[string]any, error) {
	scoped := context.WithValue(ctx, hongguoAnonIdentityKey{}, identity)
	// 让共用的 hongguoAppRequest 知道这是播放模型请求，从而换用真 551 签名。
	scoped = context.WithValue(scoped, hongguoPlaybackSignKey{}, true)
	return downloader.hongguoAppRequest(scoped, http.MethodPost, path, nil, payload)
}
