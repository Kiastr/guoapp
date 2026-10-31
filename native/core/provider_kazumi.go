package core

// Kazumi 规则源（+155）：把 Kazumi 官方规则库 Predidit/KazumiRules 的
// 「api 模式」规则翻译成本项目内部的 Drama / Chapter。
//
// 为什么单独做一层，而不是接进现有 drpy / XBPQ / 海阔？
//   - Kazumi 规则不是 JS（不需要 goja），也不是猫系选择器链
//     （position XPath / CSS 选择器），它是一份「请求 + JSON 路径」的描述；
//   - 官方库 86 条规则里只有 4 条是完整的 api 模式（searchMode/chapterMode
//     均为 api），其余要么已废弃，要么章节取值依赖 XPath 页面解析
//     （本项目没有 XPath 求值器，导入时明确拒绝而不是静默失败）。
//
// api 模式的三段结构（以 xfdmnext / giriGiriLove 为样本）：
//   1. searchApiConfig：请求一个 JSON 接口，按 listPath 取列表，
//      每项用 namePath / sourcePath 取「剧名 / 源内 ID」；
//   2. chapterApiConfig：按源内 ID 请求详情，取线路与分集。两种形态：
//        - nested：线路数组 → 分集数组（xfdmnext：$.sources[*].episodes[*]）；
//        - delimited：线路名 / 分集串用分隔符拼在一起（giriGiriLove：
//          MacCMS 的 vod_play_from / vod_play_url，「$$$/#/$」三层分隔）；
//   3. 播放：分集值可能是直链（giriGiriLove 的 mp4），也可能只是一个
//      「分集标识」，需要拼进 episodePage 模板得到播放页 URL，再交给
//      WebView 嗅探真实地址（xfdmnext，规则里 useWebview=true）。
//
// 目录浏览：Kazumi 规则没有分类 / 首页接口，官方 App 也是「只有搜索」。
// 本项目把「空关键词 + 分页」当作最近更新列表，首页才不至于空白。
// 分页键（offset / limit / page / page_number …）从规则自己的请求体里
// 推断：规则写了什么键就改什么键，不凭空塞参数。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// kazumiTimeout 规则请求超时（规则接口普遍较慢，给到 15s）。
	kazumiTimeout = 15 * time.Second
	// kazumiBackgroundTimeout 后台任务（封面补齐 / 自愈刷新）的降档超时，
	// 与 doPreparedCatalogRequest 里的背景约定一致。
	kazumiBackgroundTimeout = 8 * time.Second
	// kazumiMaxBodyBytes 单次响应上限（MacCMS 详情 body 带全文简介，
	// 4MB 足够，超出多半是异常页）。
	kazumiMaxBodyBytes = 4 << 20
	// kazumiDefaultPageSize 规则没写每页条数时的取值（与 Kazumi 一致）。
	kazumiDefaultPageSize = 24
	// kazumiMaxPage 目录最大页码：不让用户无限往下翻。
	kazumiMaxPage = 200
	// kazumiMaxKeyword 搜索关键词长度上限。
	kazumiMaxKeyword = 128
	// kazumiMaxIDKey 源内 ID / 规则名长度上限。
	kazumiMaxIDKey = 128
	// kazumiMaxSourceID 源内 ID（含 URL 时）长度上限。
	kazumiMaxSourceID = 512
	// kazumiMaxLines 单部剧的线路上限（防异常规则把线路刷爆）。
	kazumiMaxLines = 32
	// kazumiMaxEpisodes 单条线路的分集上限。
	kazumiMaxEpisodes = 10000
	// kazumiOfficialIndexURL Kazumi 官方规则库索引，用于「从官方库导入」。
	kazumiOfficialIndexURL = "https://raw.githubusercontent.com/Predidit/KazumiRules/main/index.json"
	// kazumiOfficialRuleURLTemplate 官方库单条规则的下载地址模板。
	kazumiOfficialRuleURLTemplate = "https://raw.githubusercontent.com/Predidit/KazumiRules/main/@name.json"
)

// kazumiRule 一份 Kazumi 规则（只保留 api 模式需要的字段）。
type kazumiRule struct {
	// Name 规则名（官方库的文件名，也是本站源的 key）。
	Name string
	// Version 规则版本，升级（同 key 重新导入）时用得上。
	Version string
	// BaseURL 站点地址，用于补全相对封面 / 播放页 Referer。
	BaseURL string
	// UserAgent 规则指定 UA（搜狗/PC UA 宏不展开，官方库都是整串或空）。
	UserAgent string
	// Referer 规则指定来源页。
	Referer string
	// MultiSources 是否多线路（muliSources，官方拼写如此）。
	MultiSources bool
	// UseWebview 为 true 时分集需要 WebView 嗅探才能拿到真实地址。
	UseWebview bool
	Search     kazumiSearchConfig
	Chapter    kazumiChapterConfig
}

// kazumiSearchConfig 搜索 / 目录接口。
type kazumiSearchConfig struct {
	Request kazumiRequestSpec
	// List 列表路径（相对响应根），形如 $.* / $.info.rows[*]。
	List string
	// Name / Source 单条记录内的剧名 / 源内 ID 路径。
	Name   string
	Source string
}

