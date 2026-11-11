package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Han1me（hanime1.me）原生站源。
//
// 移植自 GitHub 上的 Han1meViewer（YenalyLiew/Han1meViewer，及其社区重制版
// Han1meViewer-Remastered-Org/Han1meViewer-Remastered）：端点、查询参数、
// 卡片/详情页字段、多清晰度取流方式都对齐参考实现，另外把它的「专属适配」
// 一并搬了过来：
//
//   - 分类维度：站点的 genre 枚举（里番 / 泡面番 / Motion Anime / 3DCG /
//     2.5D / 2D動畫 / AI生成 / MMD / Cosplay）；
//   - 排行榜：站点的 sort 枚举里带排行的几档（本日 / 本週 / 本月 / 观看次数 /
//     讚好比例 / 他們在看），注册成本项目的榜单；
//   - 详情页：中文标题、简介、标签、制作商、发售日期、观看次数；
//   - 系列（playlist）：一个视频页上若挂着系列播放列表，整列映射成分集；
//   - 播放：<video id="player"> 下的多 <source size=...> 按清晰度映射成线路。
//
// 上游接口（参考实现，站点为服务端渲染的 HTML）：
//
//	GET /search?page=&genre=&sort=&query=     列表 / 搜索 / 排行榜
//	GET /watch?v=<videoCode>                  详情 + 播放地址 + 系列
//
// 注意：站点在 Cloudflare 后面，按出口 IP 地区做限制（日本 IP 会被
// 403 "you have been blocked"）。这个限制在请求层，不在解析层：只要能打开
// 网页，这里就能取到数据；打不开时错误信息会提示走代理。
//
// 本站在项目里属于敏感站源（与黄果同档）：Dart 侧 SourceSite.restrictedValues
// 默认隐藏，解锁密码后才出现在片源选择里。

const (
	hanimeBaseURL   = "https://hanime1.me"
	hanimeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	// hanimeDefaultSort 列表默认排序。参考实现的首页「最新上市」就是它，
	// 分类浏览没有单独的时间维度，换排序靠排行榜那几档。
	hanimeDefaultSort = "最新上市"

	// hanimePlayScheme 分集占位符：真实 mp4/m3u8 地址在播放阶段按 videoCode
	// 重新抓详情页换取（与 sorani-play:// 同一套做法，避免详情页展开时就
	// 把整列系列全部请求一遍）。
	hanimePlayScheme = "hanime-play://"

	hanimeMaxPage   = 500
	hanimeMaxCode   = 32
	hanimeMaxQuery  = 128
	hanimePageGuess = 20
)

// hanimeGenres 对齐参考实现 assets/search_options/genre.json（重制版）。
// ID 是站点真实下发的 search_key，含空格与大小写，不能改写成拼音或中文。
var hanimeGenres = []nativeCategory{
	{ID: "裏番", Name: "里番"},
	{ID: "泡麵番", Name: "泡面番"},
	{ID: "Motion Anime", Name: "Motion Anime"},
	{ID: "3DCG", Name: "3D 动画"},
	{ID: "2.5D", Name: "2.5D"},
	{ID: "2D動畫", Name: "2D 动画"},
	{ID: "AI生成", Name: "AI 生成"},
	{ID: "MMD", Name: "MMD"},
	{ID: "Cosplay", Name: "Cosplay"},
}

var (
	reHanimeVideoHref = regexp.MustCompile(`/watch\?v=([A-Za-z0-9_-]{1,32})`)
	// 详情页的「觀看次數：1.2萬次 2026-01-02」，简繁两套都认。
	reHanimeViewUpload = regexp.MustCompile(`(?:觀看次數|观看次数)[：:]\s*([0-9.,]+\s*[萬万]?)\s*次\s*(\d{4}-\d{2}-\d{2})`)
	// 播放器没有 <source> 子节点时的兜底：页面脚本里的 const source = '...'
	reHanimeConstSource = regexp.MustCompile(`const\s+source\s*=\s*'([^'\\]+)'`)
	// 标签文本后面常跟「(12)」这样的计数，写进 Drama.Tags 前要剥掉。
	reHanimeTagCount = regexp.MustCompile(`\s*[\(（]\s*\d+\s*[\)）]\s*$`)
)

