package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 红果「推荐流」接口：bookapi/bookmall/tab/v1。
//
// 【为什么单独开一条链路】分类/推荐原先统一走 landpage（distribution/category），
// 该接口对匿名设备返回 HTTP 200 + 0 字节（设备级门禁，换注册身份亦然）。而
// tab/v1 是首页信息流，2026-10-30 实测匿名可用：offset=0 返回 3 部，offset=1
// 返回 6 部（4 部新增），offset 递增即「换一批」，靠服务端 session_id 串会话。
//
// 【为什么不用签名】参考实现（tsukiyo-zero/hongguoTV core/RecommendationFeed.kt）
// 这条链路只带 UA + Accept + X-XS-From-Web + Sdk-Version，无 x-gorgon/x-medusa
// 等签名头；本次实测按其参数直连同样成功。签名只对 landpage 必要。
//
// 【aid 为什么是 8662 而不是红果自己的 513755】该接口属番茄小说（novelread）
// 内容池，红果短剧与番茄共用剧集 ID 空间。aid/app_name/version 三者必须成套，
// 混用会被服务端判为非法客户端。
//
// 【host 为什么是 sinfonlinea】实测可用主机；分类用的 sinfonlineb 在这条路径上
// 未验证，不做混用。
const (
	hongguoFeedBaseURL   = "https://api5-normal-sinfonlinea.fqnovel.com"
	hongguoFeedPath      = "/reading/bookapi/bookmall/tab/v1/"
	hongguoFeedUserAgent = "com.dragon.read/73532 (Linux; U; Android 16; zh_CN; 25053RT47C)"
	// hongguoFeedGenre 是推荐流在查询/缓存里的伪分类键，不参与官网兜底映射。
	hongguoFeedGenre = "feed"
	// hongguoPersonalFeedGenre 是「账号推荐（实验）」的伪分类键：与 feed 共用
	// tab/v1 的解析与分页，区别只在请求是否带红果账号 Cookie（+178）。
	hongguoPersonalFeedGenre = "feed_personal"
	// 实测响应 0.6~1.4 MB，给 16 MB 上限留足余量又不至于被异常响应拖死。
	hongguoFeedBodyLimit = 16 << 20
	// 单次交互最多连取几批、攒够多少部。
	//
	// 【为什么要连取】推荐流每批只有 3~6 张卡片，且重复率很高（实测 offset=2~5
	// 每批仅 1 部是新剧）。只取一批就返回，用户点「继续推荐」常常只多 1 部，
	// 观感像卡住了。连取到 9 部再返回，一次交互的增量才像「一批新剧」。
	// 上限 4 批是耗时与收益的折中：实测每批 0.4~1.0 秒，4 批约 1.6~4 秒。
	hongguoFeedBatchLimit  = 4
	hongguoFeedTargetItems = 9
)

// isHongguoFeedGenre 判断 genre 是否走 tab/v1 信息流：匿名公开流（feed）
// 与账号个性流（feed_personal）都算，landpage 分类推荐不算。
func isHongguoFeedGenre(genre string) bool {
	return genre == hongguoFeedGenre || genre == hongguoPersonalFeedGenre
}

// fetchHongguoFeed 连取若干批推荐流，攒到目标条数或连续无新增为止。
// cookie 非空时每批请求都带账号 Cookie（账号推荐实验档），为空即匿名公开流。
//
// 返回的 NextOffset/SessionID 是最后一批的游标，可直接用于下一次调用。
// 中途某一批失败时：只要前面已经取到剧集就正常返回（网络抖动不该把整屏打掉），
// 一部都没取到才把错误抛上去。
func (downloader *Downloader) fetchHongguoFeed(ctx context.Context, query hongguoRecommendationQuery, cookie string) (hongguoRecommendationPage, error) {
	page := hongguoRecommendationPage{Dramas: []Drama{}}
	seen := make(map[string]bool, len(query.Seen))
	for _, id := range query.Seen {
		seen[strings.TrimPrefix(id, "hongguo:")] = true
	}
	next := query
	var lastErr error
	for batch := 0; batch < hongguoFeedBatchLimit; batch++ {
		current, err := downloader.fetchHongguoFeedPage(ctx, next, cookie)
		if err != nil {
			lastErr = err
			break
		}
		added := 0
		for _, drama := range current.Dramas {
			id := strings.TrimPrefix(drama.ID, "hongguo:")
			if seen[id] {
				continue
			}
			seen[id] = true
			added++
			page.Dramas = append(page.Dramas, drama)
		}
		page.SessionID = current.SessionID
		page.NextOffset = current.NextOffset
		if added == 0 || len(page.Dramas) >= hongguoFeedTargetItems {
			break
		}
		next.Offset = current.NextOffset
		next.SessionID = current.SessionID
		next.Seen = next.Seen[:0]
		for id := range seen {
			next.Seen = append(next.Seen, "hongguo:"+id)
			if len(next.Seen) >= 540 {
				break
			}
		}
	}
	if len(page.Dramas) == 0 && lastErr != nil {
		return hongguoRecommendationPage{}, lastErr
	}
	// 推荐流没有可靠结束标记，保持可继续。
	page.HasMore = true
	return page, ctx.Err()
}