// kazumiChapterConfig 分集接口。
type kazumiChapterConfig struct {
	Request kazumiRequestSpec
	// Format nested（线路数组）或 delimited（分隔符串）。
	Format string
	// nested 形态
	Roads       string
	RoadName    string
	Episodes    string
	EpisodeName string
	EpisodeURL  string
	// PageURL 播放页模板（@source / @episodeUrl 占位），为空表示分集值是直链。
	PageURL string
	// delimited 形态
	RoadNames    string
	RoadEpisodes string
	RoadSep      string
	EpisodeSep   string
	FieldSep     string
}

// kazumiRequestSpec 规则里的请求描述（method / url / headers / query / body）。
type kazumiRequestSpec struct {
	Method   string
	URL      string
	Headers  map[string]string
	Query    map[string]any
	BodyType string
	Body     map[string]any
	RawBody  string
}

// kazumiRuleFromMap 解析规则正文；只放行 api 模式，其余模式给出明确原因。
func kazumiRuleFromMap(raw map[string]any) (kazumiRule, error) {
	if len(raw) == 0 {
		return kazumiRule{}, errors.New("Kazumi 规则内容为空")
	}
	searchMode := strings.ToLower(kazumiText(raw["searchMode"]))
	chapterMode := strings.ToLower(kazumiText(raw["chapterMode"]))
	if searchMode != "api" || chapterMode != "api" {
		return kazumiRule{}, errors.New(
			"该 Kazumi 规则不是 api 模式（依赖 XPath / 网页解析），本站源暂不支持，请选择 api 模式规则")
	}
	search := kazumiMap(raw["searchApiConfig"])
	chapter := kazumiMap(raw["chapterApiConfig"])
	if len(search) == 0 || len(chapter) == 0 {
		return kazumiRule{}, errors.New("Kazumi 规则缺少 searchApiConfig / chapterApiConfig")
	}
	rule := kazumiRule{
		Name:         kazumiText(raw["name"]),
		Version:      kazumiText(raw["version"]),
		BaseURL:      strings.TrimRight(kazumiText(raw["baseURL"]), "/"),
		UserAgent:    kazumiText(raw["userAgent"]),
		Referer:      kazumiText(raw["referer"]),
		MultiSources: kazumiBool(raw["muliSources"], false),
		UseWebview:   kazumiBool(raw["useWebview"], false),
		Search: kazumiSearchConfig{
			Request: kazumiRequestFrom(search["request"]),
			List:    firstNonEmpty(kazumiText(search["listPath"]), "$[*]"),
			Name:    firstNonEmpty(kazumiText(search["namePath"]), "$.title"),
			Source:  firstNonEmpty(kazumiText(search["sourcePath"]), "$.id"),
		},
		Chapter: kazumiChapterConfig{
			Request:      kazumiRequestFrom(chapter["request"]),
			Format:       strings.ToLower(kazumiText(chapter["format"])),
			Roads:        kazumiText(chapter["roadsPath"]),
			RoadName:     kazumiText(chapter["roadNamePath"]),
			Episodes:     kazumiText(chapter["episodesPath"]),
			EpisodeName:  kazumiText(chapter["episodeNamePath"]),
			EpisodeURL:   kazumiText(chapter["episodeUrlPath"]),
			PageURL:      kazumiText(kazumiMap(chapter["episodePage"])["url"]),
			RoadNames:    kazumiText(chapter["roadNamesPath"]),
			RoadEpisodes: kazumiText(chapter["roadEpisodesPath"]),
			RoadSep:      firstNonEmpty(kazumiText(chapter["roadSeparator"]), "$$$"),
			EpisodeSep:   firstNonEmpty(kazumiText(chapter["episodeSeparator"]), "#"),
			FieldSep:     firstNonEmpty(kazumiText(chapter["fieldSeparator"]), "$"),
		},
	}
	if rule.Search.Request.URL == "" {
		return kazumiRule{}, errors.New("Kazumi 规则缺少搜索接口地址")
	}
	if rule.Chapter.Request.URL == "" {
		return kazumiRule{}, errors.New("Kazumi 规则缺少分集接口地址")
	}
	if rule.Chapter.RoadNames != "" && rule.Chapter.RoadEpisodes != "" {
		rule.Chapter.Format = "delimited"
	} else if rule.Chapter.Format != "delimited" {
		rule.Chapter.Format = "nested"
	}
	if rule.Chapter.Format == "nested" && (rule.Chapter.Roads == "" || rule.Chapter.Episodes == "") {
		return kazumiRule{}, errors.New("Kazumi 规则缺少线路 / 分集路径（roadsPath / episodesPath）")
	}
	if rule.Name == "" {
		return kazumiRule{}, errors.New("Kazumi 规则缺少站点名称")
	}
	if len(rule.Name) > kazumiMaxIDKey {
		return kazumiRule{}, errors.New("Kazumi 规则站点名称过长")
	}
	if parsed, err := url.Parse(rule.BaseURL); err != nil || (rule.BaseURL != "" && parsed.Hostname() == "") {
		// baseURL 只用于补全相对地址 / Referer，非法时置空即可（不阻塞导入）。
		rule.BaseURL = ""
	}
	return rule, nil
}