func validHanimeCode(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > hanimeMaxCode {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

func validHanimeCategory(category string) bool {
	if category == "" {
		return true
	}
	for _, entry := range hanimeGenres {
		if entry.ID == category {
			return true
		}
	}
	return false
}

// hanimeVideoCode 从 /watch?v=xxx 里取 videoCode。参考实现的 toVideoCode()
// 同款：只认 watch 路由，避免把站内其他链接误判成视频。
func hanimeVideoCode(raw string) string {
	match := reHanimeVideoHref.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) < 2 || !validHanimeCode(match[1]) {
		return ""
	}
	return match[1]
}

// hanimeAbsURL 把详情/列表里的相对地址补成绝对地址。
func hanimeAbsURL(raw string, base *url.URL) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "data:") {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if !validNativeCoverURL(parsed) {
		return ""
	}
	return parsed.String()
}

// ---- DOM 小工具 ----------------------------------------------------------
//
// 站点是服务端渲染的 HTML，参考实现用 Jsoup 选择器。这里用 x/net/html 做
// 等价遍历：类名一律「前缀匹配」，因为站点会给同一类卡片追加状态类
// （horizontal-card、horizontal-card-active…），写死等号在改版后会全空。

func hanimeClasses(node *html.Node) []string {
	if node == nil || node.Type != html.ElementNode {
		return nil
	}
	return strings.Fields(providerHTMLAttr(node, "class"))
}

func hanimeClassHas(node *html.Node, prefix string) bool {
	for _, class := range hanimeClasses(node) {
		if class == prefix || strings.HasPrefix(class, prefix) {
			return true
		}
	}
	return false
}

func hanimeFindNode(root *html.Node, match func(*html.Node) bool) *html.Node {
	if root == nil {
		return nil
	}
	if match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := hanimeFindNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func hanimeFindNodes(root *html.Node, match func(*html.Node) bool) []*html.Node {
	return providerHTMLNodes(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && match(node)
	})
}

// hanimeOwnText 只取直接文本子节点（Jsoup 的 ownText）。详情页的简介用
// 这个：父节点里还嵌着「展开/收起」之类的小标签，全量取文本会把它们混进来。
func hanimeOwnText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var text strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}
	return strings.Join(strings.Fields(text.String()), " ")
}

func hanimeNextElement(node *html.Node) *html.Node {
	if node == nil {
		return nil
	}
	for sibling := node.NextSibling; sibling != nil; sibling = sibling.NextSibling {
		if sibling.Type == html.ElementNode {
			return sibling
		}
	}
	return nil
}

// hanimePrevElement 取上一个元素兄弟节点。
func hanimePrevElement(node *html.Node) *html.Node {
	if node == nil {
		return nil
	}
	for sibling := node.PrevSibling; sibling != nil; sibling = sibling.PrevSibling {
		if sibling.Type == html.ElementNode {
			return sibling
		}
	}
	return nil
}

// ---- 列表 / 搜索 / 排行榜 -------------------------------------------------

func (d *Downloader) hanimeGet(ctx context.Context, address string) (string, error) {
	pageContext := context.WithValue(ctx, providerTextUserAgentKey{}, hanimeUserAgent)
	body, err := d.fetchProviderText(pageContext, address, d.providerBaseURL(sourceHanime)+"/")
	if err != nil {
		return "", hanimeRequestError(err)
	}
	return body, nil
}

// hanimeRequestError 给 Cloudflare 的地区封锁补一句可执行的提示。
// 站点对日本等地区直接回 403 "you have been blocked"，笼统的「请求失败」
// 会让人以为是解析坏了。
func hanimeRequestError(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if strings.Contains(text, "Cloudflare") || strings.Contains(text, "浏览器验证") {
		return fmt.Errorf("Han1me 拒绝了当前网络的请求（站点按 IP 地区限制）。请开启代理后重试：%w", err)
	}
	return err
}

