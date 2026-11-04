package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// 行为推荐（+179）的两段式解析、曝光环与参数构造。
// 端点实测结论见 provider_hongguo_cell_feed.go 顶部注释；这里只跑离线
// fixture，不发网络请求。

const testCellGenre = "feed_behavior_unit_test"

func clearCellTestState(genre string) {
	hongguoCellMu.Lock()
	delete(hongguoCellConfigs, genre)
	delete(hongguoCellImpressions, genre)
	hongguoCellMu.Unlock()
}

func TestHongguoCellFeedGenreValidation(t *testing.T) {
	base := hongguoRecommendationQuery{Genre: hongguoCellFeedGenre, Offset: 6, SessionID: "sess-1", Seen: []string{"hongguo:123456"}}
	if err := base.validate(); err != nil {
		t.Fatalf("行为推荐 genre 应放行: %v", err)
	}
	if !isHongguoCellFeedGenre(hongguoCellFeedGenre) {
		t.Fatal("分流判定失效")
	}
	if isHongguoFeedGenre(hongguoCellFeedGenre) || isHongguoCellFeedGenre(hongguoFeedGenre) || isHongguoCellFeedGenre(hongguoPersonalFeedGenre) {
		t.Fatal("行为推荐不得与 tab/v1 两档串线")
	}
	bad := []hongguoRecommendationQuery{
		{Genre: hongguoCellFeedGenre, Offset: -1},
		{Genre: hongguoCellFeedGenre, Offset: 1_000_001},
		{Genre: hongguoCellFeedGenre, SessionID: strings.Repeat("x", 4097)},
		{Genre: hongguoCellFeedGenre, SessionID: "bad\nvalue"},
		{Genre: hongguoCellFeedGenre, Seen: []string{"hongguo:not-a-number"}},
	}
	for i, query := range bad {
		if err := query.validate(); err == nil {
			t.Fatalf("越界参数 %d 应被拒绝: %+v", i, query)
		}
	}
}

func TestHongguoCellTabRows(t *testing.T) {
	const payload = `{
	  "code": 0, "message": "SUCCESS",
	  "data": {"tab_item": [
	    {"tab_type": 7, "cell_data": []},
	    {"tab_type": 16, "session_id": "sess-tab-16", "bookstore_id": "7294257258253254693",
	     "cell_data": [{"cell_id_str": "7294257141911650341", "cell_data": [
	       {"video_data": [
	         {"series_id": "111", "recommend_group_id": "111", "video_detail": {"series_id_str": "111", "series_title": "甲", "content_type": 1}},
	         {"series_id": "222", "recommend_group_id": "222", "video_detail": {"series_id_str": "222", "series_title": "乙", "content_type": 1004}}
	       ]}
	     ]}]}
	  ]}
	}`
	var result map[string]any
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		t.Fatal(err)
	}
	tab, rows := hongguoCellTabRows(result)
	if len(tab) == 0 || len(rows) != 2 {
		t.Fatalf("应取 tab=16 的 2 张卡片，实际 tab=%v rows=%d", len(tab), len(rows))
	}
	if mapString(tab, "session_id") != "sess-tab-16" || mapString(tab, "bookstore_id") != hongguoCellFallbackPlanID {
		t.Fatalf("服务端下发配置未解析: %v", tab)
	}
	dramas, impressions := hongguoCellDramas(rows)
	if len(dramas) != 2 || len(impressions) != 2 {
		t.Fatalf("剧集/曝光应各 2 条，实际 %d / %d", len(dramas), len(impressions))
	}
	if dramas[0].ID != "hongguo:111" || dramas[0].Title != "甲" {
		t.Fatalf("剧集字段映射错误: %+v", dramas[0])
	}
}

func TestHongguoCellChangeRows(t *testing.T) {
	const payload = `{
	  "code": 0, "message": "SUCCESS",
	  "data": {"has_more": false, "next_offset": 6, "session_id": "sess-change",
	    "cell_view": {"cell_data": [
	      {"video_data": [{"series_id": "333", "video_detail": {"series_id_str": "333", "series_title": "丙", "content_type": 1}}]},
	      {"video_data": [{"series_id": "444", "video_detail": {"series_id_str": "444", "series_title": "小说", "content_type": 2}}]}
	    ]}
	  }
	}`
	var result map[string]any
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		t.Fatal(err)
	}
	data, rows := hongguoCellChangeRows(result)
	if mapString(data, "next_offset") != "6" || len(rows) != 2 {
		t.Fatalf("翻页响应解析错误: next=%q rows=%d", mapString(data, "next_offset"), len(rows))
	}
	dramas, _ := hongguoCellDramas(rows)
	if len(dramas) != 1 || dramas[0].ID != "hongguo:333" {
		t.Fatalf("content_type=2 小说应被过滤，实际 %v", cellDramaIDs(dramas))
	}
}

func cellDramaIDs(dramas []Drama) []string {
	out := make([]string, 0, len(dramas))
	for _, drama := range dramas {
		out = append(out, drama.ID)
	}
	return out
}