// kazumiRequestFrom 解析 request 段落。
func kazumiRequestFrom(value any) kazumiRequestSpec {
	row := kazumiMap(value)
	spec := kazumiRequestSpec{
		Method:   strings.ToUpper(kazumiText(row["method"])),
		URL:      kazumiText(row["url"]),
		Headers:  kazumiStringMap(row["headers"]),
		BodyType: strings.ToLower(kazumiText(row["bodyType"])),
		Query:    kazumiAnyMap(row["query"]),
		Body:     kazumiAnyMap(row["body"]),
	}
	if spec.Method == "" {
		spec.Method = http.MethodGet
	}
	switch body := row["body"].(type) {
	case string:
		spec.RawBody = body
	}
	return spec
}

// kazumiMap 宽松取 map（非 map 一律空表）。
func kazumiMap(value any) map[string]any {
	row, _ := value.(map[string]any)
	if row == nil {
		return map[string]any{}
	}
	return row
}

// kazumiAnyMap 同上，用于 query / body 这类 any 值字典。
func kazumiAnyMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
}

// kazumiStringMap 取字符串字典（header 的 value 可能是数字 / 布尔）。
func kazumiStringMap(value any) map[string]string {
	row := kazumiAnyMap(value)
	if row == nil {
		return nil
	}
	out := make(map[string]string, len(row))
	for key, item := range row {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = kazumiText(item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// kazumiBool 宽松布尔：true / 1 / "true" 都算真，缺省用 fallback。
func kazumiBool(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no", "":
			return false
		}
	}
	return fallback
}

// kazumiText JSON 节点转字符串（json.Number / float64 都按原样取值，
// 避免大整数 ID 被科学计数法改写）。
func kazumiText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ruleJSONText(value)
}

// kazumiJSONPath 按 Kazumi 的 JSON 路径取值。
//
// Kazumi 用标准 JSONPath 片段（$.a.b[*]），项目里现成的是 XBPQ / 海阔方言
// ruleJSONPath（点分隔、[] 取整数组）。两者在这几条规则用到的子集里是
// 一一对应的，直接做字符串翻译即可，不额外写一个求值器：
//
//	$.title        → title
//	$[*]           → []
//	$.info.rows[*] → info.rows[]
func kazumiJSONPath(data any, path string) any {
	translated := strings.TrimPrefix(strings.TrimSpace(path), "$")
	translated = strings.TrimPrefix(translated, ".")
	translated = strings.ReplaceAll(translated, "[*]", "[]")
	if translated == "" && strings.TrimSpace(path) == "" {
		return data
	}
	return ruleJSONPath(data, translated)
}

// kazumiRows 列表节点 → 记录字典切片（非字典项跳过）。
func kazumiRows(value any) []map[string]any {
	list, _ := value.([]any)
	rows := make([]map[string]any, 0, len(list))
	for _, entry := range list {
		if row, ok := entry.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

// kazumiApplyVars 替换 @keyword / @source / @episodeUrl 占位符。
//
// 只做字面替换：官方规则的占位符是「保持原样插入」，数值类 ID 不会带
// 分隔符；真正需要转义的 query / JSON 体由上层编码器处理。
func kazumiApplyVars(text string, vars map[string]string) string {
	out := text
	// 先长后短：@episodeUrl 必须早于 @episode 之类前缀冲突的键。
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		out = strings.ReplaceAll(out, "@"+key, vars[key])
	}
	return strings.TrimSpace(out)
}

// kazumiSubstitute 递归替换 any 结构里的占位符（body / query 的值）。
func kazumiSubstitute(value any, vars map[string]string) any {
	switch typed := value.(type) {
	case string:
		return kazumiApplyVars(typed, vars)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, kazumiSubstitute(item, vars))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = kazumiSubstitute(item, vars)
		}
		return out
	default:
		return value
	}
}

// kazumiCloneAnyMap 浅拷贝 any 字典（分页改写不能污染规则原文）。
func kazumiCloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// kazumiPageKeys 规则里出现过的分页参数键名（offset / limit / page 三大类）。

// kazumiPaginate 把页码写进规则自带的 query / body。
//
// 只改规则里已经存在的键，不凭空新增：Kazumi 规则的接口形态各异，
// 硬塞 offset 到 xfdmnext 的 JSON body 上只会让上游报错（实测该接口
// 只认 page_number / items_per_page 两个键，其它键一律忽略但不保证）。
func kazumiPaginate(spec kazumiRequestSpec, page int) (kazumiRequestSpec, bool) {
	if page <= 1 {
		return spec, kazumiHasPagingKey(spec)
	}
	size := kazumiDefaultPageSize
	if declared := kazumiIntValue(spec, kazumiSizeKeys); declared > 0 && declared <= 200 {
		size = declared
	}
	offset := (page - 1) * size
	applied := false
	for _, target := range []*map[string]any{&spec.Query, &spec.Body} {
		if *target == nil {
			continue
		}
		updated := kazumiCloneAnyMap(*target)
		for key := range updated {
			switch normalizeKazumiPageKey(key) {
			case "offset":
				updated[key] = kazumiJSONNumber(offset)
				applied = true
			case "size":
				updated[key] = kazumiJSONNumber(size)
				applied = true
			case "page":
				updated[key] = kazumiJSONNumber(page)
				applied = true
			}
		}
		*target = updated
	}
	return spec, applied || kazumiHasPagingKey(spec)
}