// fetchHanimeSearchPage 是列表/搜索/排行榜共用的取页入口。
// genre 与 query 可同时为空（全部），sort 传站点枚举原文。
func (d *Downloader) fetchHanimeSearchPage(ctx context.Context, page int, genre, sortKey, query string) ([]Drama, bool, error) {
	if page < 1 || page > hanimeMaxPage {
		return nil, false, errors.New("Han1me 目录页码无效")
	}
	if !validHanimeCategory(genre) {
		return nil, false, errors.New("Han1me 内容分类无效")
	}
	if len(query) > hanimeMaxQuery {
		return nil, false, errors.New("Han1me 搜索关键词过长")
	}
	values := url.Values{"page": {strconv.Itoa(page)}}
	if strings.TrimSpace(sortKey) != "" {
		values.Set("sort", sortKey)
	}
	if genre != "" {
		values.Set("genre", genre)
	}
	if query != "" {
		values.Set("query", query)
	}
	address := d.providerBaseURL(sourceHanime) + "/search?" + values.Encode()
	body, err := d.hanimeGet(ctx, address)
	if err != nil {
		return nil, false, err
	}
	base, _ := url.Parse(d.providerBaseURL(sourceHanime) + "/")
	categoryName := hanimeGenreName(genre)
	items := parseHanimeCards(body, base, categoryName)
	// 站点不给总页数：满一页就当作还能翻。按经验页大小 20，这里用「至少
	// 半页」判断，避免最后一页只剩几条时仍死命翻下一页。
	hasMore := len(items) >= hanimePageGuess/2
	return items, hasMore, nil
}

func hanimeGenreName(genre string) string {
	for _, entry := range hanimeGenres {
		if entry.ID == genre {
			return entry.Name
		}
	}
	return "动画"
}

// fetchHanimeCatalogPage 分类浏览 + 在线搜索（接口与排行榜同一条）。
// 搜索时不带 sort：站点默认按相关度给结果，强行按「最新上市」排会把
// 匹配度高的老片压到后面。
func (d *Downloader) fetchHanimeCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	sortKey := hanimeDefaultSort
	if query != "" {
		sortKey = ""
	}
	return d.fetchHanimeSearchPage(ctx, page, category, sortKey, query)
}

func (d *Downloader) fetchHanimeCategories() []nativeCategory {
	categories := make([]nativeCategory, 0, len(hanimeGenres)+1)
	categories = append(categories, nativeCategory{Name: "全部"})
	return append(categories, hanimeGenres...)
}

// hanimeIsCard 判断节点是不是一张视频卡。三种形态按参考实现的新旧两版
// 一起认：新版 horizontal-card、旧版 card-mobile-panel、首页横排
// home-rows-videos-div。
func hanimeIsCard(node *html.Node) bool {
	for _, class := range hanimeClasses(node) {
		switch {
		case strings.HasPrefix(class, "horizontal-card"),
			strings.HasPrefix(class, "card-mobile-panel"),
			class == "home-rows-videos-div":
			return true
		}
	}
	return false
}

// hanimeCardAnchor 找卡片对应的 <a href="/watch?v=...">。
// 新版的 a 在卡片内部，旧版 a 是卡片的「上一个兄弟」，首页横排则是卡片的
// 祖先。三种都试，找不到就放弃这张卡（参考实现对解析不了的卡片直接跳过）。
func hanimeCardAnchor(card *html.Node) *html.Node {
	isWatchLink := func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "a" && hanimeVideoCode(providerHTMLAttr(node, "href")) != ""
	}
	if isWatchLink(card) {
		return card
	}
	// 祖先：首页横排 div 包在 a 里，最多上溯三层，避免抓到整块区块的链接。
	depth := 0
	for parent := card.Parent; parent != nil && depth < 3; parent = parent.Parent {
		if isWatchLink(parent) {
			return parent
		}
		if parent.Type == html.ElementNode && parent.Data == "body" {
			break
		}
		depth++
	}
	if found := hanimeFindNode(card, isWatchLink); found != nil {
		return found
	}
	// 上一个元素兄弟：旧版把 a 放在 panel 前面。
	if previous := hanimePrevElement(card); previous != nil {
		if isWatchLink(previous) {
			return previous
		}
		if found := hanimeFindNode(previous, isWatchLink); found != nil {
			return found
		}
	}
	return nil
}