func TestHongguoCellImpressionOf(t *testing.T) {
	// 形态一：参考实现的 cell_extra_data.rec_filter_info（优先）。
	legacy := map[string]any{
		"cell_extra_data": map[string]any{"rec_filter_info": map[string]any{
			"content_type": "1", "rec_group_id": 987,
			"filter_id_map": map[string]any{"rank": "2"},
		}},
	}
	if impression, ok := hongguoCellImpressionOf(legacy); !ok || impression.RecGroupID != 987 || impression.ContentType != "1" {
		t.Fatalf("参考实现字段未优先: %+v %v", impression, ok)
	}
	// 形态二：实测 2026-11-05 的 recommend_group_id + recommend_info。
	current := map[string]any{
		"series_id": "7701935931787316286", "recommend_group_id": "7701935931787316286",
		"recommend_info": `{"channel_id":"94349633554","request_id":"req-1","rank":"2"}`,
		"video_detail":   map[string]any{"content_type": float64(1)},
	}
	impression, ok := hongguoCellImpressionOf(current)
	if !ok || impression.RecGroupID != 7701935931787316286 || impression.ContentType != "1" {
		t.Fatalf("实测形态未适配: %+v %v", impression, ok)
	}
	if impression.FilterIDMap["channel_id"] != "94349633554" || impression.FilterIDMap["rank"] != "2" {
		t.Fatalf("recommend_info 未并入 filter_id_map: %v", impression.FilterIDMap)
	}
	if _, ok := hongguoCellImpressionOf(map[string]any{"series_id": ""}); ok {
		t.Fatal("无合法 group id 时不应产出曝光记录")
	}
}

func TestHongguoCellImpressionRing(t *testing.T) {
	clearCellTestState(testCellGenre)
	t.Cleanup(func() { clearCellTestState(testCellGenre) })
	for i := int64(1); i <= 12; i++ {
		hongguoCellRecordImpressions(testCellGenre, []hongguoCellImpression{{RecGroupID: i, ContentType: "1"}})
	}
	// 去重：重复写入同一 gid 不应增加长度。
	hongguoCellRecordImpressions(testCellGenre, []hongguoCellImpression{{RecGroupID: 12}})
	ring := hongguoCellRecentImpressions(testCellGenre)
	if len(ring) != hongguoCellImpressionCap {
		t.Fatalf("曝光环容量应为 %d，实际 %d", hongguoCellImpressionCap, len(ring))
	}
	if ring[0].RecGroupID != 5 || ring[len(ring)-1].RecGroupID != 12 {
		t.Fatalf("应保留最近 8 条，实际首=%d 末=%d", ring[0].RecGroupID, ring[len(ring)-1].RecGroupID)
	}
}

func TestHongguoCellFilterIDs(t *testing.T) {
	seen := []string{"hongguo:111", "hongguo:222", "not-numeric"}
	if got := hongguoCellFilterIDs(seen); got != "111,222" {
		t.Fatalf("前缀裁剪/非法 ID 过滤错误: %q", got)
	}
	long := make([]string, 0, 250)
	for i := 0; i < 250; i++ {
		long = append(long, "hongguo:1000")
	}
	if got := hongguoCellFilterIDs(long); len(strings.Split(got, ",")) != hongguoCellFilterIDLimit {
		t.Fatalf("filter_ids 应截断到 %d 个", hongguoCellFilterIDLimit)
	}
}

func TestHongguoCellRequestValues(t *testing.T) {
	first := hongguoCellCommon()
	hongguoCellFirstValues(first)
	if first.Get("client_req_type") != "1028" || first.Get("tab_type") != "16" || first.Get("need_personal_recommend") != "1" {
		t.Fatalf("首批参数不齐: cr=%q tab=%q personal=%q", first.Get("client_req_type"), first.Get("tab_type"), first.Get("need_personal_recommend"))
	}
	if first.Get("cold_start_session_id") == "" || first.Get("normal_session_id") != first.Get("cold_start_session_id")+"#1" {
		t.Fatal("会话 UUID 两形态不一致")
	}

	clearCellTestState(testCellGenre)
	t.Cleanup(func() { clearCellTestState(testCellGenre) })
	imps := []hongguoCellImpression{{ContentType: "1", FilterIDMap: map[string]any{"rank": "2"}, RecGroupID: 555}}
	change := hongguoCellCommon()
	hongguoCellChangeValues(change, hongguoCellConfig{CellID: "cell-1", PlanID: "plan-1"}, "sess-9", 6, "111,222", imps)
	if change.Get("client_req_type") != "2" || change.Get("cell_id") != "cell-1" || change.Get("plan_id") != "plan-1" {
		t.Fatal("翻页双 id / client_req_type 错误")
	}
	if change.Get("session_id") != "sess-9" || change.Get("filter_ids") != "111,222" || change.Get("offset") != "6" {
		t.Fatal("翻页游标 / 已见 ID 回传错误")
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(change.Get("rec_filter_info_list")), &list); err != nil || len(list) != 1 {
		t.Fatalf("rec_filter_info_list 格式错误: %s %v", change.Get("rec_filter_info_list"), err)
	}
	var refresh map[string]any
	if err := json.Unmarshal([]byte(change.Get("refresh_action_info")), &refresh); err != nil {
		t.Fatal(err)
	}
	if refresh["latest_impression_gids"] != "555" {
		t.Fatalf("曝光 gid 未回传: %v", refresh)
	}
	// 空会话 / 空排除表不应带对应键。
	empty := hongguoCellCommon()
	hongguoCellChangeValues(empty, hongguoCellConfig{CellID: "c", PlanID: "p"}, "", 0, "", nil)
	if _, ok := empty["session_id"]; ok {
		t.Fatal("空会话不应带 session_id")
	}
	if _, ok := empty["filter_ids"]; ok {
		t.Fatal("空排除表不应带 filter_ids")
	}
}
