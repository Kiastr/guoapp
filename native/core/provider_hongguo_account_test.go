package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func hongguoAccountTestDownloader() *Downloader {
	return &Downloader{
		client:  &http.Client{},
		limiter: newRequestLimiterWithExternal(3, 3, 8, time.Millisecond),
	}
}

func hongguoAccountTestDevice() hongguoAccountDevice {
	return hongguoAccountDevice{
		DeviceID:  "7300000000000000001",
		InstallID: "7300000000000000002",
		CDID:      "0b9b9f5e-6a4b-4f0e-9a4f-2f5f0e6a4b9f",
	}
}

// TestHongguoAccountFavoritesParsesShelf 书架返回嵌套 video_data 时字段要能取全。
func TestHongguoAccountFavoritesParsesShelf(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reading/bookapi/bookshelf/video/info/v1/" {
			t.Errorf("路径不对：%s", r.URL.Path)
		}
		if r.Header.Get("Cookie") != "sessionid=fixture" {
			t.Errorf("Cookie 没带上：%q", r.Header.Get("Cookie"))
		}
		for _, name := range []string{"x-gorgon", "x-khronos", "x-argus", "x-ladon", "x-helios", "x-medusa"} {
			if r.Header.Get(name) == "" {
				t.Errorf("缺签名头 %s", name)
			}
		}
		if r.URL.Query().Get("cdid") == "" {
			t.Error("query 缺 cdid")
		}
		if version := r.URL.Query().Get("version_code"); version != hongguoAccountReadVersionCode {
			t.Errorf("读请求版本号应为 %s，实际 %s", hongguoAccountReadVersionCode, version)
		}
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": {
				"video_shelf_info": [
					{
						"book_id": "7300000000000000001",
						"series_title": "示例剧",
						"series_cover_url": "//p3.example.com/cover.webp",
						"series_intro": "一句话简介",
						"collect_time": 1730000000,
						"category_schema": "[{\"name\":\"都市\"},{\"name\":\"逆袭\"}]",
						"video_data": {"episode_cnt": 80, "video_desc": "嵌套简介"}
					},
					{
						"book_id": "7300000000000000003",
						"series_title": "另一部",
						"collect_time": 1740000000000
					},
					{
						"book_id": "不是数字",
						"series_title": "坏数据"
					}
				]
			}
		}`))
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), hongguoAccountBaseKey{}, server.URL)
	items, err := hongguoAccountTestDownloader().hongguoAccountFavorites(ctx, hongguoAccountTestDevice(), "sessionid=fixture")
	if err != nil {
		t.Fatalf("拉收藏失败：%v", err)
	}
	if len(items) != 2 {
		t.Fatalf("应过滤掉坏数据，实际 %d 条：%+v", len(items), items)
	}
	first := items[0]
	if first.SeriesID != "7300000000000000003" {
		t.Fatalf("应按收藏时间倒序，首条实际 %+v", first)
	}
	second := items[1]
	if second.Title != "示例剧" || second.Episodes != 80 {
		t.Errorf("标题或集数解析错误：%+v", second)
	}
	if second.Cover != "https://p3.example.com/cover.webp" {
		t.Errorf("协议相对封面没有补全：%q", second.Cover)
	}
	if second.Category != "都市 · 逆袭" {
		t.Errorf("题材解析错误：%q", second.Category)
	}
	if second.CollectedAt != 1730000000000 {
		t.Errorf("秒级收藏时间没有转毫秒：%d", second.CollectedAt)
	}
	if second.Description != "一句话简介" {
		t.Errorf("简介解析错误：%q", second.Description)
	}
}

// TestHongguoAccountHistoryParsesProgress 进度接口的毫秒字段要转秒，并用详情补标题。
func TestHongguoAccountHistoryParsesProgress(t *testing.T) {
	detailCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reading/bookapi/read_progress/list/v1/":
			if got := r.URL.Query().Get("book_types"); got != "2" {
				t.Errorf("book_types 应为 2，实际 %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Errorf("limit 应为 50，实际 %q", got)
			}
			// video_data 与 data 平级：这个形态在实测里出现过，必须兜住。
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [],
				"video_data": [
					{
						"book_id": "7300000000000000001",
						"vid": "7400000000000000001",
						"vid_index": 12,
						"current_play_position": 125000,
						"duration": 600000,
						"read_timestamp_ms": 1730000000000
					}
				]
			}`))
		case "/reading/bookapi/multi-detail/v1/":
			detailCalled = true
			if got := r.URL.Query().Get("book_id"); got != "7300000000000000001" {
				t.Errorf("补详情 book_id 不对：%q", got)
			}
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": [
					{
						"book_id": "7300000000000000001",
						"book_name": "补回来的标题",
						"thumb_url": "https://p3.example.com/detail.webp",
						"abstract": "补回来的简介",
						"episode_cnt": 66
					}
				]
			}`))
		default:
			t.Errorf("意外请求：%s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), hongguoAccountBaseKey{}, server.URL)
	items, err := hongguoAccountTestDownloader().hongguoAccountHistory(ctx, hongguoAccountTestDevice(), "sessionid=fixture")
	if err != nil {
		t.Fatalf("拉历史失败：%v", err)
	}
	if !detailCalled {
		t.Error("没有调用 multi-detail 补资料")
	}
	if len(items) != 1 {
		t.Fatalf("应有 1 条，实际 %d", len(items))
	}
	item := items[0]
	if item.Episode != 12 || item.EpisodeID != "7400000000000000001" {
		t.Errorf("集号解析错误：%+v", item)
	}
	if item.Position != 125 || item.Duration != 600 {
		t.Errorf("毫秒没有转秒：position=%v duration=%v", item.Position, item.Duration)
	}
	if item.Title != "补回来的标题" || item.Cover == "" || item.Episodes != 66 {
		t.Errorf("详情没有合并进来：%+v", item)
	}
	if item.UpdatedAt != 1730000000000 {
		t.Errorf("更新时间解析错误：%d", item.UpdatedAt)
	}
}

// TestHongguoAccountRequestReportsCode 服务端错误码要带出来，不能当成「空数据」。
func TestHongguoAccountRequestReportsCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1001,"message":"登录态已失效"}`))
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), hongguoAccountBaseKey{}, server.URL)
	_, err := hongguoAccountTestDownloader().hongguoAccountFavorites(ctx, hongguoAccountTestDevice(), "sessionid=fixture")
	if err == nil {
		t.Fatal("错误码应报错")
	}
	if !strings.Contains(err.Error(), "登录态已失效") {
		t.Errorf("错误文案没带服务端消息：%v", err)
	}
}