func hanimeCardCover(card *html.Node, base *url.URL) string {
	images := hanimeFindNodes(card, func(node *html.Node) bool {
		return node.Data == "img"
	})
	// 旧版卡片第一张是懒加载占位图、第二张才是封面（参考实现取 img[1]），
	// 新版只有一张 .main-thumb。先认 main-thumb，否则取最后一张有效图。
	last := ""
	for _, image := range images {
		// 站点封面多为懒加载：真实地址在 data-src / data-original 上，
		// src 常常只是占位图，所以先取 data-*。
		address := hanimeAbsURL(firstNonEmpty(providerHTMLAttr(image, "data-src"), providerHTMLAttr(image, "data-original"), providerHTMLAttr(image, "src")), base)
		if address == "" {
			continue
		}
		if hanimeClassHas(image, "main-thumb") {
			return address
		}
		last = address
	}
	return last
}

func hanimeCardTitle(card *html.Node) string {
	// 类名优先级按参考实现的新->旧排列。
	for _, prefix := range []string{"home-rows-videos-title", "card-mobile-title", "video-title", "title"} {
		if found := hanimeFindNode(card, func(node *html.Node) bool {
			return hanimeClassHas(node, prefix)
		}); found != nil {
			if text := providerHTMLText(found); text != "" {
				return text
			}
		}
	}
	if image := hanimeFindNode(card, func(node *html.Node) bool { return node.Data == "img" }); image != nil {
		return strings.TrimSpace(providerHTMLAttr(image, "alt"))
	}
	return ""
}

// hanimeCardMetrics 取「时长 / 观看次数」。新版把时长放在 .duration，
// 观看次数是 .stat-item 里的第二项（第一项是评分/评论数）；旧版是两条
// card-mobile-duration，第一条时长、第二条观看次数。
func hanimeCardMetrics(card *html.Node) (string, string) {
	duration := ""
	var statItems []string
	var legacy []string
	for _, node := range hanimeFindNodes(card, func(node *html.Node) bool {
		return hanimeClassHas(node, "duration") || hanimeClassHas(node, "card-mobile-duration") || hanimeClassHas(node, "stat-item")
	}) {
		text := providerHTMLText(node)
		if text == "" {
			continue
		}
		switch {
		case hanimeClassHas(node, "card-mobile-duration"):
			legacy = append(legacy, text)
		case hanimeClassHas(node, "duration"):
			if duration == "" {
				duration = text
			}
		default:
			statItems = append(statItems, text)
		}
	}
	views := ""
	if len(statItems) > 1 {
		views = statItems[1]
	} else if len(statItems) == 1 {
		views = statItems[0]
	}
	if len(legacy) > 0 {
		if duration == "" {
			duration = legacy[0]
		}
		if len(legacy) > 1 {
			views = legacy[1]
		}
	}
	return strings.TrimSpace(duration), strings.TrimSpace(views)
}

// parseHanimeCards 解析列表页。整页扫卡片节点，再逐张取锚点/封面/标题，
// 解析失败的卡片直接跳过（站点会往列表里插广告卡，参考实现同样跳过）。
func parseHanimeCards(body string, base *url.URL, category string) []Drama {
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	cards := hanimeFindNodes(document, hanimeIsCard)
	items := make([]Drama, 0, hanimePageGuess)
	seen := map[string]bool{}
	for _, card := range cards {
		anchor := hanimeCardAnchor(card)
		if anchor == nil {
			continue
		}
		code := hanimeVideoCode(providerHTMLAttr(anchor, "href"))
		if code == "" || seen[code] {
			continue
		}
		// 标题/封面在卡片里；旧版的卡片本身没有 a，字段还在卡片上。
		title := hanimeCardTitle(card)
		if title == "" && anchor != card {
			title = hanimeCardTitle(anchor)
		}
		cover := hanimeCardCover(card, base)
		if cover == "" && anchor != card {
			cover = hanimeCardCover(anchor, base)
		}
		if title == "" || cover == "" {
			continue
		}
		duration, views := hanimeCardMetrics(card)
		seen[code] = true
		items = append(items, Drama{
			ID: providerDramaID(sourceHanime, code), Source: sourceHanime, SourceID: code,
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover,
			ChannelName: "Han1me", Category: category,
			Remark: truncate(duration, 32), Views: truncate(views, 32),
		})
	}
	return items
}

