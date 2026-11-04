package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// 红果「行为推荐」：两段式首页信息流（路线二 / 第三档）。
//
// 【为什么单独开一个文件】现有 feed / feed_personal 走 bookmall/tab/v1 单端点，
// offset+1 就是「换一批」，服务端没有任何行为回传。本文件是另一条完整链路：
//
//	首批  GET /reading/bookapi/bookmall/tab/v?client_req_type=1028
//	      → data.tab_item[tab_type=16] 给 session_id / cell_id_str /
//	        bookstore_id（翻页必需的双 id）+ 首批 3 张卡片
//	翻页  GET /reading/bookapi/bookmall/cell/change/v?client_req_type=2
//	      → data.cell_view.cell_data[].video_data[]，next_offset 递进（实测 0→6→12）
//	      回传 filter_ids（已下发 series_id）+ 最近曝光环
//
// 端点、参数名、会话状态族、曝光环结构均照 it00021hot/hongguo-desktop
// crates/hongguo-api/src/recommend/mod.rs（Rust 参考实现）；本项目不引签名器，
// 实测 2026-11-05 匿名直连 code=0 SUCCESS，无需 x-gorgon / x-medusa 族签名。
//
// 【与参考实现的差异，实测得出，改动前务必重新验证】
//  1. 参考实现从卡片 cell_extra_data.rec_filter_info 取曝光上下文。当前服务端
//     已不下发该字段（实测 cell_extra_data 恒为 null），改从同卡片顶层的
//     recommend_group_id（= series_id）、recommend_info（JSON 字符串，含
//     channel_id / request_id / rank）与 video_detail.content_type 组装。
//     hongguoCellImpressionOf 仍优先读 rec_filter_info，服务端恢复后自动切回。
//  2. 响应 has_more 恒 false，但继续翻页仍有 6 张全新卡片（与 tab/v1 一样
//     不可信），所以固定 HasMore=true，靠本地 seen 判断「这批有没有新剧」。
//  3. 参考实现曝光环是进程全局；这里按 genre 分开，避免以后再加子项串味。
//
// 【为什么所有运行态都只在内存里，不进 hongguoRecommendationQuery】
// query 会被序列化进 engine.recommendations 并由 writeCatalogDiskLocked 写进
// catalogs.json。会话 uuid、曝光环、服务端下发的 cell/plan 双 id 都是会话级
// 运行态：落盘既没有意义（重启后会话本来就该重开），也会把用户浏览行为写到
// 磁盘缓存里。所以这里用包级 sync.Mutex 状态，进程退出即清空；双 id 另外带
// 实测常量兜底，重启后即使 query 里恢复了旧 session_id 也能继续翻页。
//
// 【为什么不需要 Cookie】本档定位「无需登录的行为推荐」：cr=1028 自带
// need_personal_recommend=1，会话级画像由 session_id + 曝光回传建立。带账号
// Cookie 在这套参数下未验证（tab/v1 那边有 110 ILLEGAL_ACCESS 的先例），
// 因此 cookie 参数保留但默认由调用方传空；要接账号态时只改 app_recommendations.go
// 里取 cookie 的那一段即可，本文件不用动。
const (
	// hongguoCellFeedGenre 是「行为推荐」的伪分类键，Dart 侧
	// recommendations_screen.dart 的 _genres 必须用同一字符串（+179）。
	hongguoCellFeedGenre = "feed_behavior"

	hongguoCellOrigin     = "https://api5-normal-sinfonlinec.fqnovel.com"
	hongguoCellTabPath    = "/reading/bookapi/bookmall/tab/v"
	hongguoCellChangePath = "/reading/bookapi/bookmall/cell/change/v"
	hongguoCellTabType    = "16"

	// 参考实现官方恒值：图片缩略规格、recommend_extra、NPS 插入位配额。
	hongguoCellImageShrink = "W3siaW1hZ2VfdHlwZSI6MiwiaW1hZ2Vfd2lkdGgiOjM2MCwic2hyaW5rX3R5cGUiOjJ9LHsiaW1hZ2VfdHlwZSI6MywiaW1hZ2Vfd2lkdGgiOjEwNzgsInNocmlua190eXBlIjozfSx7ImltYWdlX3R5cGUiOjQsImltYWdlX3dpZHRoIjo5OSwic2hyaW5rX3R5cGUiOjR9XQ=="
	hongguoCellExtra       = "eyJyZWNlbnRfZGlzbGlrZV9naWQiOltdLCJzZXNzaW9uX2FwcF9zdGF5X3RpbWUiOjB9"
	hongguoCellNPSRank     = `{"1":3,"3":3,"2":3,"4":3}`

	// 实测 2026-11-05 服务端下发的 tab=16 配置，与参考实现 known_tab_config("16")
	// 完全一致。仅在内存配置丢失（进程重启）时兜底，避免翻页拿不到双 id。
	hongguoCellFallbackCellID = "7294257141911650341"
	hongguoCellFallbackPlanID = "7294257258253254693"

	// 官方 latest_impression_gids 一次 2~6 个，取 8 留余量。
	hongguoCellImpressionCap = 8
	// filter_ids 最多回传多少个已见 series_id。app_recommendations.go 的 Seen
	// 上限是 540，全塞进 GET query 会有 ~10KB URL；实测 200 个足够覆盖一轮会话，
	// 其余靠本地 seen 去重，不必让服务端重复排除。
	hongguoCellFilterIDLimit = 200
)

