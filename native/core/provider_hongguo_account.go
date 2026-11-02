package core

// 红果账号数据同步（收藏 + 观看记录）。
//
// 与 provider_hongguo_web_login.go 的分工：那边只负责扫码并把 Cookie 落盘，
// 这边才真正把 Cookie 送进账号接口——没有这个文件，登录态就只是个摆设。
//
// 协议来源：tsukiyo-zero/hongguoTV 的 CloudAccount.kt（GPL-3.0-only），
// 上游又参考 charlie901208/HGTV feat/phone-login 的公开请求模型。三处与内容
// 接口不同，改之前先看：
//
//  1. **主机是 api5-normal-sinfonlinea**，不是内容主站 sinfonlineb。账号数据
//     端点单独一台，打到内容主机会 404。
//  2. **读写版本号不同**：读 72232 / 7.2.2.32，写 73932 / 7.3.9.32。写接口
//     版本号低了会被拒，这个差别在别的接口上不存在。
//  3. **签名要全套**（gorgon / ladon / argus / helios / medusa）。内容链路
//     只送四头也能过，账号链路只送四头拿的是空 body——别照
//     signHongguoRequest 的做法简化。
//
// 收藏接口用 bookshelf/video/info/v1（不是 list/v）：上游与 HGTV 的公开实现
// 都是 info/v1，list/v 的写法没有出处。分页契约上游也没验证，这里只取首批。

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// hongguoAccountBaseURL 账号数据端点主机。
	hongguoAccountBaseURL = "https://api5-normal-sinfonlinea.fqnovel.com"

	// 读请求画像（无 body）。
	hongguoAccountReadVersionCode = "72232"
	hongguoAccountReadVersionName = "7.2.2.32"

	// 写请求画像（有 body）。版本号必须更高，否则书架写接口拒绝。
	hongguoAccountWriteVersionCode = "73932"
	hongguoAccountWriteVersionName = "7.3.9.32"

	// UA 按「有没有 body」选，与上游 Signer 一致：无 body 用普通 App UA，
	// 有 body 用播放器 UA（带 Cronet/TTNet 段）。
	hongguoAccountAppUA   = "com.phoenix.read/72232 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3)"
	hongguoAccountVideoUA = "com.phoenix.read/71332 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3; Cronet/TTNetVersion:04657795 2026-01-23 QuicVersion:c67e9834 2025-09-08)"

	// 单次同步的上限。收藏与历史都只取首批——分页契约未验证，宁可少而准。
	hongguoAccountMaxFavorites = 200
	hongguoAccountMaxHistory   = 50
)

// hongguoAccountDevice 账号请求的设备身份。
//
// 必须是 device_register 注册过的身份：随机合成的 device_id/iid 会被判成
// 空 body，cdid 缺失则三件套对不上。
type hongguoAccountDevice struct {
	DeviceID  string
	InstallID string
	CDID      string
}

func (device hongguoAccountDevice) valid() bool {
	return hongguoNumericID.MatchString(device.DeviceID) &&
		hongguoNumericID.MatchString(device.InstallID) &&
		len(strings.TrimSpace(device.CDID)) >= 8
}