// kazumiJSONNumber 规则里原有的分页值是 float64 / json.Number，
// 改写时保持数字形态（写成字符串会让部分上游 RPC 直接报错）。
func kazumiJSONNumber(value int) float64 { return float64(value) }

var kazumiSizeKeys = map[string]bool{
	"limit": true, "size": true, "pagesize": true, "items_per_page": true, "rows": true,
}

func kazumiIntValue(spec kazumiRequestSpec, keys map[string]bool) int {
	for _, source := range []map[string]any{spec.Query, spec.Body} {
		for key, value := range source {
			if !keys[normalizeKazumiPageKey(key)] {
				continue
			}
			switch typed := value.(type) {
			case float64:
				return int(typed)
			case json.Number:
				if number, err := strconv.Atoi(typed.String()); err == nil {
					return number
				}
			case string:
				if number, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
					return number
				}
			}
		}
	}
	return 0
}

// normalizeKazumiPageKey 分页键归一化（offset / limit / page 三大类）。
func normalizeKazumiPageKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "offset", "page_number", "page", "p", "pg", "pageindex":
		if strings.EqualFold(strings.TrimSpace(key), "offset") {
			return "offset"
		}
		return "page"
	case "limit", "size", "pagesize", "items_per_page", "rows", "per_page":
		return "size"
	default:
		return ""
	}
}

func kazumiHasPagingKey(spec kazumiRequestSpec) bool {
	for _, source := range []map[string]any{spec.Query, spec.Body} {
		for key := range source {
			if normalizeKazumiPageKey(key) != "" {
				return true
			}
		}
	}
	return false
}

