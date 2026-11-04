package core

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

type hongguoRecommendationQuery struct {
	Genre     string   `json:"genre"`
	Offset    int      `json:"offset"`
	SessionID string   `json:"sessionId"`
	Seen      []string `json:"seen"`
}

type hongguoRecommendationPage struct {
	Dramas     []Drama `json:"data"`
	NextOffset int     `json:"nextOffset"`
	SessionID  string  `json:"sessionId"`
	HasMore    bool    `json:"hasMore"`
	Saved      bool    `json:"saved"`
}

func (query hongguoRecommendationQuery) validate() error {
	// [feed] 推荐流是独立的一路：它没有 genre 维度（返回的是混排信息流），
	// 用伪分类键 feed / feed_personal / feed_behavior 与三个真实分类区分。
	// 前两者共用 provider_hongguo_feed.go 的 tab/v1 解析（匿名 / 账号 Cookie），
	// feed_behavior 走 provider_hongguo_cell_feed.go 的 tab/v + cell/change
	// 两段式（+179）。三者都按同一套分页参数校验，避免缓存恢复时被判无效。
	if isHongguoFeedGenre(query.Genre) || isHongguoCellFeedGenre(query.Genre) {
		if query.Offset < 0 || query.Offset > 1_000_000 || len(query.SessionID) > 4096 || strings.ContainsAny(query.SessionID, "\r\n\x00") || len(query.Seen) > 540 {
			return errors.New("推荐分页参数无效，请重新获取")
		}
		for _, id := range query.Seen {
			if !hongguoNumericID.MatchString(strings.TrimPrefix(id, "hongguo:")) {
				return errors.New("推荐分页含无效剧集 ID")
			}
		}
		return nil
	}
	known := false
	for _, genre := range hongguoAppGenres {
		known = known || query.Genre == genre.key
	}
	if !known || query.Offset < 0 || query.Offset > 1_000_000 || len(query.SessionID) > 4096 || strings.ContainsAny(query.SessionID, "\r\n\x00") || len(query.Seen) > 540 {
		return errors.New("推荐分类或分页参数无效，请重新获取")
	}
	for _, id := range query.Seen {
		if !hongguoNumericID.MatchString(strings.TrimPrefix(id, "hongguo:")) {
			return errors.New("推荐分页含无效剧集 ID")
		}
	}
	return nil
}

func (downloader *Downloader) fetchHongguoRecommendations(ctx context.Context, query hongguoRecommendationQuery, cookie string) (hongguoRecommendationPage, error) {
	page := hongguoRecommendationPage{Dramas: []Drama{}}
	if err := query.validate(); err != nil {
		return page, err
	}
	// [+179] 行为推荐走两段式 tab/v?cr=1028 + cell/change/v，与下面两档
	// 的 tab/v1 单端点完全隔离：它有自己的会话、双 id 与曝光环（内存态），
	// 出问题时切回「公开推荐」即可，不影响另外两档。
	if isHongguoCellFeedGenre(query.Genre) {
		return downloader.fetchHongguoCellFeed(ctx, query, cookie)
	}
	// [feed] 推荐流走 bookmall/tab/v1，与 landpage 分类推荐不是同一条链路：
	// 后者对匿名设备空 body，前者可用且 offset 递增即「换一批」。cookie
	// 非空时由 feed_personal 一路带上账号 Cookie（个性推荐）。
	if isHongguoFeedGenre(query.Genre) {
		return downloader.fetchHongguoFeed(ctx, query, cookie)
	}
	scene, category := "", ""
	for _, genre := range hongguoAppGenres {
		if genre.key == query.Genre {
			scene, category = genre.scene, genre.name
		}
	}
	seen := make(map[string]bool, len(query.Seen))
	filterIDs := make([]string, 0, len(query.Seen))
	for _, id := range query.Seen {
		id = strings.TrimPrefix(id, "hongguo:")
		if !seen[id] {
			filterIDs = append(filterIDs, id)
			seen[id] = true
		}
	}
	payload := map[string]any{
		"req_scene": scene, "offset": query.Offset, "limit": 18,
		"req_type": "only_content", "need_selector_panel": false, "client_req_type": 3,
		"session_id": query.SessionID, "filter_ids": strings.Join(filterIDs, ","),
		"select_items": map[string]any{
			"genre": []string{query.Genre}, "sort": []string{}, "gender": []string{},
			"category_dim_theme": []string{}, "category_dim_role": []string{}, "category_dim_epoch": []string{},
			"online_time": []string{}, "creation_status": []string{},
		},
	}
	if query.Offset > 0 {
		payload["client_req_type"] = 2
	}
	result, err := downloader.hongguoAppRequest(ctx, http.MethodPost, "/reading/distribution/category/landpage/v/", nil, payload)
	if err != nil {
		return page, err
	}
	data := nestedMap(result, "data")
	rows, valid := data["video_data"].([]any)
	hasMore, paginationOK := data["has_more"].(bool)
	next, parseErr := strconv.Atoi(mapString(data, "next_offset"))
	if !valid || !paginationOK || hasMore && parseErr != nil {
		return page, errors.New("红果推荐数据格式异常，已保留上次位置")
	}
	validRows := 0
	for _, row := range rows {
		drama := hongguoDramaFromAny(row, category)
		if drama.ID != "" {
			validRows++
		}
		id := strings.TrimPrefix(drama.ID, "hongguo:")
		if drama.ID == "" || seen[id] {
			continue
		}
		seen[id] = true
		page.Dramas = append(page.Dramas, drama)
	}
	if len(rows) > 0 && validRows == 0 {
		return hongguoRecommendationPage{}, errors.New("红果推荐未返回可识别的剧集")
	}
	if hasMore && (next <= query.Offset || next > 1_000_000 || len(page.Dramas) == 0) {
		return hongguoRecommendationPage{}, errors.New("红果推荐分页未前进，可重试或重新获取")
	}
	page.NextOffset, page.HasMore, page.SessionID = next, hasMore, mapString(data, "session_id")
	if !hasMore && (next < query.Offset || next > 1_000_000) {
		page.NextOffset = query.Offset
	}
	if len(page.SessionID) > 4096 || strings.ContainsAny(page.SessionID, "\r\n\x00") {
		return hongguoRecommendationPage{}, errors.New("红果推荐分页标记无效")
	}
	return page, ctx.Err()
}
