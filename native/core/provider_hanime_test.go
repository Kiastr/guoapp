package core

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Han1me 源的解析与接线测试。站点的 HTML 结构按 Han1meViewer（原版 + 重制版）
// 的选择器构造夹具：新版 horizontal-card、旧版 card-mobile-panel、首页横排
// home-rows-videos-div 三种卡片，以及详情页的 <video><source size> 多清晰度。
// 这些夹具不连外网：沙箱出口 IP 被站点按地区拦截，解析层只能离线回归。

const hanimeListFixture = `<html><body>
<div id="home-rows-wrapper">
  <a href="/watch?v=111"><div class="home-rows-videos-div">
    <img src="/images/simple1.jpg"><div class="home-rows-videos-title">简化卡样本</div>
  </div></a>
</div>
<div class="horizontal-card active">
  <a href="https://hanime1.me/watch?v=222">
    <div class="thumb-container">
      <img class="main-thumb" src="/images/cover2.jpg">
      <div class="duration">18:24</div>
      <div class="stat-item"><span>4.8</span></div>
      <div class="stat-item">12.3萬次</div>
    </div>
    <div class="title">新版卡片样本</div>
  </a>
</div>
<a href="/watch?v=333"></a>
<div class="card-mobile-panel">
  <img src="/images/lazy-placeholder.png">
  <img src="/images/cover3.jpg">
  <div class="card-mobile-title">旧版卡片样本</div>
  <div class="card-mobile-duration">07:30</div>
  <div class="card-mobile-duration">3.4萬次</div>
</div>
<div class="horizontal-card"><div class="title">广告位（没有 watch 链接）</div></div>
<div class="horizontal-card"><a href="/watch?v=222"><img src="/images/cover2.jpg"><div class="title">重复卡</div></a></div>
</body></html>`

func hanimeTestDownloader(t *testing.T, transport sourceFixtureTransport) *Downloader {
	t.Helper()
	d := sourceFixtureDownloader(t, transport)
	d.cfg.HanimeURL = "https://hanime.test"
	return d
}

func TestHanimeParsesCatalogCardsBothLayouts(t *testing.T) {
	base, _ := url.Parse("https://hanime1.me/")
	items := parseHanimeCards(hanimeListFixture, base, "里番")
	if len(items) != 3 {
		t.Fatalf("期望 3 张有效卡片（广告卡与重复卡应被丢弃），实际 %d：%+v", len(items), items)
	}
	byCode := map[string]Drama{}
	for _, item := range items {
		byCode[item.SourceID] = item
	}
	simple, ok := byCode["111"]
	if !ok || simple.Title != "简化卡样本" || simple.Cover != "https://hanime1.me/images/simple1.jpg" {
		t.Fatalf("简化卡解析错误：%+v", simple)
	}
	newCard, ok := byCode["222"]
	if !ok || newCard.Title != "新版卡片样本" || newCard.Remark != "18:24" || newCard.Views != "12.3萬次" {
		t.Fatalf("新版卡片解析错误：%+v", newCard)
	}
	oldCard, ok := byCode["333"]
	// 旧版卡片的封面是第二张图（第一张是懒加载占位图）。
	if !ok || oldCard.Title != "旧版卡片样本" || oldCard.Cover != "https://hanime1.me/images/cover3.jpg" ||
		oldCard.Remark != "07:30" || oldCard.Views != "3.4萬次" {
		t.Fatalf("旧版卡片解析错误：%+v", oldCard)
	}
	if oldCard.Category != "里番" || oldCard.ChannelName != "Han1me" || oldCard.ID != "hanime:333" {
		t.Fatalf("卡片公共字段错误：%+v", oldCard)
	}
}