// TestHongguoAccountSyncWithoutLogin 未登录要返回 SignedIn=false，而不是报错。
func TestHongguoAccountSyncWithoutLogin(t *testing.T) {
	engine := &nativeEngine{directory: t.TempDir()}
	snapshot, err := engine.hongguoAccountSync(context.Background())
	if err != nil {
		t.Fatalf("未登录不该报错：%v", err)
	}
	if snapshot.SignedIn {
		t.Fatal("未登录却返回 SignedIn=true")
	}
	if len(snapshot.Favorites) != 0 || len(snapshot.History) != 0 {
		t.Fatal("未登录不该带数据")
	}
}

// TestHongguoAccountDeviceRejectsRandom 设备身份三件套缺一不可。
func TestHongguoAccountDeviceRejectsRandom(t *testing.T) {
	cases := []hongguoAccountDevice{
		{DeviceID: "", InstallID: "7300000000000000002", CDID: "0b9b9f5e-6a4b"},
		{DeviceID: "7300000000000000001", InstallID: "", CDID: "0b9b9f5e-6a4b"},
		{DeviceID: "7300000000000000001", InstallID: "7300000000000000002", CDID: ""},
		{DeviceID: "abc", InstallID: "7300000000000000002", CDID: "0b9b9f5e-6a4b"},
	}
	for index, device := range cases {
		if device.valid() {
			t.Errorf("第 %d 组不该通过：%+v", index, device)
		}
	}
	if !hongguoAccountTestDevice().valid() {
		t.Fatal("标准设备身份应通过")
	}
}

// TestHongguoAccountQueryKeepsOrder 签名对 query 字节敏感，顺序必须稳定。
func TestHongguoAccountQueryKeepsOrder(t *testing.T) {
	device := hongguoAccountTestDevice()
	first := hongguoAccountQuery(nil, device, hongguoAccountReadVersionCode, hongguoAccountReadVersionName, 1730000000, 1730000000000)
	second := hongguoAccountQuery(nil, device, hongguoAccountReadVersionCode, hongguoAccountReadVersionName, 1730000000, 1730000000000)
	if first != second {
		t.Fatal("同样输入产出不同 query")
	}
	for _, fragment := range []string{
		"version_code=72232",
		"version_name=7.2.2.32",
		"iid=7300000000000000002",
		"device_id=7300000000000000001",
		"cdid=0b9b9f5e-6a4b-4f0e-9a4f-2f5f0e6a4b9f",
		"ts=1730000000",
	} {
		if !strings.Contains(first, fragment) {
			t.Errorf("query 缺 %s：%s", fragment, first)
		}
	}
	writeQuery := hongguoAccountQuery(nil, device, hongguoAccountWriteVersionCode, hongguoAccountWriteVersionName, 1730000000, 1730000000000)
	if !strings.Contains(writeQuery, "version_code=73932") || !strings.Contains(writeQuery, "version_name=7.3.9.32") {
		t.Errorf("写请求画像没生效：%s", writeQuery)
	}
}

// TestHongguoAccountSnapshotJSON 快照字段名与 Dart 侧约定一致。
func TestHongguoAccountSnapshotJSON(t *testing.T) {
	raw, err := json.Marshal(hongguoAccountSnapshot{
		SignedIn:  true,
		Favorites: []hongguoAccountItem{{SeriesID: "1", Title: "剧"}},
		SyncedAt:  1730000000,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, key := range []string{`"signedIn":true`, `"favorites"`, `"syncedAt"`, `"seriesId"`} {
		if !strings.Contains(text, key) {
			t.Errorf("JSON 缺 %s：%s", key, text)
		}
	}
}
