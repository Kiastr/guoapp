package core

// Kazumi 规则源测试（+155）。
//
// 全部用例走 sourceFixtureDownloader / sourceFixtureEngine（见 app_sources_test.go），
// 不发真实网络请求：上游随时可能改版，回归要靠固定夹具，验证真实可用性另做
// （开发时手动跑一遍上游真实请求）。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const kazumiFixtureDelimited = `{
  "api": "9", "type": "anime", "name": "giriGiriLove", "version": "2.6",
  "muliSources": true, "baseURL": "https://ani.example.test",
  "searchMode": "api", "chapterMode": "api",
  "searchApiConfig": {
    "request": {"method": "GET", "url": "https://api.example.test/api.php/Vod/get_list",
      "query": {"vod_name": "@keyword", "offset": "0", "limit": "24"}},
    "listPath": "$.info.rows[*]", "namePath": "$.vod_name", "sourcePath": "$.vod_id"
  },
  "chapterApiConfig": {
    "request": {"method": "GET", "url": "https://api.example.test/api.php/Vod/get_detail",
      "query": {"vod_id": "@source"}},
    "format": "delimited", "roadNamesPath": "$.info.vod_play_from",
    "roadEpisodesPath": "$.info.vod_play_url",
    "roadSeparator": "$$$", "episodeSeparator": "#", "fieldSeparator": "$"
  }
}`

const kazumiFixtureNested = `{
  "api": "8", "type": "anime", "name": "xfdmnext", "version": "1.1",
  "baseURL": "https://next.example.test/", "useWebview": true,
  "searchMode": "api", "chapterMode": "api",
  "searchApiConfig": {
    "request": {"method": "POST", "url": "https://api.example.test/rest/v1/rpc/search_animes",
      "headers": {"apikey": "fixture-key"},
      "bodyType": "json",
      "body": {"search_term": "@keyword", "page_number": 1, "items_per_page": 24}},
    "listPath": "$[*]", "namePath": "$.title", "sourcePath": "$.id"
  },
  "chapterApiConfig": {
    "request": {"method": "POST", "url": "https://api.example.test/rest/v1/rpc/get_anime_detail",
      "headers": {"apikey": "fixture-key"}, "bodyType": "json", "body": {"p_id": "@source"}},
    "format": "nested", "roadsPath": "$.sources[*]", "roadNamePath": "$.name",
    "episodesPath": "$.episodes[*]", "episodeNamePath": "$.title", "episodeUrlPath": "$.id",
    "episodePage": {"url": "https://next.example.test/anime/@source/play/@episodeUrl", "query": {}}
  }
}`

const kazumiFixtureXPathMode = `{"api":"7","name":"AGE","version":"1.6","baseURL":"https://x.test","searchMode":"api","chapterMode":"xpath"}`

// kazumiFixtureSite 把规则文本解析成站点（不经 store，单测只验引擎）。
func kazumiFixtureSite(t *testing.T, raw string) kazumiSite {
	t.Helper()
	var rule map[string]any
	if err := json.Unmarshal([]byte(raw), &rule); err != nil {
		t.Fatalf("规则夹具不是有效 JSON: %v", err)
	}
	parsed, err := kazumiRuleFromMap(rule)
	if err != nil {
		t.Fatalf("规则解析失败: %v", err)
	}
	return kazumiSite{
		Key:  kazumiKeyForRule(parsed.Name),
		Name: parsed.Name,
		Host: kazumiHostFromRule(parsed),
		File: kazumiRuleFile(kazumiKeyForRule(parsed.Name)),
		Rule: parsed,
		Raw:  rule,
	}
}

func TestKazumiRuleRejectsNonAPIMode(t *testing.T) {
	var rule map[string]any
	if err := json.Unmarshal([]byte(kazumiFixtureXPathMode), &rule); err != nil {
		t.Fatal(err)
	}
	if _, err := kazumiRuleFromMap(rule); err == nil {
		t.Fatal("非 api 模式规则应当被拒绝")
	} else if !strings.Contains(err.Error(), "api 模式") {
		t.Fatalf("错误信息应当说明原因，实际: %v", err)
	}
}

func TestKazumiSearchDelimitedPagination(t *testing.T) {
	site := kazumiFixtureSite(t, kazumiFixtureDelimited)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Errorf("该规则的搜索应当是 GET，实际 %s", request.Method)
		}
		// 第二页要把 offset 推进到 24（规则自带 offset/limit 键）。
		offset := request.URL.Query().Get("offset")
		if offset != "0" && offset != "24" {
			t.Errorf("意外的 offset=%q", offset)
		}
		if got := request.URL.Query().Get("vod_name"); got != "" {
			t.Errorf("空关键词场景 vod_name 应为空，实际 %q", got)
		}
		body := `{"code":1,"info":{"offset":0,"limit":24,"total":3795,"rows":[
			{"vod_id":27198,"vod_name":"黑色五叶草 第二季","vod_pic":"/upload/a.webp","vod_score":"10.0"},
			{"vod_id":27167,"vod_name":"野生的大魔王出现了！","vod_pic":"https://jpg.example.test/b.webp"}]}}`
		return sourceFixtureResponse(request, 200, body), nil
	})
	items, hasMore, err := d.kazumiSearch(context.Background(), site, "", 1)
	if err != nil {
		t.Fatalf("目录拉取失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 部剧，实际 %d", len(items))
	}
	if items[0].ID != "kazumi:giriGiriLove:27198" {
		t.Fatalf("剧集 ID 形态不符: %s", items[0].ID)
	}
	if items[0].Cover != "https://ani.example.test/upload/a.webp" {
		t.Fatalf("相对封面未按站点地址补全: %s", items[0].Cover)
	}
	if hasMore {
		t.Fatal("只有 2 条（不足满页）不该声明还有下一页")
	}
}

func TestKazumiDetailNestedUsesPlayPage(t *testing.T) {
	site := kazumiFixtureSite(t, kazumiFixtureNested)
	d := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Errorf("该规则应当是 POST，实际 %s", request.Method)
		}
		if got := request.Header.Get("apikey"); got != "fixture-key" {
			t.Errorf("自定义 header 未透传: %q", got)
		}
		_ = request.Body
		body := `{"anime":{"id":306,"title":"笨蛋，测验，召唤兽","cover_url":"https://img.test/a.jpg"},
		  "sources":[{"id":9,"code":"x","name":"主线","episodes":[
			{"id":4361,"title":"第 1 集"},{"id":4362,"title":"第 2 集"}]},
		   {"id":10,"code":"y","name":"备用","episodes":[{"id":4361,"title":"第 1 集"}]}]}`
		return sourceFixtureResponse(request, 200, body), nil
	})
	drama, chapters, err := d.kazumiDetail(context.Background(), site, "306")
	if err != nil {
		t.Fatalf("详情拉取失败: %v", err)
	}
	if drama.ID != "kazumi:xfdmnext:306" || drama.Title != "笨蛋，测验，召唤兽" {
		t.Fatalf("详情解析不符: %+v", drama)
	}
	// 分集值不是直链：应当拼成播放页（Dart 的 WebViewSniffer 负责嗅探）。
	if len(chapters) != 2 {
		t.Fatalf("备用线与主线重复地址应去重，实际 %d 集", len(chapters))
	}
	if chapters[0].VideoURL != "https://next.example.test/anime/306/play/4361" {
		t.Fatalf("播放页拼接不符: %s", chapters[0].VideoURL)
	}
	if chapters[0].Referer != "https://next.example.test/" {
		t.Fatalf("Referer 应取站点地址: %s", chapters[0].Referer)
	}
}

func TestKazumiStoreSeedsAndRemoves(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		return nil, http.ErrUseLastResponse
	})
	// 出厂内置两条（索引缺失时写入）。
	seeded := engine.downloader.kazumi.list()
	if len(seeded) != 2 {
		t.Fatalf("出厂应当有 2 条规则，实际 %d", len(seeded))
	}
	if _, ok := engine.downloader.kazumi.get(kazumiSourceID("giriGiriLove")); !ok {
		t.Fatal("缺少出厂内置 giriGiriLove")
	}
	// 删掉一条后重启（重载 store）不应复活。
	if err := engine.removeKazumiSite("giriGiriLove"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	reloaded := loadKazumiStore(engine.directory)
	if _, ok := reloaded.get(kazumiSourceID("giriGiriLove")); ok {
		t.Fatal("删除的规则在重载后又回来了")
	}
	if _, ok := reloaded.get(kazumiSourceID("xfdmnext")); !ok {
		t.Fatal("未删除的规则应当在重载后仍在")
	}
}

func TestKazumiImportRejectsInvalidRule(t *testing.T) {
	engine := sourceFixtureEngine(t, func(request *http.Request) (*http.Response, error) {
		return nil, http.ErrUseLastResponse
	})
	if _, err := engine.importKazumiRule(context.Background(), "not-json", ""); err == nil {
		t.Fatal("非 JSON 应当报错")
	}
	if _, err := engine.importKazumiRule(context.Background(), kazumiFixtureXPathMode, ""); err == nil {
		t.Fatal("非 api 模式规则应当被拒绝")
	}
	if _, err := engine.importKazumiRule(context.Background(), "", ""); err == nil {
		t.Fatal("空内容应当报错")
	}
}

func TestKazumiLooksLikeMediaAddress(t *testing.T) {
	cases := map[string]bool{
		"https://a.test/x/01.mp4":                     true,
		"https://a.test/x/index.m3u8?token=1":         true,
		"https://a.test/anime/306/play/4361":          false,
		"https://a.test/x/01.mp4download":             false,
		"https://a.test/play?url=https%3A%2F%2F.m3u8": false,
	}
	for address, expected := range cases {
		if got := kazumiLooksLikeMedia(address); got != expected {
			t.Errorf("%s 期望 %v，实际 %v", address, expected, got)
		}
	}
}

func TestKazumiSourceIDRoundTrip(t *testing.T) {
	if kazumiKeyFromSource("kazumi:giriGiriLove") != "giriGiriLove" {
		t.Fatal("key 反解失败")
	}
	if kazumiKeyFromSource("libretv:abc") != "" {
		t.Fatal("其它家族不该被识别成 Kazumi")
	}
	source, sourceID, ok := splitProviderDramaID("kazumi:giriGiriLove:27198")
	if !ok || source != "kazumi:giriGiriLove" || sourceID != "27198" {
		t.Fatalf("剧集 ID 切分失败: %q %q %v", source, sourceID, ok)
	}
	if !isHuangguoProviderSource(kazumiSourceID("xfdmnext")) {
		t.Fatal("Kazumi 家族源应当通过源合法性校验")
	}
}