// ---- 详情 / 系列 / 播放 ---------------------------------------------------

type hanimePart struct {
	Code     string
	Title    string
	Cover    string
	Duration string
}

type hanimeSource struct {
	URL     string
	Quality int
}

type hanimeDetail struct {
	Code         string
	PageURL      string
	Title        string
	ChineseTitle string
	Intro        string
	Cover        string
	Artist       string
	Genre        string
	UploadDate   string
	Views        string
	Tags         []string
	SeriesName   string
	Parts        []hanimePart
	Sources      []hanimeSource
}

func (d *Downloader) fetchHanimeDetail(ctx context.Context, code string) (hanimeDetail, error) {
	if !validHanimeCode(code) {
		return hanimeDetail{}, errors.New("Han1me 视频编号无效")
	}
	pageURL := d.providerBaseURL(sourceHanime) + "/watch?v=" + url.QueryEscape(code)
	body, err := d.hanimeGet(ctx, pageURL)
	if err != nil {
		return hanimeDetail{}, err
	}
	base, _ := url.Parse(d.providerBaseURL(sourceHanime) + "/")
	detail := parseHanimeDetail(body, base)
	detail.Code, detail.PageURL = code, pageURL
	if detail.Title == "" {
		return hanimeDetail{}, errors.New("Han1me 详情页解析失败，站点可能已改版")
	}
	return detail, nil
}