// kazumiRequest 执行一次规则请求并返回解码后的 JSON。
func (d *Downloader) kazumiRequest(
	ctx context.Context,
	site kazumiSite,
	spec kazumiRequestSpec,
	vars map[string]string,
) (any, error) {
	target := kazumiApplyVars(spec.URL, vars)
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, errors.New("Kazumi 规则请求地址无效")
	}
	// 用户导入的规则指向任意地址：与 drpy 同一套 SSRF 判定，
	// 挡住 127.0.0.1 / 内网 / 链路本地目标（测试夹具按 drpyAllowLocalTargets 放行）。
	if !d.drpyAllowLocalTargets {
		if err := drpyBlockedTarget(target); err != nil {
			return nil, err
		}
	}
	method := spec.Method
	if method != http.MethodGet && method != http.MethodPost {
		method = http.MethodGet
	}
	query := url.Values{}
	for key, value := range parsed.Query() {
		for _, item := range value {
			query.Add(key, item)
		}
	}
	for key, value := range spec.Query {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		query.Set(key, kazumiText(kazumiSubstitute(value, vars)))
	}
	var body string
	var contentType string
	if len(spec.Body) > 0 {
		encoded, err := json.Marshal(kazumiSubstitute(spec.Body, vars))
		if err != nil {
			return nil, errors.New("Kazumi 规则请求体无效")
		}
		body, contentType = string(encoded), "application/json"
	} else if strings.TrimSpace(spec.RawBody) != "" {
		body = kazumiApplyVars(spec.RawBody, vars)
		contentType = "application/x-www-form-urlencoded"
		if trimmed := strings.TrimSpace(body); strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			contentType = "application/json"
		}
	}
	if method == http.MethodGet {
		parsed.RawQuery = query.Encode()
	} else if body == "" && len(query) > 0 {
		// jQuery $.post 风格：bodyType=form 时把 query 当作表单体。
		body, contentType = query.Encode(), "application/x-www-form-urlencoded"
	} else if strings.EqualFold(spec.BodyType, "form") && len(query) > 0 {
		body, contentType = query.Encode(), "application/x-www-form-urlencoded"
	}
	if method == http.MethodPost && strings.EqualFold(spec.BodyType, "form") &&
		contentType == "application/json" && strings.TrimSpace(spec.RawBody) != "" {
		body, contentType = spec.RawBody, "application/x-www-form-urlencoded"
	}

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), reader)
	if err != nil {
		return nil, errors.New("Kazumi 规则请求构造失败")
	}
	request.Header.Set("User-Agent", firstNonEmpty(site.Rule.UserAgent, userAgent))
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	if referer := firstNonEmpty(site.Rule.Referer, site.Rule.BaseURL); referer != "" {
		request.Header.Set("Referer", referer)
		if origin, err := url.Parse(referer); err == nil && origin.Host != "" {
			request.Header.Set("Origin", origin.Scheme+"://"+origin.Host)
		}
	}
	for key, value := range spec.Headers {
		key, value = strings.TrimSpace(key), kazumiApplyVars(value, vars)
		if key == "" || value == "" {
			continue
		}
		request.Header.Set(key, value)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	timeout := kazumiTimeout
	if background, _ := ctx.Value(backgroundCatalogKey{}).(bool); background {
		timeout = kazumiBackgroundTimeout
	}
	response, err := d.doCatalogRequestWithTimeout(request, timeout)
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, kazumiMaxBodyBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(payload) > kazumiMaxBodyBytes {
		return nil, fmt.Errorf("Kazumi 接口响应超过 %d 字节", kazumiMaxBodyBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || catalogResponseBlockReason(response, payload) != "" {
		return nil, d.catalogResponseError(request, response, payload)
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	// 双 Decode：上游偶尔在 JSON 后面追加调试输出，直接 Unmarshal 会失败。
	if decoder.Decode(&decoded) != nil {
		if json.Unmarshal(payload, &decoded) != nil {
			return nil, errors.New("Kazumi 接口返回的不是有效 JSON")
		}
	}
	return decoded, nil
}

// kazumiSearch 目录 / 搜索：Kazumi 只有一个搜索接口，空关键词即「最近更新」。
func (d *Downloader) kazumiSearch(
	ctx context.Context,
	site kazumiSite,
	keyword string,
	page int,
) ([]Drama, bool, error) {
	if page < 1 || page > kazumiMaxPage {
		return nil, false, errors.New("Kazumi 目录页码无效")
	}
	keyword = strings.TrimSpace(keyword)
	if len([]rune(keyword)) > kazumiMaxKeyword {
		return nil, false, errors.New("搜索关键词过长")
	}
	spec, paged := kazumiPaginate(site.Rule.Search.Request, page)
	payload, err := d.kazumiRequest(ctx, site, spec, map[string]string{"keyword": keyword})
	if err != nil {
		return nil, false, err
	}
	rows := kazumiRows(kazumiJSONPath(payload, site.Rule.Search.List))
	if len(rows) > 500 {
		return nil, false, errors.New("Kazumi 接口返回数据过多，疑似异常响应")
	}
	items := make([]Drama, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		name := kazumiText(kazumiJSONPath(row, site.Rule.Search.Name))
		sourceKey := kazumiText(kazumiJSONPath(row, site.Rule.Search.Source))
		drama, err := kazumiDrama(site, row, name, sourceKey)
		if err != nil {
			continue
		}
		if seen[drama.ID] {
			continue
		}
		seen[drama.ID] = true
		items = append(items, drama)
	}
	if len(items) == 0 {
		return nil, false, nil
	}
	// 只有规则自带分页参数时才敢说「还有下一页」：否则每次刷新都是同一页，
	// 翻页会变成重复数据。
	size := kazumiDefaultPageSize
	if declared := kazumiIntValue(spec, kazumiSizeKeys); declared > 0 && declared <= 200 {
		size = declared
	}
	hasMore := paged && len(items) >= size
	return items, hasMore, nil
}

// kazumiDrama 把规则返回的一条记录映射成 Drama。
//
// Kazumi 规则没有「简介 / 封面 / 标签」的配置项（官方 App 也只用这四个字段
// 渲染搜索结果），所以这里按常见 key 做启发式取值：不认识就留空，
// 详情页补全接口（app_cover_metadata）另有通道。
func kazumiDrama(site kazumiSite, row map[string]any, name, sourceKey string) (Drama, error) {
	name = strings.TrimSpace(name)
	sourceKey = strings.TrimSpace(sourceKey)
	if name == "" || sourceKey == "" || len(sourceKey) > kazumiMaxSourceID {
		return Drama{}, errors.New("Kazumi 记录的剧名或 ID 无效")
	}
	source := kazumiSourceID(site.Key)
	cover := kazumiCover(site, firstAnyValue(row,
		"cover_url", "coverUrl", "cover", "pic", "vod_pic", "image", "img", "poster", "thumb"))
	description := kazumiText(firstAnyValue(row,
		"description", "desc", "intro", "summary", "vod_content", "vod_blurb", "blurb", "vod_plot"))
	tags := kazumiTags(row)
	category := kazumiText(firstAnyValue(row, "category", "type_name", "anime_type", "vod_class"))
	if nested, ok := row["anime_type"].(map[string]any); ok {
		category = firstNonEmpty(kazumiText(nested["name"]), category)
	}
	if category == "" {
		category = "动漫"
	}
	return Drama{
		ID:            providerDramaID(source, sourceKey),
		Source:        source,
		SourceID:      sourceKey,
		Title:         truncate(name, kazumiMaxIDKey*2),
		Name:          truncate(name, kazumiMaxIDKey*2),
		Desc:          truncate(description, 12000),
		Intro:         truncate(description, 12000),
		Cover:         cover,
		CoverURL:      cover,
		Category:      truncate(category, 64),
		Tags:          tags,
		ChannelName:   site.Name,
		TotalEpisode:  kazumiInt(row, "total_episodes", "episodeCount", "vod_total", "vod_serial"),
		EpisodeCount:  kazumiInt(row, "current_episodes", "total_episodes", "vod_serial"),
		Score:         kazumiScore(row),
		Actor:         truncate(kazumiActor(row), 256),
		Director:      truncate(kazumiText(firstAnyValue(row, "director", "vod_director")), 128),
		ReleaseStatus: kazumiReleaseStatus(row),
		OnlineDate:    kazumiText(firstAnyValue(row, "release_date", "vod_pubdate", "vod_time_add", "pubdate")),
		UpdateTime:    kazumiText(firstAnyValue(row, "updated_at", "vod_time", "vod_time_add")),
		Remark:        truncate(kazumiText(firstAnyValue(row, "remarks", "vod_remarks", "state")), 64),
		Views:         truncate(kazumiText(firstAnyValue(row, "view_count", "vod_hits", "hits")), 32),
	}, nil
}

// kazumiCover 封面地址：绝对地址直接使用，相对地址按站点地址补全。
func kazumiCover(site kazumiSite, value any) string {
	base := firstNonEmpty(site.Rule.BaseURL, "https://"+kazumiHostOf(site))
	if base == "" {
		base = site.Rule.BaseURL
	}
	if address := providerCoverAddress(value, strings.TrimRight(base, "/")+"/"); address != "" {
		return address
	}
	text := strings.TrimSpace(kazumiText(value))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "//") {
		text = "https:" + text
	}
	if parsed, err := url.Parse(text); err == nil && validNativeCoverURL(parsed) {
		return text
	}
	return ""
}

// kazumiHostOf 规则没有可用 baseURL 时用的主域名占位（仅用于相对地址补全）。
func kazumiHostOf(site kazumiSite) string {
	if site.Host != "" {
		return site.Host
	}
	return "localhost.invalid"
}

// kazumiInt 取记录里的数字字段（0 表示没有 / 无效）。
func kazumiInt(row map[string]any, keys ...string) int {
	for _, key := range keys {
		text := kazumiText(row[key])
		if text == "" {
			continue
		}
		if number, err := strconv.Atoi(text); err == nil && number > 0 && number <= kazumiMaxEpisodes {
			return number
		}
	}
	return 0
}

// kazumiScore 评分：MacCMS 常见 10 分制，Bangumi 常见 10 分制，统一保留原值。
func kazumiScore(row map[string]any) string {
	text := kazumiText(firstAnyValue(row, "bangumi_score", "score", "vod_score", "vod_douban_score"))
	if value, err := strconv.ParseFloat(text, 64); err != nil || value <= 0 || value > 10 {
		return ""
	}
	return truncate(text, 16)
}

// kazumiActor 声优 / 演员：数组或斜杠串都能接收。
func kazumiActor(row map[string]any) string {
	switch typed := firstAnyValue(row, "actors", "vod_actor", "actor", "cast").(type) {
	case []any:
		var parts []string
		for _, entry := range typed {
			if item, ok := entry.(map[string]any); ok {
				text := firstNonEmpty(kazumiText(item["person_name"]), kazumiText(item["name"]))
				if text != "" {
					parts = append(parts, text)
				}
				continue
			}
			if text := kazumiText(entry); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " / ")
	default:
		return kazumiText(typed)
	}
}

// kazumiReleaseStatus 完结判定：Kazumi 侧用 is_finished，MacCMS 用 vod_isend。
func kazumiReleaseStatus(row map[string]any) string {
	if value, ok := row["is_finished"].(bool); ok {
		if value {
			return "finished"
		}
		return "ongoing"
	}
	switch strings.TrimSpace(kazumiText(firstAnyValue(row, "vod_isend", "isend"))) {
	case "1", "true":
		return "finished"
	case "0", "false":
		return "ongoing"
	}
	remarks := kazumiText(firstAnyValue(row, "vod_remarks", "remarks"))
	if strings.Contains(remarks, "完结") {
		return "finished"
	}
	return ""
}

// kazumiTags 标签：只挑短字段，避免把整段简介塞进标签。
func kazumiTags(row map[string]any) []string {
	var tags []string
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || len([]rune(value)) > 24 || len(tags) >= 12 {
			return
		}
		seen[value] = true
		tags = append(tags, value)
	}
	for _, value := range mapStringSlice(row, "meta_tags", "tags", "tagList", "genre") {
		add(value)
	}
	if values, ok := firstAnyValue(row, "meta_tags", "tags").([]any); ok {
		for _, entry := range values {
			add(kazumiText(entry))
		}
	}
	if text := kazumiText(firstAnyValue(row, "region", "vod_area", "area")); text != "" {
		add(text)
	}
	if text := kazumiText(firstAnyValue(row, "release_year", "vod_year", "year")); text != "" {
		add(leadingYear(text) + "年")
	}
	if text := kazumiScore(row); text != "" {
		add("评分 " + text)
	}
	return tags
}

// kazumiDetail 详情：详情接口与分集接口在 Kazumi 里是同一个（Kazumi 自己
// 「点进详情」= 调一次 chapterApiConfig），这里一次请求同时产出
// 详情 Drama 与分集列表。
func (d *Downloader) kazumiDetail(ctx context.Context, site kazumiSite, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(sourceID) > kazumiMaxSourceID {
		return Drama{}, nil, errors.New("Kazumi 剧集 ID 无效")
	}
	payload, err := d.kazumiRequest(ctx, site, site.Rule.Chapter.Request, map[string]string{"source": sourceID})
	if err != nil {
		return Drama{}, nil, err
	}
	root := kazumiMap(payload)
	drama, err := kazumiDetailDrama(site, root, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	lines := kazumiLines(site.Rule, root, sourceID)
	chapters := ruleChapters(kazumiSourceID(site.Key), sourceID,
		firstNonEmpty(site.Rule.Referer, strings.TrimRight(site.Rule.BaseURL, "/")+"/"), lines)
	if len(chapters) == 0 {
		return Drama{}, nil, errors.New("Kazumi 规则未取到可播放分集")
	}
	// Drama 的这些计数字段是 any（要兼容各源的字段名与类型），这里补齐成
	// 实际分集数：目录页给的总数可能滞后于详情页的实时分集。
	total := kazumiIntOf(drama.TotalEpisode)
	if len(chapters) > total {
		total = len(chapters)
	}
	drama.EpisodeCount, drama.TotalEpisode = len(chapters), total
	return drama, chapters, nil
}

// kazumiDetailDrama 详情接口里的剧集信息。
//
// 详情响应的「剧集对象」位置各家不同：xfdmnext 在 $.anime，
// giriGiriLove 在 $.info（MacCMS 惯例）。规则里没有这个路径的配置项，
// 按惯例逐个试；都取不到时退回响应根（此时至少分集路径是可用的）。
func kazumiDetailDrama(site kazumiSite, root map[string]any, sourceID string) (Drama, error) {
	for _, key := range []string{"info", "anime", "data", "result", "detail"} {
		row := kazumiMap(root[key])
		if len(row) == 0 {
			continue
		}
		name := kazumiText(firstAnyValue(row, "title", "name", "vod_name", "anime_name"))
		if name == "" {
			continue
		}
		// 详情对象里的 id 不一定等于请求的 id（xfdmnext 的 $.id 是 anime id，
		// $.series_id 才是请求用的那个）。剧集 ID 一律按请求的 sourceID，
		// 否则详情返回的剧集 ID 与列表不一致，history / 追剧会撕裂。
		return kazumiDrama(site, row, name, sourceID)
	}
	return kazumiDrama(site, root, "", "")
}

// kazumiIntOf 把 any 计数（各源字段类型不一）转成 int，非法按 0。
func kazumiIntOf(value any) int {
	text := kazumiText(value)
	if text == "" {
		return 0
	}
	number, err := strconv.Atoi(text)
	if err != nil || number < 0 || number > kazumiMaxEpisodes {
		return 0
	}
	return number
}

// kazumiLines 按 nested / delimited 两种形态取出线路与分集。
func kazumiLines(rule kazumiRule, payload any, sourceID string) []ruleLine {
	var lines []ruleLine
	if rule.Chapter.Format == "delimited" {
		lines = append(lines, kazumiDelimitedLines(rule, payload)...)
	} else {
		lines = append(lines, kazumiNestedLines(rule, payload)...)
	}
	if len(lines) > kazumiMaxLines {
		lines = lines[:kazumiMaxLines]
	}
	vars := map[string]string{"source": sourceID, "episodeUrl": ""}
	for index := range lines {
		for position, episode := range lines[index].Episodes {
			address := strings.TrimSpace(episode.URL)
			if !kazumiLooksLikeMedia(address) && address != "" && rule.Chapter.PageURL != "" {
				vars["episodeUrl"] = address
				lines[index].Episodes[position].URL = kazumiApplyVars(rule.Chapter.PageURL, vars)
			}
		}
	}
	return lines
}

// kazumiNestedLines 线路数组 → 分集数组（xfdmnext）。
func kazumiNestedLines(rule kazumiRule, payload any) []ruleLine {
	var lines []ruleLine
	for _, entry := range kazumiRows(kazumiJSONPath(payload, rule.Chapter.Roads)) {
		name := strings.TrimSpace(kazumiText(kazumiJSONPath(entry, rule.Chapter.RoadName)))
		if name == "" {
			name = "线路"
		}
		episodes := kazumiRows(kazumiJSONPath(entry, rule.Chapter.Episodes))
		var items []ruleEpisode
		for _, episode := range episodes {
			title := strings.TrimSpace(kazumiText(kazumiJSONPath(episode, rule.Chapter.EpisodeName)))
			address := strings.TrimSpace(kazumiText(kazumiJSONPath(episode, rule.Chapter.EpisodeURL)))
			if number := kazumiInt(episode, "episode_number"); number > 0 && title == "" {
				title = fmt.Sprintf("第 %d 集", number)
			}
			if address == "" && rule.Chapter.PageURL == "" {
				continue
			}
			items = append(items, ruleEpisode{Title: title, URL: address})
			if len(items) >= kazumiMaxEpisodes {
				break
			}
		}
		if len(items) == 0 {
			continue
		}
		lines = append(lines, ruleLine{Title: name, Episodes: items})
	}
	return lines
}

// kazumiDelimitedLines 分隔符串形态（MacCMS：线路名 $$$ 分集名$地址#分集名$地址）。
func kazumiDelimitedLines(rule kazumiRule, payload any) []ruleLine {
	names := strings.Split(kazumiText(kazumiJSONPath(payload, rule.Chapter.RoadNames)), rule.Chapter.RoadSep)
	groups := strings.Split(kazumiText(kazumiJSONPath(payload, rule.Chapter.RoadEpisodes)), rule.Chapter.RoadSep)
	var lines []ruleLine
	for index, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		name := "线路"
		if index < len(names) && strings.TrimSpace(names[index]) != "" {
			name = strings.TrimSpace(names[index])
		}
		var items []ruleEpisode
		for _, chunk := range strings.Split(group, rule.Chapter.EpisodeSep) {
			chunk = strings.TrimSpace(chunk)
			if chunk == "" {
				continue
			}
			title, address := "", ""
			if parts := strings.SplitN(chunk, rule.Chapter.FieldSep, 2); len(parts) == 2 {
				title, address = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			} else {
				title, address = chunk, ""
			}
			if address == "" {
				continue
			}
			items = append(items, ruleEpisode{Title: title, URL: address})
			if len(items) >= kazumiMaxEpisodes {
				break
			}
		}
		if len(items) == 0 {
			continue
		}
		lines = append(lines, ruleLine{Title: name, Episodes: items})
	}
	return lines
}

// kazumiLooksLikeMedia 判断地址是否已经是媒体直链。
//
// Dart 侧判断同一个正则（WebViewSniffer.looksLikeMediaUrl）；Go 的
// regexp 是 RE2、没有前瞻断言，这里手写等价判断：扩展名后只能跟
// 非字母数字或结束（避免把 xxx.mp4download 误判成直链）。
func kazumiLooksLikeMedia(address string) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if path == "" {
		return false
	}
	for _, extension := range []string{".m3u8", ".mp4", ".m4s", ".flv", ".mkv", ".mov", ".ts", ".webm"} {
		position := strings.LastIndex(path, extension)
		if position < 0 {
			continue
		}
		rest := path[position+len(extension):]
		if rest == "" {
			return true
		}
		if char := rest[0]; char < '0' || (char > '9' && char < 'a') || char > 'z' {
			return true
		}
	}
	return false
}