const hanimeDetailFixture = `<html><body>
<input name="_token" value="token">
<h2 id="shareBtn-title">サンプル作品 第一話</h2>
<div class="video-details-wrapper">
  <div><div><div>觀看次數：12.3萬次 2026-01-02</div></div></div>
  <div>中文标题样本</div>
  <div class="video-caption-text-block">简介正文，含 <a href="#">链接</a> 与说明。</div>
  <div id="video-artist-name">制作商A</div><div>同人作品</div>
</div>
<div class="single-video-tag"><a href="/search?tags[]=x">#巨乳 (12)</a></div>
<div class="single-video-tag"><a href="/search?tags[]=y">#NTR</a></div>
<video id="player" poster="/images/poster.jpg" controls>
  <source src="https://vdownload.hanime1.me/720.mp4?token=a" size="720" type="video/mp4">
  <source src="https://vdownload.hanime1.me/1080.mp4?token=b" size="1080" type="video/mp4">
</video>
<div id="video-playlist-wrapper">
  <div id="playlist-top-block"><h4><a href="/search?query=x">系列名样本</a></h4></div>
  <div id="playlist-scroll">
    <div class="playlist-hover-wrap" data-href="/watch?v=111">
      <div class="thumb-container"><img class="main-thumb" src="/images/p1.jpg"><div class="duration">12:00</div></div>
      <h4 class="video-title"><a href="/watch?v=111">第一话</a></h4>
    </div>
    <div class="playlist-hover-wrap" data-href="/watch?v=222">
      <div class="thumb-container"><img class="main-thumb" src="/images/p2.jpg"><div class="duration">13:00</div></div>
      <h4 class="video-title"><a href="/watch?v=222">第二话</a></h4>
    </div>
  </div>
</div>
</body></html>`

func TestHanimeParsesDetailSourcesTagsAndSeries(t *testing.T) {
	base, _ := url.Parse("https://hanime1.me/")
	detail := parseHanimeDetail(hanimeDetailFixture, base)
	if detail.Title != "サンプル作品 第一話" || detail.ChineseTitle != "中文标题样本" {
		t.Fatalf("标题解析错误：%q / %q", detail.Title, detail.ChineseTitle)
	}
	if detail.Cover != "https://hanime1.me/images/poster.jpg" {
		t.Fatalf("封面解析错误：%q", detail.Cover)
	}
	if !strings.Contains(detail.Intro, "简介正文") || strings.Contains(detail.Intro, "链接") {
		t.Fatalf("简介应只取自有文本：%q", detail.Intro)
	}
	if detail.Artist != "制作商A" || detail.Genre != "同人作品" {
		t.Fatalf("制作商/类型解析错误：%q / %q", detail.Artist, detail.Genre)
	}
	if detail.Views != "12.3萬" || detail.UploadDate != "2026-01-02" {
		t.Fatalf("观看次数/日期解析错误：%q / %q", detail.Views, detail.UploadDate)
	}
	if len(detail.Tags) != 2 || detail.Tags[0] != "巨乳" || detail.Tags[1] != "NTR" {
		t.Fatalf("标签解析错误：%v", detail.Tags)
	}
	if len(detail.Sources) != 2 || detail.Sources[0].Quality != 720 || detail.Sources[1].Quality != 1080 {
		t.Fatalf("多清晰度解析错误：%+v", detail.Sources)
	}
	if detail.SeriesName != "系列名样本" || len(detail.Parts) != 2 ||
		detail.Parts[0].Code != "111" || detail.Parts[0].Title != "第一话" || detail.Parts[0].Duration != "12:00" {
		t.Fatalf("系列解析错误：%q %+v", detail.SeriesName, detail.Parts)
	}
}

func TestHanimeDetailChaptersIncludeCurrentVideoFirst(t *testing.T) {
	d := hanimeTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/watch" || request.URL.Query().Get("v") != "222" {
			t.Fatalf("详情请求参数错误：%s", request.URL.String())
		}
		return sourceFixtureResponse(request, 200, hanimeDetailFixture), nil
	})
	drama, chapters, err := d.fetchHanimeDetailChapters(context.Background(), "222")
	if err != nil {
		t.Fatal(err)
	}
	if drama.ID != "hanime:222" || len(chapters) != 2 {
		t.Fatalf("分集构造错误：%s %d %+v", drama.ID, len(chapters), chapters)
	}
	// 当前视频排第一，系列里与它同编号的一项被去重；每个分集都带自己的
	// videoCode 占位符，播放阶段按它重新抓详情页换直链。
	if chapters[0].ID != "hanime:222:222" || chapters[0].VideoURL != "hanime-play://222" ||
		chapters[1].ID != "hanime:222:111" || chapters[1].VideoURL != "hanime-play://111" {
		t.Fatalf("分集顺序/编号错误：%+v", chapters)
	}
	if drama.Episodes != 2 || drama.Tags[0] != "巨乳" || !strings.Contains(drama.Desc, "制作商：制作商A") {
		t.Fatalf("详情字段错误：%+v", drama)
	}
}