func isHongguoCellFeedGenre(genre string) bool { return genre == hongguoCellFeedGenre }

// ---------------------------------------------------------------------------
// 会话级运行态（内存，不落盘）
// ---------------------------------------------------------------------------

type hongguoCellConfig struct {
	CellID string // cell_id_str → cell/change 的 cell_id
	PlanID string // bookstore_id → cell/change 的 plan_id
}

type hongguoCellImpression struct {
	ContentType string         `json:"content_type"`
	FilterIDMap map[string]any `json:"filter_id_map"`
	RecGroupID  int64          `json:"rec_group_id"`
}

var (
	hongguoCellMu            sync.Mutex
	hongguoCellConfigs       = map[string]hongguoCellConfig{}
	hongguoCellImpressions   = map[string][]hongguoCellImpression{}
	hongguoCellSessionOnce   sync.Once
	hongguoCellSessionUUID   string
	hongguoCellRequestSerial int64
)

// hongguoCellSession 返回进程生命周期内固定的会话 UUID。参考实现一次启动发
// 三枚，这里用同一枚派生裸 uuid 与 uuid#1 两形态；服务端只要求同会话内一致。
func hongguoCellSession() string {
	hongguoCellSessionOnce.Do(func() {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			hongguoCellSessionUUID = fmt.Sprintf("%016x", time.Now().UnixNano())
			return
		}
		b[6] = b[6]&0x0f | 0x40
		b[8] = b[8]&0x3f | 0x80
		h := hex.EncodeToString(b[:])
		hongguoCellSessionUUID = h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
	})
	return hongguoCellSessionUUID
}

func hongguoCellSaveConfig(genre string, config hongguoCellConfig) {
	if config.CellID == "" || config.PlanID == "" {
		return
	}
	hongguoCellMu.Lock()
	hongguoCellConfigs[genre] = config
	hongguoCellMu.Unlock()
}

func hongguoCellConfigFor(genre string) hongguoCellConfig {
	hongguoCellMu.Lock()
	config, ok := hongguoCellConfigs[genre]
	hongguoCellMu.Unlock()
	if ok && config.CellID != "" && config.PlanID != "" {
		return config
	}
	return hongguoCellConfig{CellID: hongguoCellFallbackCellID, PlanID: hongguoCellFallbackPlanID}
}

// hongguoCellRecordImpressions 把本批卡片的曝光上下文压进容量 8 的环，
// 按 rec_group_id 去重。下一批 cell/change 会原样回传给服务端做调序上下文。
func hongguoCellRecordImpressions(genre string, items []hongguoCellImpression) {
	if len(items) == 0 {
		return
	}
	hongguoCellMu.Lock()
	defer hongguoCellMu.Unlock()
	ring := hongguoCellImpressions[genre]
	for _, item := range items {
		duplicate := false
		for _, existing := range ring {
			if existing.RecGroupID == item.RecGroupID {
				duplicate = true
				break
			}
		}
		if !duplicate {
			ring = append(ring, item)
		}
	}
	if excess := len(ring) - hongguoCellImpressionCap; excess > 0 {
		ring = append([]hongguoCellImpression{}, ring[excess:]...)
	}
	hongguoCellImpressions[genre] = ring
}