func parseHanimeDetail(body string, base *url.URL) hanimeDetail {
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return hanimeDetail{}
	}
	detail := hanimeDetail{}
	if node := hanimeFindNode(document, func(node *html.Node) bool {
		return providerHTMLAttr(node, "id") == "shareBtn-title"
	}); node != nil {
		detail.Title = providerHTMLText(node)
	}
	if detail.Title == "" {
		if node := hanimeFindNode(document, func(node *html.Node) bool { return node.Data == "h1" }); node != nil {
			detail.Title = providerHTMLText(node)
		}
	}

	// 播放器：poster 是封面，<source size="1080"> 是各清晰度。
	if player := hanimeFindNode(document, func(node *html.Node) bool {
		return node.Data == "video" && providerHTMLAttr(node, "id") == "player"
	}); player != nil {
		detail.Cover = hanimeAbsURL(providerHTMLAttr(player, "poster"), base)
		for _, source := range hanimeFindNodes(player, func(node *html.Node) bool { return node.Data == "source" }) {
			address := hanimeAbsURL(providerHTMLAttr(source, "src"), base)
			if address == "" {
				continue
			}
			// size="1080" 是清晰度；个别源只给 type，按 0 处理（不参与排序）。
			quality, _ := strconv.Atoi(strings.TrimSuffix(providerHTMLAttr(source, "size"), "P"))
			detail.Sources = append(detail.Sources, hanimeSource{URL: address, Quality: quality})
		}
		if len(detail.Sources) == 0 {
			if address := hanimeAbsURL(providerHTMLAttr(player, "src"), base); address != "" {
				detail.Sources = append(detail.Sources, hanimeSource{URL: address})
			}
		}
	}
	// 没有 <source> 时页面脚本里还有一条 const source = '...'。
	if len(detail.Sources) == 0 {
		if match := reHanimeConstSource.FindSubmatch([]byte(body)); len(match) > 1 {
			if address := hanimeAbsURL(string(match[1]), base); address != "" {
				detail.Sources = append(detail.Sources, hanimeSource{URL: address})
			}
		}
	}
	if detail.Cover == "" {
		if image := hanimeFindNode(document, func(node *html.Node) bool {
			return node.Data == "img" && hanimeClassHas(node, "main-thumb")
		}); image != nil {
			detail.Cover = hanimeAbsURL(providerHTMLAttr(image, "src"), base)
		}
	}

	// 简介与中文标题：video-caption-text 自己的文本是简介，上一个兄弟节点
	// 是中文标题（参考实现 hanimeVideoVer2 同款）。
	if caption := hanimeFindNode(document, func(node *html.Node) bool {
		return hanimeClassHas(node, "video-caption-text")
	}); caption != nil {
		detail.Intro = hanimeOwnText(caption)
		if detail.Intro == "" {
			// 简介里若整段套了 <span>/<br>，ownText 会取空，退回全量文本。
			detail.Intro = providerHTMLText(caption)
		}
		if previous := hanimePrevElement(caption); previous != nil {
			detail.ChineseTitle = hanimeOwnText(previous)
		}
	}

	// 标签：single-video-tag 的第一个子元素是 <a>，文本形如「#巨乳 (12)」。
	for _, node := range hanimeFindNodes(document, func(node *html.Node) bool {
		return hanimeClassHas(node, "single-video-tag")
	}) {
		// 标签文本在里面的 <a> 上；找不到链接时才退回节点自有文本。
		text := ""
		if link := hanimeFindNode(node, func(node *html.Node) bool { return node.Data == "a" }); link != nil {
			text = providerHTMLText(link)
		}
		if text == "" {
			text = hanimeOwnText(node)
		}
		text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "#"))
		text = strings.TrimSpace(reHanimeTagCount.ReplaceAllString(text, ""))
		if text == "" || len([]rune(text)) > 48 || len(detail.Tags) >= 16 {
			continue
		}
		duplicated := false
		for _, existing := range detail.Tags {
			duplicated = duplicated || existing == text
		}
		if !duplicated {
			detail.Tags = append(detail.Tags, text)
		}
	}

	// 制作商 + 类型：video-artist-name 与它的下一个兄弟。
	if artist := hanimeFindNode(document, func(node *html.Node) bool {
		return providerHTMLAttr(node, "id") == "video-artist-name"
	}); artist != nil {
		detail.Artist = providerHTMLText(artist)
		if next := hanimeNextElement(artist); next != nil {
			detail.Genre = providerHTMLText(next)
		}
	}

	// 观看次数 + 发售日期：整页文本里匹配一次。
	pageText := providerHTMLText(document)
	if match := reHanimeViewUpload.FindStringSubmatch(pageText); len(match) > 2 {
		detail.Views = strings.TrimSpace(match[1])
		detail.UploadDate = strings.TrimSpace(match[2])
	}

	// 系列播放列表：容器里每一项都是另一个 videoCode。
	if wrapper := hanimeFindNode(document, func(node *html.Node) bool {
		return providerHTMLAttr(node, "id") == "video-playlist-wrapper" || hanimeClassHas(node, "video-playlist-wrapper")
	}); wrapper != nil {
		if name := hanimeFindNode(wrapper, func(node *html.Node) bool {
			return providerHTMLAttr(node, "id") == "playlist-top-block"
		}); name != nil {
			if link := hanimeFindNode(name, func(node *html.Node) bool { return node.Data == "a" }); link != nil {
				detail.SeriesName = providerHTMLText(link)
			}
		}
		if detail.SeriesName == "" {
			if heading := hanimeFindNode(wrapper, func(node *html.Node) bool { return node.Data == "h4" }); heading != nil {
				detail.SeriesName = providerHTMLText(heading)
			}
		}
		detail.Parts = parseHanimeParts(wrapper, base)
	}
	return detail
}

