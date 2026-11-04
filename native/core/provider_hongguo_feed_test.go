package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// 推荐流（bookmall/tab/v1）解析边界：混排卡片、video_detail 包裹、
// content_type 过滤、seen 去重、session 携带。
func TestParseHongguoFeedPage(t *testing.T) {
	const payload = `{
	  "code": 0,
	  "message": "SUCCESS",
	  "data": {
	    "tab_item": [
	      {"tab_type": 7, "cell_data": []},
	      {"tab_type": 16, "session_id": "sess-abc", "cell_data": [
	        {"cell_name": "猜你喜欢", "cell_data": [
	          {"video_data": [
	            {"video_detail": {"series_id": 111, "series_title": "真人剧甲", "content_type": 1, "episode_cnt": 80, "first_vid": 9001}},
	            {"video_detail": {"series_id": 222, "series_title": "漫剧乙", "content_type": 1004, "episode_cnt": 60}}
	          ]},
	          {"video_data": [
	            {"series_id": "333", "series_title": "平铺丙", "content_type": "1004"},
	            {"series_id": 444, "series_title": "小说丁", "content_type": 2},
	            {"series_id": "abc", "series_title": "非法 ID", "content_type": 1004},
	            {"series_id": 111, "series_title": "重复甲", "content_type": 1}
	          ]}
	        ]}
	      ]}
	    ]
	  }
	}`
	var result map[string]any
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		t.Fatalf("测试数据无效: %v", err)
	}
	page, err := parseHongguoFeedPage(result, hongguoRecommendationQuery{Genre: hongguoFeedGenre})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(page.Dramas) != 3 {
		t.Fatalf("应保留 3 部（真人剧+漫剧+平铺，去掉小说/非法ID/重复），实际 %d: %v", len(page.Dramas), feedIDs(page.Dramas))
	}
	if page.Dramas[0].ID != "hongguo:111" || page.Dramas[1].ID != "hongguo:222" || page.Dramas[2].ID != "hongguo:333" {
		t.Fatalf("顺序或 ID 不符: %v", feedIDs(page.Dramas))
	}
	if page.SessionID != "sess-abc" {
		t.Fatalf("session 应透传，实际 %q", page.SessionID)
	}
	if !page.HasMore {
		t.Fatal("推荐流没有可靠结束标记，HasMore 必须为 true")
	}
	if page.NextOffset != 1 {
		t.Fatalf("首批 NextOffset 应为 1，实际 %d", page.NextOffset)
	}
}

func TestParseHongguoFeedPageFiltersSeen(t *testing.T) {
	const payload = `{"code":0,"data":{"tab_item":[{"tab_type":"16","session_id":"s","cell_data":[
	  {"cell_data":[{"video_data":[{"series_id":111,"series_title":"甲","content_type":1},
	                                {"series_id":222,"series_title":"乙","content_type":1004}]}]}]}]}}`
	var result map[string]any
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		t.Fatalf("测试数据无效: %v", err)
	}
	page, err := parseHongguoFeedPage(result, hongguoRecommendationQuery{Genre: hongguoFeedGenre, Offset: 3, Seen: []string{"hongguo:111"}})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(page.Dramas) != 1 || page.Dramas[0].ID != "hongguo:222" {
		t.Fatalf("应过滤已看过的 111，实际 %v", feedIDs(page.Dramas))
	}
	if page.NextOffset != 4 {
		t.Fatalf("NextOffset 应递增到 4，实际 %d", page.NextOffset)
	}
}

func TestParseHongguoFeedPageRejects(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"业务错误码", `{"code":123,"data":{}}`, "错误码 123"},
		{"缺少数据", `{"code":0}`, "格式异常"},
		{"缺少 tab16", `{"code":0,"data":{"tab_item":[{"tab_type":7}]}}`, "格式异常"},
		{"无卡片", `{"code":0,"data":{"tab_item":[{"tab_type":16,"cell_data":[]}]}}`, "未返回可播放"},
		{"只有非剧内容", `{"code":0,"data":{"tab_item":[{"tab_type":16,"cell_data":[{"cell_data":[{"video_data":[{"series_id":1,"content_type":2}]}]}]}]}}`, "未返回可播放"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			var result map[string]any
			if err := json.Unmarshal([]byte(item.payload), &result); err != nil {
				t.Fatalf("测试数据无效: %v", err)
			}
			_, err := parseHongguoFeedPage(result, hongguoRecommendationQuery{Genre: hongguoFeedGenre})
			if err == nil || !strings.Contains(err.Error(), item.want) {
				t.Fatalf("期望错误含 %q，实际 %v", item.want, err)
			}
		})
	}
}

// feed 是伪分类：校验要放行，但越界参数仍须拒绝。
func TestHongguoFeedQueryValidate(t *testing.T) {
	if err := (hongguoRecommendationQuery{Genre: hongguoFeedGenre}).validate(); err != nil {
		t.Fatalf("feed 应通过校验: %v", err)
	}
	if err := (hongguoRecommendationQuery{Genre: "feed", Offset: -1}).validate(); err == nil {
		t.Fatal("负 offset 应被拒绝")
	}
	if err := (hongguoRecommendationQuery{Genre: "feed", SessionID: "a\nb"}).validate(); err == nil {
		t.Fatal("含换行的 session 应被拒绝")
	}
	if err := (hongguoRecommendationQuery{Genre: "unknown"}).validate(); err == nil {
		t.Fatal("未知分类应被拒绝")
	}
}

func feedIDs(items []Drama) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ID)
	}
	return out
}

// [+178] 个性推荐 genre 与公开流共用 tab/v1：校验必须放行，但越界参数仍拒绝，
// 且绝不能被误当成 landpage 分类（否则会走到对匿名设备空 body 的老链路）。
func TestHongguoPersonalFeedGenreValidation(t *testing.T) {
	query := hongguoRecommendationQuery{Genre: hongguoPersonalFeedGenre, Offset: 2, SessionID: "abc"}
	if err := query.validate(); err != nil {
		t.Fatalf("个性推荐 genre 应通过校验: %v", err)
	}
	if err := (hongguoRecommendationQuery{Genre: hongguoPersonalFeedGenre, Offset: -1}).validate(); err == nil {
		t.Fatal("个性推荐的负 offset 也应被拒绝")
	}
	if !isHongguoFeedGenre(hongguoFeedGenre) || !isHongguoFeedGenre(hongguoPersonalFeedGenre) {
		t.Fatal("两个 feed genre 都应走 tab/v1")
	}
	for _, genre := range []string{"short_play", "comic_series", "ai_series", "unknown", ""} {
		if isHongguoFeedGenre(genre) {
			t.Fatalf("分类 genre %q 不应被当成 feed", genre)
		}
	}
}