func TestHanimeCatalogSendsGenreSortAndCursor(t *testing.T) {
	var seen []string
	d := hanimeTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		seen = append(seen, request.URL.RequestURI())
		return sourceFixtureResponse(request, 200, hanimeListFixture), nil
	})
	items, more, err := d.fetchHanimeCatalogPage(context.Background(), 2, "裏番", "")
	if err != nil || len(items) != 3 || more {
		t.Fatalf("目录解析错误：%d %v %v", len(items), more, err)
	}
	if len(seen) != 1 || !strings.Contains(seen[0], "page=2") ||
		!strings.Contains(seen[0], "sort=%E6%9C%80%E6%96%B0%E4%B8%8A%E5%B8%82") ||
		!strings.Contains(seen[0], "genre=%E8%A3%8F%E7%95%AA") {
		t.Fatalf("目录请求参数错误：%v", seen)
	}
	if _, _, err := d.fetchHanimeCatalogPage(context.Background(), 1, "", strings.Repeat("x", hanimeMaxQuery+1)); err == nil {
		t.Fatal("超长关键词应被拒绝")
	}
}

func TestHanimeSearchUsesQueryParameter(t *testing.T) {
	var seen string
	d := hanimeTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		seen = request.URL.RawQuery
		return sourceFixtureResponse(request, 200, hanimeListFixture), nil
	})
	if _, _, err := d.fetchHanimeCatalogPage(context.Background(), 1, "", "样本"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "query=%E6%A0%B7%E6%9C%AC") || strings.Contains(seen, "genre=") || strings.Contains(seen, "sort=") {
		t.Fatalf("搜索请求应只带 query：%s", seen)
	}
}

func TestHanimeRankingBoardsUseSiteSortKeys(t *testing.T) {
	for _, board := range rankingBoards {
		if board.Source != sourceHanime {
			continue
		}
		if board.path == "" {
			t.Fatalf("Han1me 榜单 %s 缺少排序键", board.ID)
		}
	}
	board, found := findRankingBoard("hanime-weekly")
	if !found || board.path != "本週排行" {
		t.Fatalf("周榜定义错误：%+v", board)
	}
	var seen string
	d := hanimeTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		seen = request.URL.RawQuery
		return sourceFixtureResponse(request, 200, hanimeListFixture), nil
	})
	page, err := d.fetchHanimeRankingPage(context.Background(), board, 2)
	if err != nil || len(page.Items) != 3 || page.Items[0].Rank != hanimePageGuess+1 {
		t.Fatalf("榜单解析错误：%+v %v", page, err)
	}
	if !strings.Contains(seen, "sort=%E6%9C%AC%E9%80%B1%E6%8E%92%E8%A1%8C") {
		t.Fatalf("榜单没有带排序键：%s", seen)
	}
}

func TestHanimeResolvePicksHighestQualityAndKeepsVariants(t *testing.T) {
	d := hanimeTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Query().Get("v") != "222" {
			t.Fatalf("播放解析请求参数错误：%s", request.URL.String())
		}
		return sourceFixtureResponse(request, 200, hanimeDetailFixture), nil
	})
	media, err := d.resolveHanimeMedia(context.Background(), Task{
		DramaID: "hanime:222",
		Chapter: Chapter{ID: "hanime:222:222", Source: sourceHanime, VideoURL: "hanime-play://222"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if media.Quality != 1080 || !strings.Contains(media.URL, "1080.mp4") {
		t.Fatalf("主线路应取最高清晰度：%+v", media)
	}
	if len(media.Variants) != 1 || media.Variants[0].Quality != 720 {
		t.Fatalf("其余清晰度应作为候选线路：%+v", media.Variants)
	}
	if media.Referer != "https://hanime.test/watch?v=222" {
		t.Fatalf("Referer 应指向详情页：%s", media.Referer)
	}
}

func TestHanimeRejectsBadIdentity(t *testing.T) {
	if validHanimeCode("12/34") || validHanimeCode("") || !validHanimeCode("12ab-CD_3") {
		t.Fatal("videoCode 校验错误")
	}
	if validHanimeCategory("里番") || !validHanimeCategory("裏番") || !validHanimeCategory("") {
		t.Fatal("分类校验错误")
	}
	if _, _, valid := splitProviderDramaID("hanime:222"); !valid {
		t.Fatal("Han1me 剧集 ID 未被识别")
	}
	// 分集编号损坏时回落到剧集自身的 videoCode：单片视频的章节本来就只有一个。
	if code, err := hanimeChapterCode(Task{DramaID: "hanime:222", Chapter: Chapter{ID: "hanime:222:22/4"}}); err != nil || code != "222" {
		t.Fatalf("分集编号损坏时应回落到剧集编号：%q %v", code, err)
	}
	if _, err := hanimeChapterCode(Task{DramaID: "huangju:1"}); err == nil {
		t.Fatal("非 Han1me 剧集应被拒绝")
	}
}