// resolveKazumiMedia 播放地址解析。
//
// 直链（giriGiriLove 的 mp4）直接交给通用收尾；播放页（xfdmnext）在 Go 侧
// 只能用静态嗅探兜底（下载链路 / 无 WebView 平台），正常播放由 Dart 侧
// WebViewSniffer 先把 videoUrl 换成直链再进来。
func (d *Downloader) resolveKazumiMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, _, valid := splitProviderDramaID(task.DramaID)
	site, ok := d.kazumiSite(source)
	if !valid || !ok {
		return providerMedia{}, errors.New("Kazumi 站源信息无效，请在站源管理确认该规则仍在列表中")
	}
	address := strings.TrimSpace(task.Chapter.VideoURL)
	referer := firstNonEmpty(strings.TrimSpace(task.Chapter.Referer),
		site.Rule.Referer, strings.TrimRight(site.Rule.BaseURL, "/")+"/")
	if !isProviderHTTPMediaURL(address) {
		return providerMedia{}, errors.New("Kazumi 分集地址无效，请刷新详情")
	}
	if kazumiLooksLikeMedia(address) {
		return d.finishProviderMedia(ctx, providerMedia{URL: address, Referer: referer}, source)
	}
	// 播放页：先静态嗅探（抓 HTML / JSON 里的直链），失败给明确提示而不是
	// 把网页塞给播放器。
	sniffed, err := d.drpySniffPlayPage(ctx, address, referer)
	if err != nil {
		return providerMedia{}, err
	}
	if !kazumiLooksLikeMedia(sniffed) {
		return providerMedia{}, errors.New("该 Kazumi 站源需要浏览器嗅探真实地址，请在播放页播放（下载暂不支持）")
	}
	return d.finishProviderMedia(ctx, providerMedia{URL: sniffed, Referer: referer}, source)
}

// kazumiSourceID 完整源 ID：kazumi:<key>。
func kazumiSourceID(key string) string {
	return sourceKazumiPrefix + strings.TrimSpace(key)
}

// kazumiKeyFromSource 由完整源 ID 反解 rule key；非本家族返回空串。
func kazumiKeyFromSource(source string) string {
	trimmed := strings.TrimSpace(source)
	if !strings.HasPrefix(trimmed, sourceKazumiPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, sourceKazumiPrefix))
}

// kazumiSite 按完整源 ID 取站点注册信息。
func (d *Downloader) kazumiSite(source string) (kazumiSite, bool) {
	if d == nil || d.kazumi == nil {
		return kazumiSite{}, false
	}
	return d.kazumi.get(source)
}