// parseHanimeParts 解析系列列表。新版把地址放在每一项的 data-href 上，
// 旧版是里面第一个 <a>；两种都收。
func parseHanimeParts(wrapper *html.Node, base *url.URL) []hanimePart {
	parts := make([]hanimePart, 0, 12)
	seen := map[string]bool{}
	for _, node := range hanimeFindNodes(wrapper, func(node *html.Node) bool {
		if providerHTMLAttr(node, "data-href") != "" {
			return true
		}
		return node.Data == "a" && hanimeVideoCode(providerHTMLAttr(node, "href")) != ""
	}) {
		code := hanimeVideoCode(firstNonEmpty(providerHTMLAttr(node, "data-href"), providerHTMLAttr(node, "href")))
		if code == "" || seen[code] {
			continue
		}
		title := ""
		if heading := hanimeFindNode(node, func(node *html.Node) bool {
			return hanimeClassHas(node, "video-title") || node.Data == "h4"
		}); heading != nil {
			title = providerHTMLText(heading)
		}
		cover := ""
		if image := hanimeFindNode(node, func(node *html.Node) bool { return node.Data == "img" }); image != nil {
			cover = hanimeAbsURL(providerHTMLAttr(image, "src"), base)
			if title == "" {
				title = strings.TrimSpace(providerHTMLAttr(image, "alt"))
			}
		}
		duration := ""
		if node := hanimeFindNode(node, func(node *html.Node) bool { return hanimeClassHas(node, "duration") }); node != nil {
			duration = providerHTMLText(node)
		}
		seen[code] = true
		parts = append(parts, hanimePart{Code: code, Title: title, Cover: cover, Duration: duration})
	}
	return parts
}

func (d *Downloader) fetchHanimeDetailChapters(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	detail, err := d.fetchHanimeDetail(ctx, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	drama := hanimeDramaFromDetail(detail)
	chapters := make([]Chapter, 0, max(1, len(detail.Parts)))
	seen := map[string]bool{}
	appendChapter := func(code, title string, index int) {
		if code == "" || seen[code] {
			return
		}
		seen[code] = true
		if strings.TrimSpace(title) == "" {
			title = fmt.Sprintf("第 %d 集", index)
		}
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceHanime, sourceID, code), Source: sourceHanime,
			Title: truncate(title, 128), VideoURL: hanimePlayScheme + url.PathEscape(code),
			CurrentEpisode: json.RawMessage(strconv.Itoa(index)), Referer: detail.PageURL,
		})
	}
	// 系列里先放当前这一集，再按页面顺序补齐其余；没有系列时就是单片。
	if len(detail.Parts) == 0 {
		appendChapter(sourceID, hanimeDisplayTitle(detail), 1)
	} else {
		index := 1
		appendChapter(sourceID, firstNonEmpty(hanimeDisplayTitle(detail), detail.SeriesName), index)
		index++
		for _, part := range detail.Parts {
			appendChapter(part.Code, part.Title, index)
			index++
		}
	}
	if len(chapters) == 0 {
		return Drama{}, nil, errors.New("Han1me 该视频暂无可播放的分集")
	}
	drama.Episodes = len(chapters)
	return drama, chapters, nil
}

// hanimeDisplayTitle 优先中文标题，但排除「中文标题：」这类标签文本；
// 没有可用中文标题时用原标题。
func hanimeDisplayTitle(detail hanimeDetail) string {
	chinese := strings.TrimSpace(detail.ChineseTitle)
	if chinese != "" && chinese != "中文标题" && chinese != "中文標題" &&
		!strings.HasSuffix(chinese, "：") && !strings.HasSuffix(chinese, ":") {
		return chinese
	}
	return detail.Title
}