// hongguoAccountItem 归一化后的收藏 / 历史条目。
//
// 收藏只填前六个字段，历史额外带集号与进度。Dart 侧按 SeriesID 建 Drama，
// 所以系列 id 必须是红果自己的数字 id（与官网 detail 路径同一套）。
type hongguoAccountItem struct {
	SeriesID    string  `json:"seriesId"`
	Title       string  `json:"title"`
	Cover       string  `json:"cover,omitempty"`
	Description string  `json:"description,omitempty"`
	Episodes    int     `json:"episodes,omitempty"`
	Category    string  `json:"category,omitempty"`
	Episode     int     `json:"episode,omitempty"`
	EpisodeID   string  `json:"episodeId,omitempty"`
	Position    float64 `json:"position,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
	UpdatedAt   int64   `json:"updatedAt,omitempty"`
	CollectedAt int64   `json:"collectedAt,omitempty"`
}

// hongguoAccountSnapshot 一次同步的完整结果。
//
// 收藏与历史分开报错：一边失败不该把另一边也丢掉，Warning 说明是哪边没拿到。
type hongguoAccountSnapshot struct {
	SignedIn  bool                 `json:"signedIn"`
	UserID    string               `json:"userId,omitempty"`
	Nickname  string               `json:"nickname,omitempty"`
	Favorites []hongguoAccountItem `json:"favorites"`
	History   []hongguoAccountItem `json:"history"`
	SyncedAt  int64                `json:"syncedAt,omitempty"`
	Warning   string               `json:"warning,omitempty"`
}

// hongguoAccountCookieHeader 把落盘的 cookie map 拼成请求头。
//
// 顺序排序只为稳定（服务端不要求顺序，但同样的登录态每次产出同样的头，
// 排查时少一个变量）。
func hongguoAccountCookieHeader(cookies map[string]string) string {
	names := make([]string, 0, len(cookies))
	for name, value := range cookies {
		if strings.TrimSpace(name) != "" && strings.TrimSpace(value) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookies[name])
	}
	return strings.Join(parts, "; ")
}

// hongguoAccountQuery 拼账号请求的 query。
//
// 顺序必须固定：签名对 query 字节敏感，业务参数在前、设备画像居中、
// iid/device_id/cdid 与 ts/_rticket 收尾——与上游 spread 顺序一致。
func hongguoAccountQuery(extra [][2]string, device hongguoAccountDevice, versionCode, versionName string, ts, ticket int64) string {
	values := make([][2]string, 0, len(extra)+len(hgDrpyDeviceParams)+6)
	values = append(values, extra...)
	for _, pair := range hgDrpyDeviceParams {
		switch pair[0] {
		case "version_code", "manifest_version_code", "update_version_code", "pv_player":
			values = append(values, [2]string{pair[0], versionCode})
		case "version_name":
			values = append(values, [2]string{pair[0], versionName})
		default:
			values = append(values, pair)
		}
	}
	values = append(values,
		[2]string{"iid", device.InstallID},
		[2]string{"device_id", device.DeviceID},
		[2]string{"cdid", device.CDID},
		[2]string{"ts", strconv.FormatInt(ts, 10)},
		[2]string{"_rticket", strconv.FormatInt(ticket, 10)},
	)
	return hgDrpyPythonURLEncode(values)
}

// hongguoAccountSign 组装账号请求的完整签名头。
//
// 与 hgDrpySignedVideoRequestWithDevice 的差别只有三处：UA 按 body 切换、
// medusa 的版本号跟请求画像走、无 body 时不加 Content-Type / x-ss-stub /
// x-vc-bdturing-sdk-version。其余保持同源，方便与取流链路对拍。
func hongguoAccountSign(fullURL, query string, body []byte, now time.Time, device hongguoAccountDevice, versionName, userAgent string) map[string]string {
	khronos := uint32(now.Unix())
	random := uint32(0)
	var randomBuffer [2]byte
	if _, err := crand.Read(randomBuffer[:]); err == nil {
		random = uint32(binary.LittleEndian.Uint16(randomBuffer[:]))
	}
	ladonStamp := hgDrpyBe32(khronos)
	argusStamp := hgDrpyLe32(khronos)
	nonce, started := hgDrpyLoadProcess()
	headers := map[string]string{
		"User-Agent":           userAgent,
		"Accept":               "application/json; charset=utf-8,application/x-protobuf",
		"x-ss-req-ticket":      strconv.FormatInt(now.UnixMilli(), 10),
		"x-tt-request-tag":     "t=0;n=0",
		"sdk-version":          "2",
		"passport-sdk-version": "50561",
		"x-xs-from-web":        "0",
		"x-khronos":            strconv.FormatUint(uint64(khronos), 10),
		"x-ladon":              base64.StdEncoding.EncodeToString(ladonStamp[:]),
		"x-argus":              base64.StdEncoding.EncodeToString(argusStamp[:]),
		"x-gorgon":             hgDrpyXGorgon(query, body, khronos, random),
		"x-helios":             hgDrpyHelios(khronos, hgDrpyRandomU32()),
		"x-medusa":             base64.StdEncoding.EncodeToString(hgDrpyBuildMedusa(fullURL, body, khronos, device.DeviceID, versionName, nonce, started/1000, started, nil)),
		"x-tt-dt":              "",
	}
	if body != nil {
		headers["Content-Type"] = "application/json; charset=UTF-8"
		headers["x-vc-bdturing-sdk-version"] = "3.7.2.cn"
		headers["x-ss-stub"] = hgDrpyMd5Hex(body)
	}
	return headers
}

// hongguoAccountRequest 发一次账号请求。
//
// write=true 时用写画像与 POST；不自动重试——书架写接口重复提交会变成
// 「取消后再取消」，上游明确要求由调用方回读确认，不能靠重试兜底。
func (downloader *Downloader) hongguoAccountRequest(ctx context.Context, device hongguoAccountDevice, cookie, path string, extra [][2]string, payload any, write bool) (map[string]any, error) {
	base := hongguoAccountBaseURL
	if custom, ok := ctx.Value(hongguoAccountBaseKey{}).(string); ok && custom != "" {
		base = strings.TrimRight(custom, "/")
	}
	if !device.valid() {
		return nil, errors.New("账号设备身份无效，请重新登录")
	}
	var body []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = encoded
	}
	versionCode, versionName := hongguoAccountReadVersionCode, hongguoAccountReadVersionName
	userAgent := hongguoAccountAppUA
	if write || body != nil {
		versionCode, versionName = hongguoAccountWriteVersionCode, hongguoAccountWriteVersionName
		userAgent = hongguoAccountVideoUA
	}
	now := time.Now()
	khronos := uint32(now.Unix())
	ticketBase := now.UnixMilli()
	query, fullURL := "", ""
	for offset := int64(0); offset < hongguoBranchAttempts; offset++ {
		query = hongguoAccountQuery(extra, device, versionCode, versionName, int64(khronos), ticketBase+offset)
		fullURL = base + path + "?" + query
		if hgDrpyBranchOf(fullURL, body, khronos) == 0 {
			break
		}
	}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	request, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for name, value := range hongguoAccountSign(fullURL, query, body, now, device, versionName, userAgent) {
		request.Header.Set(name, value)
	}
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	content, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("红果账号接口 HTTP %d", response.StatusCode)
	}
	if len(content) == 0 {
		// 空 body 在红果接口里通常意味着签名或设备身份没过，不是「没有数据」。
		return nil, errors.New("红果账号接口返回空响应，登录态或设备身份可能已失效")
	}
	if len(content) > providerMaxBodyBytes {
		return nil, errors.New("红果账号接口返回内容过大")
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, errors.New("红果账号接口返回格式异常")
	}
	if code := mapString(result, "code"); code != "" && code != "0" {
		message := mapString(result, "message", "msg")
		if message == "" || strings.EqualFold(message, "success") {
			message = "错误码 " + truncate(code, 20)
		}
		return nil, fmt.Errorf("红果账号接口返回：%s", truncate(message, 80))
	}
	return result, nil
}

// hongguoAccountBaseKey 让测试把账号主机指向 httptest（生产不设置）。
type hongguoAccountBaseKey struct{}

// hongguoAccountFavorites 拉视频书架（收藏）。
//
// 路径里的 /video/ 是服务端过滤，只回剧集，不混小说。
func (downloader *Downloader) hongguoAccountFavorites(ctx context.Context, device hongguoAccountDevice, cookie string) ([]hongguoAccountItem, error) {
	result, err := downloader.hongguoAccountRequest(ctx, device, cookie, "/reading/bookapi/bookshelf/video/info/v1/", nil, nil, false)
	if err != nil {
		return nil, err
	}
	data := nestedMap(result, "data")
	rows := anyList(data["video_shelf_info"])
	items := make([]hongguoAccountItem, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		entry, _ := row.(map[string]any)
		item, ok := hongguoAccountItemFromShelf(entry)
		if !ok || seen[item.SeriesID] {
			continue
		}
		seen[item.SeriesID] = true
		items = append(items, item)
		if len(items) >= hongguoAccountMaxFavorites {
			break
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CollectedAt > items[j].CollectedAt })
	return items, nil
}

// hongguoAccountHistory 拉最近 50 部观看记录，再用 multi-detail 补封面标题。
//
// 上游用 read_progress 而不是 read_history：后者是网页端的口径，字段与
// App 端不一致；read_progress 的 video_data 才带 vid_index 与毫秒进度。
func (downloader *Downloader) hongguoAccountHistory(ctx context.Context, device hongguoAccountDevice, cookie string) ([]hongguoAccountItem, error) {
	extra := [][2]string{{"book_types", "2"}, {"limit", strconv.Itoa(hongguoAccountMaxHistory)}}
	result, err := downloader.hongguoAccountRequest(ctx, device, cookie, "/reading/bookapi/read_progress/list/v1/", extra, nil, false)
	if err != nil {
		return nil, err
	}
	rows := anyList(nestedMap(result, "data")["video_data"])
	if len(rows) == 0 {
		// 实测这个接口的 video_data 有时与 data 平级（顶层）。
		rows = anyList(result["video_data"])
	}
	items := make([]hongguoAccountItem, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		entry, _ := row.(map[string]any)
		item, ok := hongguoAccountItemFromProgress(entry)
		if !ok || seen[item.SeriesID] {
			continue
		}
		seen[item.SeriesID] = true
		items = append(items, item)
		if len(items) >= hongguoAccountMaxHistory {
			break
		}
	}
	if len(items) == 0 {
		return items, nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.SeriesID)
	}
	detailExtra := [][2]string{{"book_id", strings.Join(ids, ",")}, {"book_type", "2"}}
	if details, detailErr := downloader.hongguoAccountRequest(ctx, device, cookie, "/reading/bookapi/multi-detail/v1/", detailExtra, nil, false); detailErr == nil {
		byID := map[string]map[string]any{}
		for _, row := range anyList(details["data"]) {
			entry, _ := row.(map[string]any)
			if id := firstNonEmpty(mapString(entry, "book_id", "series_id", "book_id_str")); id != "" {
				byID[id] = entry
			}
		}
		for index := range items {
			entry := byID[items[index].SeriesID]
			if len(entry) == 0 {
				continue
			}
			items[index].Title = firstNonEmpty(mapString(entry, "book_name", "series_title", "title"), items[index].Title)
			items[index].Cover = firstNonEmpty(items[index].Cover, hongguoAccountCover(entry))
			items[index].Description = firstNonEmpty(items[index].Description, mapString(entry, "abstract", "description"))
			if count := hongguoAccountInt(entry, "episode_cnt", "episode_count", "drama_chapter_number", "chapter_count"); count > 0 {
				items[index].Episodes = count
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	return items, nil
}

// hongguoAccountItemFromShelf 解析一条书架记录。
func hongguoAccountItemFromShelf(row map[string]any) (hongguoAccountItem, bool) {
	if len(row) == 0 {
		return hongguoAccountItem{}, false
	}
	video := nestedMap(row, "video_data")
	if len(video) == 0 {
		video = row
	}
	id := firstNonEmpty(
		mapString(video, "series_id", "book_id", "series_id_str", "book_id_str"),
		mapString(row, "series_id", "book_id", "series_id_str", "book_id_str"),
	)
	title := firstNonEmpty(mapString(row, "series_title", "book_name"), mapString(video, "title", "book_name", "series_title"))
	if !hongguoNumericID.MatchString(id) || title == "" {
		return hongguoAccountItem{}, false
	}
	return hongguoAccountItem{
		SeriesID:    id,
		Title:       strings.TrimSpace(title),
		Cover:       hongguoAccountCover(row, video),
		Description: firstNonEmpty(mapString(row, "series_intro", "abstract"), mapString(video, "video_desc", "description")),
		Episodes:    hongguoAccountInt(video, "episode_cnt", "episode_count"),
		Category:    hongguoAccountCategory(row, video),
		CollectedAt: hongguoAccountMillis(row, "collect_time"),
	}, true
}

// hongguoAccountItemFromProgress 解析一条观看进度。
func hongguoAccountItemFromProgress(row map[string]any) (hongguoAccountItem, bool) {
	if len(row) == 0 {
		return hongguoAccountItem{}, false
	}
	id := firstNonEmpty(mapString(row, "book_id_str", "book_id", "series_id"))
	episodeID := firstNonEmpty(mapString(row, "vid", "item_id", "episode_id"))
	if !hongguoNumericID.MatchString(id) || !hongguoNumericID.MatchString(episodeID) {
		return hongguoAccountItem{}, false
	}
	index := hongguoAccountInt(row, "vid_index")
	if index < 0 {
		index = 0
	}
	position := hongguoAccountSeconds(row, "current_play_position", "current_play_time")
	duration := hongguoAccountSeconds(row, "duration", "current_total_time", "total_time")
	return hongguoAccountItem{
		SeriesID:  id,
		Title:     strings.TrimSpace(firstNonEmpty(mapString(row, "series_title", "book_name"), "剧集 "+id)),
		Cover:     hongguoAccountCover(row),
		Episodes:  hongguoAccountInt(row, "episode_cnt", "episode_count"),
		Episode:   index,
		EpisodeID: episodeID,
		Position:  position,
		Duration:  duration,
		UpdatedAt: hongguoAccountMillis(row, "read_timestamp_ms", "last_watch_time", "update_timestamp_ms"),
	}, true
}

// hongguoAccountCover 按优先级取封面，顺手把 // 开头的协议相对地址补全。
func hongguoAccountCover(rows ...map[string]any) string {
	for _, row := range rows {
		value := firstNonEmpty(
			mapString(row, "series_cover_url", "thumb_url", "cover", "cover_url"),
			mapString(nestedMap(row, "video_data"), "cover", "cover_url", "thumb_url", "series_cover_url"),
		)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "//") {
			return "https:" + value
		}
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			return value
		}
	}
	return ""
}

// hongguoAccountCategory 取题材标签。
//
// category_schema 是「JSON 字符串里套数组」的形态，解析失败就退回 category，
// 不让一个展示字段把整条记录判废。
func hongguoAccountCategory(rows ...map[string]any) string {
	for _, row := range rows {
		raw := mapString(row, "category_schema")
		if raw != "" {
			var entries []map[string]any
			if err := json.Unmarshal([]byte(raw), &entries); err == nil {
				names := make([]string, 0, 5)
				for _, entry := range entries {
					if name := strings.TrimSpace(mapString(entry, "name")); name != "" {
						names = append(names, name)
					}
					if len(names) >= 5 {
						break
					}
				}
				if len(names) > 0 {
					return strings.Join(names, " · ")
				}
			}
		}
		if value := mapString(row, "category"); value != "" {
			return value
		}
	}
	return ""
}

// hongguoAccountInt 读整数，兼容 json.Number / float64 / string。
func hongguoAccountInt(row map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return int(parsed)
			}
			if parsed, err := typed.Float64(); err == nil {
				return int(parsed)
			}
		case float64:
			return int(typed)
		case int:
			return typed
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return parsed
			}
		}
	}
	return 0
}

// hongguoAccountMillis 读时间戳并统一成毫秒。
//
// 服务端同一字段有秒有毫秒（10 位 / 13 位），按量级判断，不做猜测。
func hongguoAccountMillis(row map[string]any, keys ...string) int64 {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		var number int64
		switch typed := value.(type) {
		case json.Number:
			number, _ = typed.Int64()
		case float64:
			number = int64(typed)
		case string:
			number, _ = strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		}
		if number <= 0 {
			continue
		}
		if number < 10_000_000_000 {
			return number * 1000
		}
		return number
	}
	return 0
}

// hongguoAccountSeconds 读毫秒时长并转成秒。
func hongguoAccountSeconds(row map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value, ok := row[key]
		if !ok || value == nil {
			continue
		}
		var number float64
		switch typed := value.(type) {
		case json.Number:
			number, _ = typed.Float64()
		case float64:
			number = typed
		case string:
			number, _ = strconv.ParseFloat(strings.TrimSpace(typed), 64)
		}
		if number > 0 {
			return number / 1000
		}
	}
	return 0
}

// hongguoAccountSync 一次账号同步：读登录态 → 拉收藏与历史 → 返回快照。
//
// 未登录返回 SignedIn=false 而不是错误——设置页打开时调用它不该弹红条。
func (engine *nativeEngine) hongguoAccountSync(ctx context.Context) (hongguoAccountSnapshot, error) {
	session := engine.hongguoLoginLoad()
	if !hongguoLoginHasSession(session.Cookies) {
		return hongguoAccountSnapshot{SignedIn: false}, nil
	}
	snapshot := hongguoAccountSnapshot{
		SignedIn: true,
		UserID:   session.UserID,
		Nickname: session.Nickname,
	}
	anon, err := engine.downloader.hongguoAnonDevice(ctx)
	if err != nil {
		return snapshot, err
	}
	device := hongguoAccountDevice{DeviceID: anon.DeviceID, InstallID: anon.InstallID, CDID: anon.CDID}
	cookie := hongguoAccountCookieHeader(session.Cookies)
	favorites, favoritesErr := engine.downloader.hongguoAccountFavorites(ctx, device, cookie)
	history, historyErr := engine.downloader.hongguoAccountHistory(ctx, device, cookie)
	snapshot.Favorites = favorites
	snapshot.History = history
	snapshot.SyncedAt = time.Now().Unix()
	switch {
	case favoritesErr != nil && historyErr != nil:
		return snapshot, favoritesErr
	case favoritesErr != nil:
		snapshot.Warning = "收藏没同步成功：" + favoritesErr.Error()
	case historyErr != nil:
		snapshot.Warning = "观看记录没同步成功：" + historyErr.Error()
	}
	return snapshot, nil
}