func hongguoCellRecentImpressions(genre string) []hongguoCellImpression {
	hongguoCellMu.Lock()
	defer hongguoCellMu.Unlock()
	return append([]hongguoCellImpression{}, hongguoCellImpressions[genre]...)
}

// ---------------------------------------------------------------------------
// 请求构造
// ---------------------------------------------------------------------------

// hongguoCellCommon 是与本项目 tab/v1 一致的公共设备参数（见
// provider_hongguo_feed.go 的注释）。参考实现由 sign_request_with 统一注入，
// 本项目没有签名器，所以在这里显式补齐。
func hongguoCellCommon() url.Values {
	return url.Values{
		"aid":                   {"8662"},
		"app_name":              {"novelread"},
		"channel":               {"update_64"},
		"device_platform":       {"android"},
		"os":                    {"android"},
		"ssmix":                 {"a"},
		"device_type":           {"25053RT47C"},
		"device_brand":          {"Redmi"},
		"language":              {"zh"},
		"os_api":                {"36"},
		"os_version":            {"16"},
		"resolution":            {"1280*2772"},
		"dpi":                   {"520"},
		"ac":                    {"wifi"},
		"version_code":          {"73532"},
		"version_name":          {"7.3.5.32"},
		"manifest_version_code": {"73532"},
		"update_version_code":   {"73532"},
		"_rticket":              {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}
}

func hongguoCellSet(values url.Values, pairs map[string]string) {
	for key, value := range pairs {
		values.Set(key, value)
	}
}

// hongguoCellFirstValues：client_req_type=1028 的书城首页请求。
// 逐项照抄参考实现 request_feed_tab_first_page，零值族不能省（缺了会被
// 服务端当「无个性化上下文」处理）。
func hongguoCellFirstValues(values url.Values) {
	session := hongguoCellSession()
	hongguoCellSet(values, map[string]string{
		"filter_ids": "", "last_search_query_from_rec": "false",
		"rec_filter_info_list": "", "bottom_tab_type_list": "7,0,1,2,3,4",
		"last_tab_index": "0", "bookmall_video_last_active_enter_time": "0",
		"is_video_feed_tab_first_request_cold_start": "true",
		"is_horizontal_screen":                       "false", "screen_width_px": "1078",
		"tab_type": hongguoCellTabType, "first_use_category_select": "false",
		"image_shrink_datas_str":      hongguoCellImageShrink,
		"last_session_video_tab_type": hongguoCellTabType,
		"disable_digg_stat":           "false", "ecom_refresh_type": "0",
		"pad_column_cover": "0", "last_cold_start_time": "0",
		"last_tab_type": hongguoCellTabType, "req_rank_category_id": "0",
		"ranklist_in_1col_no_consume_show_times": "0", "video_tab_cold_start": "0",
		"offset": "0", "video_type_preferences_str": "[]", "auth_aweme": "false",
		"cold_start_is_double_gd": "false", "item_id": "0", "page_entry_time": "0",
		"cold_start_session": "0", "tab_index": "0",
		"bookmall_playlet_last_active_enter_time": "0", "book_id": "0",
		"ecom_impression_start_time": "0", "enable_search_box_collapse": "false",
		"ranklist_in_1col_show_times": "0", "landing_bottom_tab_type": "0",
		"bottom_tab_type": "7", "top_tab_extra": "",
		"migration_top_tab_enable": "false", "auth_backward": "false",
		"video_bottom_tab_bubble_display_count": "0", "has_video_cache": "false",
		"pad_column_detail": "0", "after_genre_preference_popup": "0",
		"client_req_type": "1028", "req_rank_algo": "0",
		"unlimited_short_series_next_offset": "0",
		"normal_session_cnt_in_day":          "8", "cold_start_session_cnt_in_day": "4",
		"normal_session_id": session + "#1", "cold_start_session_id": session,
		"cold_start_session_cnt_in_life": "32", "normal_session_cnt_in_life": "64",
		"charging": "0", "har_status": "0", "is_power_save_mode": "0",
		"app_dark_mode": "1", "screen_brightness": "21", "battery_pct": "71",
		"down_speed": "60000", "sys_dark_mode": "1", "current_volume": "0",
		"app_mini_window": "0", "sys_mini_window": "1", "font_scale": "100",
		"network_type": "4", "rom_version": "V417IR release-keys",
		"need_personal_recommend": "1", "recommend_extra": hongguoCellExtra,
	})
}

// hongguoCellChangeValues：client_req_type=2 的 cell/change 翻页请求。
// 曝光环 → rec_filter_info_list + refresh_action_info.latest_impression_gids；
// 已见 series_id → filter_ids。两者是「推荐跟着浏览走」的核心回传。
func hongguoCellChangeValues(values url.Values, config hongguoCellConfig, sessionID string, offset int, filterIDs string, impressions []hongguoCellImpression) {
	session := hongguoCellSession()
	list, err := json.Marshal(impressions)
	if err != nil || len(impressions) == 0 {
		list = []byte("[]")
	}
	gids := make([]string, 0, len(impressions))
	for _, item := range impressions {
		gids = append(gids, strconv.FormatInt(item.RecGroupID, 10))
	}
	refresh, err := json.Marshal(map[string]any{
		"first_screen_impression_gids": []string{},
		"has_active_refresh":           false,
		"latest_impression_gids":       strings.Join(gids, ","),
	})
	if err != nil {
		refresh = []byte(`{"first_screen_impression_gids":[],"has_active_refresh":false,"latest_impression_gids":""}`)
	}
	hongguoCellSet(values, map[string]string{
		"author_id": "0", "book_comment_id": "0", "category_id": "0",
		"cell_id": config.CellID, "cell_sub_id": "0", "change_type": "0",
		"client_req_type": "2", "client_template": "7", "cold_start_session": "0",
		"current_chapter_num": "0", "disable_digg_stat": "false",
		"ecom_category_id": "0", "ecom_impression_start_time": "0",
		"ecom_refresh_type": "0", "ecom_sort_by": "0", "genre": "0", "genre_type": "0",
		"idol_tag_id": "0", "image_shrink_datas_str": hongguoCellImageShrink,
		"inner_algo": "0", "inner_category_id": "0", "is_horizontal_screen": "false",
		"item_id": "0", "limit": "0", "nps_insert_rank_json": hongguoCellNPSRank,
		"offset": strconv.Itoa(offset), "page": "0", "page_entry_time": "0",
		"plan_id": config.PlanID, "post_id": "0", "rank_sub_info_id": "0",
		"reader_single_col_from_ip_strategy": "false",
		"rec_filter_info_list":               string(list),
		"recommend_extra":                    hongguoCellExtra,
		"refresh_action_info":                string(refresh),
		"related_book_id":                    "0", "screen_width_px": "1078", "search_tab_type": "0",
		"source_tab_type": "0", "sub_genre": "0", "sub_tag_id": "0",
		"support_gender_list": "false", "tab_type": hongguoCellTabType,
		"tag_id": "0", "total_chapter_num": "0",
		"unlimited_selector_change_type": "0", "video_tab_cold_start": "0",
		"video_type_preferences_str": "[]", "web_page_version_code": "0",
		"app_dark_mode": "1", "app_launch_times": "0", "app_mini_window": "0",
		"battery_pct": "71", "charging": "0",
		"cold_start_session_cnt_in_day": "4", "cold_start_session_cnt_in_life": "32",
		"cold_start_session_id": session, "current_volume": "0",
		"down_speed": "60000", "font_scale": "100", "gender": "2",
		"har_status": "0", "is_power_save_mode": "0", "network_type": "4",
		"normal_session_cnt_in_day": "8", "normal_session_cnt_in_life": "64",
		"normal_session_id": session + "#1", "rom_version": "V417IR release-keys",
		"screen_brightness": "21", "sys_dark_mode": "1", "sys_mini_window": "1",
		"session_id": sessionID, "filter_ids": filterIDs,
	})
	// 空会话/空排除表不发对应键（与参考实现一致）。
	if sessionID == "" {
		values.Del("session_id")
	}
	if filterIDs == "" {
		values.Del("filter_ids")
	}
}

// hongguoCellRequest 发起 GET 并解析成 map。与 tab/v1 一样走
// doPreparedCatalogRequest（限流 + 超时），不引签名器；reading 系轻签名头
// 是参考实现 reading_headers() 的三件套，实测缺了也能通，保留对齐官方。
func (downloader *Downloader) hongguoCellRequest(ctx context.Context, path string, values url.Values, cookie string) (map[string]any, error) {
	if len(path) == 0 || path[0] != '/' {
		return nil, errors.New("红果行为推荐请求路径无效")
	}
	values.Set("_rticket", strconv.FormatInt(time.Now().UnixMilli(), 10))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, hongguoCellOrigin+path+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", hongguoFeedUserAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-XS-From-Web", "0")
	request.Header.Set("Sdk-Version", "2")
	request.Header.Set("x-ss-dp", "8662")
	request.Header.Set("lc", "101")
	hongguoCellMu.Lock()
	hongguoCellRequestSerial++
	serial := hongguoCellRequestSerial
	hongguoCellMu.Unlock()
	request.Header.Set("x-reading-request", fmt.Sprintf("%d-%d", time.Now().UnixMilli(), serial))
	if cookie != "" {
		// 预留：目前调用方传空。带账号 Cookie 在这套参数下未验证，接之前先看
		// tab/v1 那边 110 ILLEGAL_ACCESS 的处理（provider_hongguo_feed.go）。
		request.Header.Set("Cookie", cookie)
	}
	response, err := downloader.doPreparedCatalogRequest(request, 20*time.Second, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("红果行为推荐请求失败：HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, hongguoFeedBodyLimit))
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errHongguoEmptyBody
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, errors.New("红果行为推荐数据格式异常")
	}
	if code := mapString(result, "code"); code != "" && code != "0" {
		return nil, errors.New("红果行为推荐暂不可用（错误码 " + code + "）")
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// 卡片解析
// ---------------------------------------------------------------------------

// hongguoCellImpressionOf 从一张卡片提取曝光上下文。
// 优先按参考实现读 cell_extra_data.rec_filter_info；当前服务端已不下发
// 该字段（实测 2026-11-05 恒 null），回退到 recommend_group_id +
// recommend_info + video_detail.content_type 组装等价结构。
func hongguoCellImpressionOf(raw map[string]any) (hongguoCellImpression, bool) {
	if info := nestedMap(raw, "cell_extra_data", "rec_filter_info"); len(info) > 0 {
		gid, _ := strconv.ParseInt(mapString(info, "rec_group_id"), 10, 64)
		if gid > 0 {
			filters, _ := info["filter_id_map"].(map[string]any)
			return hongguoCellImpression{
				ContentType: mapString(info, "content_type"),
				FilterIDMap: filters,
				RecGroupID:  gid,
			}, true
		}
	}
	gidText := mapString(raw, "recommend_group_id", "series_id")
	gid, err := strconv.ParseInt(gidText, 10, 64)
	if err != nil || gid <= 0 {
		return hongguoCellImpression{}, false
	}
	detail := nestedMap(raw, "video_detail")
	contentType := mapString(detail, "content_type")
	if contentType == "" {
		contentType = mapString(raw, "content_type")
	}
	filters := map[string]any{}
	if text := mapString(raw, "recommend_info"); text != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(text), &parsed) == nil {
			for _, key := range []string{"channel_id", "request_id", "rank"} {
				if value, ok := parsed[key]; ok {
					filters[key] = value
				}
			}
		}
	}
	return hongguoCellImpression{ContentType: contentType, FilterIDMap: filters, RecGroupID: gid}, true
}

// hongguoCellDramas 把卡片数组摊平成剧集列表，并把曝光记录返回给调用方入环。
// 卡片形态（实测）：顶层 series_id / vid / recommend_group_id / recommend_info，
// 展示字段在 video_detail（series_title / series_cover / episode_cnt /
// content_type / series_id_str）。hongguoDramaFromAny 期望 video_data 包一层，
// 所以这里把 video_detail 拷进 video_data，并在缺失时补顶层 series_id。
// content_type 1=真人剧、1004=漫剧/AI剧，其余（小说/广告位）丢弃。
func hongguoCellDramas(rows []any) ([]Drama, []hongguoCellImpression) {
	dramas := make([]Drama, 0, len(rows))
	impressions := make([]hongguoCellImpression, 0, len(rows))
	for _, item := range rows {
		raw, _ := item.(map[string]any)
		if len(raw) == 0 {
			continue
		}
		detail := raw
		if nested, ok := raw["video_detail"].(map[string]any); ok {
			detail = nested
		}
		// content_type 缺失时不做过滤（少数运营卡不带该字段，但有合法
		// series_id）；带了就必须是 1 / 1004，否则是小说或广告位。
		contentType := mapString(detail, "content_type")
		if contentType == "" {
			contentType = mapString(raw, "content_type")
		}
		if contentType != "" && !hongguoFeedContentType(map[string]any{"content_type": contentType}) {
			continue
		}
		merged := make(map[string]any, len(detail)+2)
		for key, value := range detail {
			merged[key] = value
		}
		if mapString(merged, "series_id_str", "series_id") == "" {
			merged["series_id"] = mapString(raw, "series_id", "series_id_str")
		}
		drama := hongguoDramaFromAny(map[string]any{"video_data": merged}, "")
		if drama.ID == "" {
			continue
		}
		dramas = append(dramas, drama)
		if impression, ok := hongguoCellImpressionOf(raw); ok {
			impressions = append(impressions, impression)
		}
	}
	return dramas, impressions
}

func hongguoCellTabRows(result map[string]any) (map[string]any, []any) {
	data := nestedMap(result, "data")
	for _, item := range anyList(data["tab_item"]) {
		tab, _ := item.(map[string]any)
		if mapString(tab, "tab_type") != hongguoCellTabType {
			continue
		}
		rows := make([]any, 0)
		for _, cellItem := range anyList(tab["cell_data"]) {
			cell, _ := cellItem.(map[string]any)
			for _, innerItem := range anyList(cell["cell_data"]) {
				inner, _ := innerItem.(map[string]any)
				rows = append(rows, anyList(inner["video_data"])...)
			}
		}
		return tab, rows
	}
	return nil, nil
}

func hongguoCellChangeRows(result map[string]any) (map[string]any, []any) {
	data := nestedMap(result, "data")
	view := nestedMap(data, "cell_view")
	rows := make([]any, 0)
	for _, cellItem := range anyList(view["cell_data"]) {
		cell, _ := cellItem.(map[string]any)
		rows = append(rows, anyList(cell["video_data"])...)
	}
	return data, rows
}

// ---------------------------------------------------------------------------
// 两段式取流
// ---------------------------------------------------------------------------

// hongguoCellFilterIDs 把 query.Seen（"hongguo:<数字>"）转成 filter_ids 逗号表。
func hongguoCellFilterIDs(seen []string) string {
	ids := make([]string, 0, min(len(seen), hongguoCellFilterIDLimit))
	start := max(0, len(seen)-hongguoCellFilterIDLimit)
	for _, id := range seen[start:] {
		id = strings.TrimPrefix(id, "hongguo:")
		if hongguoNumericID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

// fetchHongguoCellFirstPage 拉首批：tab/v?cr=1028，同时把服务端下发的
// session_id / cell_id_str / bookstore_id 存进内存供翻页使用。
func (downloader *Downloader) fetchHongguoCellFirstPage(ctx context.Context, genre, cookie string) (hongguoRecommendationPage, error) {
	page := hongguoRecommendationPage{Dramas: []Drama{}}
	values := hongguoCellCommon()
	hongguoCellFirstValues(values)
	result, err := downloader.hongguoCellRequest(ctx, hongguoCellTabPath, values, cookie)
	if err != nil {
		return page, err
	}
	tab, rows := hongguoCellTabRows(result)
	if len(tab) == 0 {
		return page, errors.New("红果行为推荐未返回推荐 tab")
	}
	dramas, impressions := hongguoCellDramas(rows)
	if len(rows) > 0 && len(dramas) == 0 {
		return page, errors.New("红果行为推荐未返回可播放的剧集")
	}
	// tab 顶层给 bookstore_id，cell_id_str 在带卡片的内层 cell 上（实测
	// 2026-11-05 响应如此），两处都试；两者是 cell/change 翻页必需的双 id。
	config := hongguoCellConfig{
		CellID: mapString(tab, "cell_id_str", "cell_id"),
		PlanID: mapString(tab, "bookstore_id"),
	}
	if config.CellID == "" {
		for _, cellItem := range anyList(tab["cell_data"]) {
			cell, _ := cellItem.(map[string]any)
			if id := mapString(cell, "cell_id_str", "cell_id"); id != "" {
				config.CellID = id
				break
			}
		}
	}
	hongguoCellSaveConfig(genre, config)
	hongguoCellRecordImpressions(genre, impressions)
	page.Dramas = dramas
	page.SessionID = mapString(tab, "session_id")
	page.NextOffset = 0 // 首次翻页 offset=0，之后按响应 next_offset 递进
	page.HasMore = true
	return page, ctx.Err()
}

// fetchHongguoCellChangePage 拉一批翻页：cell/change/v，回传曝光环与已见 ID。
func (downloader *Downloader) fetchHongguoCellChangePage(ctx context.Context, genre, cookie string, query hongguoRecommendationQuery) (hongguoRecommendationPage, error) {
	page := hongguoRecommendationPage{Dramas: []Drama{}}
	values := hongguoCellCommon()
	hongguoCellChangeValues(values, hongguoCellConfigFor(genre), query.SessionID, query.Offset,
		hongguoCellFilterIDs(query.Seen), hongguoCellRecentImpressions(genre))
	result, err := downloader.hongguoCellRequest(ctx, hongguoCellChangePath, values, cookie)
	if err != nil {
		return page, err
	}
	data, rows := hongguoCellChangeRows(result)
	if len(data) == 0 {
		return page, errors.New("红果行为推荐数据格式异常")
	}
	dramas, impressions := hongguoCellDramas(rows)
	if len(rows) > 0 && len(dramas) == 0 {
		return page, errors.New("红果行为推荐未返回可播放的剧集")
	}
	hongguoCellRecordImpressions(genre, impressions)
	page.Dramas = dramas
	page.SessionID = mapString(data, "session_id")
	if page.SessionID == "" {
		page.SessionID = query.SessionID
	}
	// has_more 恒 false 不可信（实测 false 时仍有 6 张全新卡片），以
	// next_offset 为游标；服务端没给就按本批卡片数递进，避免卡在同一批。
	page.NextOffset = query.Offset + len(dramas)
	if next := mapString(data, "next_offset"); next != "" {
		if parsed, err := strconv.Atoi(next); err == nil && parsed > query.Offset && parsed <= 1_000_000 {
			page.NextOffset = parsed
		}
	}
	page.HasMore = true
	return page, ctx.Err()
}

// fetchHongguoCellFeed 连取若干批，攒够目标条数或本批无新增为止。
// 与 tab/v1 的 fetchHongguoFeed 同策略：一次交互多取几批，避免点「继续推荐」
// 只多 1 部。中途某一批失败时，只要前面已取到剧集就正常返回。
func (downloader *Downloader) fetchHongguoCellFeed(ctx context.Context, query hongguoRecommendationQuery, cookie string) (hongguoRecommendationPage, error) {
	page := hongguoRecommendationPage{Dramas: []Drama{}}
	genre := query.Genre
	seen := make(map[string]bool, len(query.Seen))
	for _, id := range query.Seen {
		seen[strings.TrimPrefix(id, "hongguo:")] = true
	}
	next := query
	var lastErr error
	for batch := 0; batch < hongguoFeedBatchLimit; batch++ {
		var current hongguoRecommendationPage
		var err error
		if next.SessionID == "" {
			current, err = downloader.fetchHongguoCellFirstPage(ctx, genre, cookie)
		} else {
			current, err = downloader.fetchHongguoCellChangePage(ctx, genre, cookie, next)
		}
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
		page.SessionID, page.NextOffset = current.SessionID, current.NextOffset
		if added == 0 || len(page.Dramas) >= hongguoFeedTargetItems {
			break
		}
		next.Offset = current.NextOffset
		next.SessionID = current.SessionID
		next.Seen = next.Seen[:0]
		for id := range seen {
			next.Seen = append(next.Seen, "hongguo:"+id)
		}
	}
	if len(page.Dramas) == 0 && lastErr != nil {
		return hongguoRecommendationPage{}, lastErr
	}
	page.HasMore = true
	return page, ctx.Err()
}