func hanimeDramaFromDetail(detail hanimeDetail) Drama {
	description := detail.Intro
	extra := make([]string, 0, 4)
	if detail.Title != "" && detail.Title != hanimeDisplayTitle(detail) {
		extra = append(extra, "原名："+detail.Title)
	}
	if detail.Artist != "" {
		extra = append(extra, "制作商："+detail.Artist)
	}
	if detail.Genre != "" {
		extra = append(extra, "类型："+detail.Genre)
	}
	if detail.UploadDate != "" {
		extra = append(extra, "发售日期："+detail.UploadDate)
	}
	if detail.Views != "" {
		extra = append(extra, "观看次数："+detail.Views+" 次")
	}
	if len(extra) > 0 {
		description = strings.TrimSpace(description + "\n\n" + strings.Join(extra, "\n"))
	}
	title := hanimeDisplayTitle(detail)
	category := detail.Genre
	if category == "" && len(detail.Tags) > 0 {
		category = detail.Tags[0]
	}
	if category == "" {
		category = "动画"
	}
	return Drama{
		ID: providerDramaID(sourceHanime, detail.Code), Source: sourceHanime, SourceID: detail.Code,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Desc: truncate(description, 12000), Intro: truncate(description, 12000),
		Cover: detail.Cover, CoverURL: detail.Cover,
		ChannelName: "Han1me", Category: category,
		Tags:  append([]string(nil), detail.Tags...),
		Views: detail.Views, OnlineDate: detail.UploadDate,
	}
}

// hanimeChapterCode 从分集上取回 videoCode，先看占位符，再退回章节 ID。
func hanimeChapterCode(task Task) (string, error) {
	chapter := task.Chapter
	if strings.HasPrefix(chapter.VideoURL, hanimePlayScheme) {
		code, err := url.PathUnescape(strings.TrimPrefix(chapter.VideoURL, hanimePlayScheme))
		if err == nil && validHanimeCode(code) {
			return code, nil
		}
	}
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid || source != sourceHanime {
		return "", errors.New("Han1me 播放信息无效，请刷新详情")
	}
	prefix := providerChapterID(sourceHanime, sourceID, "")
	if strings.HasPrefix(chapter.ID, prefix) {
		code := strings.TrimPrefix(chapter.ID, prefix)
		if validHanimeCode(code) {
			return code, nil
		}
	}
	if validHanimeCode(sourceID) {
		return sourceID, nil
	}
	return "", errors.New("Han1me 分集编号无效")
}

// resolveHanimeMedia 按清晰度把多 <source> 映射成播放线路：主线路取最高清，
// 其余作为 Variants，播放器里就是「线路/清晰度」切换。
func (d *Downloader) resolveHanimeMedia(ctx context.Context, task Task) (providerMedia, error) {
	code, err := hanimeChapterCode(task)
	if err != nil {
		return providerMedia{}, err
	}
	detail, err := d.fetchHanimeDetail(ctx, code)
	if err != nil {
		return providerMedia{}, err
	}
	if len(detail.Sources) == 0 {
		return providerMedia{}, errors.New("Han1me 该视频没有可用的播放地址（可能需要登录或站点限制）")
	}
	sources := append([]hanimeSource(nil), detail.Sources...)
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].Quality > sources[j].Quality })
	media := providerMedia{URL: sources[0].URL, Referer: detail.PageURL, Quality: sources[0].Quality}
	for _, source := range sources[1:] {
		if source.URL == media.URL {
			continue
		}
		media.Variants = append(media.Variants, providerMedia{URL: source.URL, Referer: detail.PageURL, Quality: source.Quality})
	}
	return d.finishProviderMedia(ctx, media, "Han1me")
}

// ---- 榜单 ---------------------------------------------------------------

// fetchHanimeRankingPage 排行榜直接用站点的 sort 枚举翻页，排序键放在
// rankingBoard.path 里（与剧果把分类放在 path 是同一套约定）。
func (d *Downloader) fetchHanimeRankingPage(ctx context.Context, board rankingBoard, page int) (rankingPage, error) {
	items, more, err := d.fetchHanimeSearchPage(ctx, page, "", board.path, "")
	if err != nil {
		return rankingPage{}, err
	}
	result := rankingPage{Items: make([]rankingItem, 0, len(items)), HasMore: more}
	for index, drama := range items {
		result.Items = append(result.Items, rankingItem{
			Rank: (page-1)*hanimePageGuess + index + 1, Drama: rankingDramaWithoutImages(drama),
		})
	}
	return result, nil
}