// fetchHongguoFeedPage 拉取推荐流的一批（单次请求）。
//
// offset 语义与 landpage 不同：landpage 是「第 N 页」，这里是从 0 开始的批次序号，
// 每次 +1 就是「换一批」。has_more / bottom_unlimited 在实测中不可信（恒 false
// 或缺失，但继续请求仍有新卡片），所以固定 HasMore=true，由上层按「这批有没有
// 新剧」决定是否提示换一批。
func (downloader *Downloader) fetchHongguoFeedPage(ctx context.Context, query hongguoRecommendationQuery, cookie string) (hongguoRecommendationPage, error) {
	page := hongguoRecommendationPage{Dramas: []Drama{}}
	if !isHongguoFeedGenre(query.Genre) {
		return page, errors.New("推荐分类无效")
	}
	values := url.Values{
		"aid":                     {"8662"},
		"app_name":                {"novelread"},
		"channel":                 {"update_64"},
		"device_platform":         {"android"},
		"os":                      {"android"},
		"ssmix":                   {"a"},
		"device_type":             {"25053RT47C"},
		"device_brand":            {"Redmi"},
		"language":                {"zh"},
		"os_api":                  {"36"},
		"os_version":              {"16"},
		"resolution":              {"1280*2772"},
		"dpi":                     {"520"},
		"ac":                      {"wifi"},
		"version_code":            {"73532"},
		"version_name":            {"7.3.5.32"},
		"manifest_version_code":   {"73532"},
		"update_version_code":     {"73532"},
		"tab_type":                {"16"},
		"bottom_tab_type":         {"7"},
		"landing_bottom_tab_type": {"7"},
		"offset":                  {strconv.Itoa(query.Offset)},
		"client_req_type":         {"2"},
		"_rticket":                {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}
	if query.Offset == 0 {
		// 首批标记：参考实现 offset==0 时传 1，其余传 2；实测两种都能返回，
		// 但按参考实现对齐可以避免服务端把首请求当续页处理。
		values.Set("client_req_type", "1")
	}
	if query.SessionID != "" {
		values.Set("session_id", query.SessionID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, hongguoFeedBaseURL+hongguoFeedPath+"?"+values.Encode(), nil)
	if err != nil {
		return page, err
	}
	request.Header.Set("User-Agent", hongguoFeedUserAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-XS-From-Web", "0")
	request.Header.Set("Sdk-Version", "2")
	if cookie != "" {
		// [+178] 账号推荐实验档把落盘的账号 Cookie 送进 tab/v1。Cookie 只在内存里
		// 流动：绝不能塞进 hongguoRecommendationQuery —— 那个结构会被序列化
		// 进 engine.recommendations 并由 writeCatalogDiskLocked 落盘，等于把
		// 会话凭据写进磁盘缓存。
		//
		// 参考 tsukiyo-zero/hongguoTV core/RecommendationFeed.kt：带 Cookie 时
		// 不能再塞固定 device_id / iid，否则 feed 返回 110 ILLEGAL_ACCESS。
		// 本实现的 tab/v1 参数本来就不含 device_id / iid（只有 UA 设备型号、
		// aid 等公共参数）；若实测出现 110，再考虑移除设备指纹字段。
		request.Header.Set("Cookie", cookie)
	}
	response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, nil)
	if err != nil {
		return page, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return page, fmt.Errorf("红果推荐请求失败：HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, hongguoFeedBodyLimit))
	if err != nil {
		return page, err
	}
	if len(body) == 0 {
		// 与 landpage 一样存在空 body 语义，但这条链路实测未出现；保留同义错误，
		// 便于界面提示统一走「红果推荐暂时不可用」而不是显示 JSON 解析失败。
		return page, errHongguoEmptyBody
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return page, errors.New("红果推荐数据格式异常")
	}
	return parseHongguoFeedPage(result, query)
}

// parseHongguoFeedPage 把 tab/v1 的响应摊平成剧集列表。
//
// 结构：data.tab_item[] 里取 tab_type=16 的那一项，卡片藏在
// cell_data[].cell_data[].video_data[]，实际剧集字段在每项的 video_detail 里
// （个别卡片直接平铺）。content_type 1=真人剧、1004=漫剧/AI剧，其余（小说、
// 广告位等）一律丢弃——推荐流是混排，不做过滤会把非剧内容带进剧库。
func parseHongguoFeedPage(result map[string]any, query hongguoRecommendationQuery) (hongguoRecommendationPage, error) {
	page := hongguoRecommendationPage{Dramas: []Drama{}}
	if code := mapString(result, "code"); code != "" && code != "0" {
		return page, errors.New("红果推荐暂不可用（错误码 " + code + "）")
	}
	data := nestedMap(result, "data")
	if len(data) == 0 {
		return page, errors.New("红果推荐数据格式异常")
	}
	var tab map[string]any
	for _, item := range anyList(data["tab_item"]) {
		candidate, _ := item.(map[string]any)
		if mapString(candidate, "tab_type") == "16" {
			tab = candidate
			break
		}
	}
	if len(tab) == 0 {
		return page, errors.New("红果推荐数据格式异常")
	}
	session := mapString(tab, "session_id")
	if len(session) > 4096 || strings.ContainsAny(session, "\r\n\x00") {
		return page, errors.New("红果推荐分页标记无效")
	}
	seen := make(map[string]bool, len(query.Seen))
	for _, id := range query.Seen {
		seen[strings.TrimPrefix(id, "hongguo:")] = true
	}
	rows := 0
	valid := 0
	for _, cellItem := range anyList(tab["cell_data"]) {
		cell, _ := cellItem.(map[string]any)
		for _, cardItem := range anyList(cell["cell_data"]) {
			card, _ := cardItem.(map[string]any)
			for _, videoItem := range anyList(card["video_data"]) {
				rows++
				video, _ := videoItem.(map[string]any)
				detail := video
				if nested, ok := video["video_detail"].(map[string]any); ok {
					detail = nested
				}
				if !hongguoFeedContentType(detail) {
					continue
				}
				drama := hongguoDramaFromAny(detail, "")
				if drama.ID == "" {
					continue
				}
				valid++
				id := strings.TrimPrefix(drama.ID, "hongguo:")
				if seen[id] {
					continue
				}
				seen[id] = true
				page.Dramas = append(page.Dramas, drama)
			}
		}
	}
	// rows==0：结构变了，一张卡片都没解析出来。
	// valid==0：有卡片但全是非剧内容（小说/广告位）或 ID 非法——同样是数据面异常，
	// 不能当成「这批没有新剧」放过，否则界面会显示成空列表而不是可重试的错误。
	// 只有「有效但都被 seen 过滤」才是正常的换批空结果，交由上层提示。
	if rows == 0 || valid == 0 {
		return hongguoRecommendationPage{}, errors.New("红果推荐未返回可播放的剧集")
	}
	page.NextOffset = query.Offset + 1
	page.SessionID = session
	// 推荐流没有可靠结束标记（has_more 恒 false 但仍有新卡片），保持可继续。
	page.HasMore = true
	return page, nil
}

// hongguoFeedContentType 判断卡片是否为短剧。
func hongguoFeedContentType(detail map[string]any) bool {
	switch mapString(detail, "content_type") {
	case "1", "1004":
		return true
	}
	return false
}
