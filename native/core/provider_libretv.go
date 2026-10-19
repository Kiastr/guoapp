package core

// LibreTV 格式片源（Apple CMS / MacCMS v10 provide/vod JSON 采集接口）。
//
// 背景：LibreTV / TVBox 生态的「片源链接」分两种形态：
//  1. 完整订阅配置：JSON，形如 {"sites":[{"key":"xx","name":"某某","type":1,
//     "api":"https://host/api.php/provide/vod"}]}（type=1 即 MacCMS 采集接口，
//     type=2 是 XML 采集接口；type≥3 的 drpy/spider 脚本地址会收成 drpy
//     候选，由用户在站源管理一键导入）；
//  2. 裸采集接口地址：形如 https://host/api.php/provide/vod，
//     LibreTV 里即一个 type=1 站点。
//
// 本文件把上述形态归一成运行时站源：源 ID 为 "libretv:<key>"，
// 剧集 ID 为 "libretv:<key>:<vodId>"，分集 ID 为 "libretv:<key>:<vodId>:<集键>"。
// 播放地址由 vod_play_url 直接给出（m3u8/mp4），复用既有的通用解析链路
// （resolveProviderMedia 对直链做 M3U8 拉取与密钥处理），不需要二级解析。
//
// 订阅持久化在引擎目录 libretv.json，导入时逐个探测（共享预算，避免拖垮
// 导入请求），探测失败的站点也会保留，用户可在站源管理里更新/检测/删除。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// sourceLibreTV LibreTV 站源家族名（完整源 ID 为 sourceLibreTV:<key>）。
	sourceLibreTV = "libretv"
	// sourceLibreTVPrefix 运行时 LibreTV 站源的 ID 前缀。
	sourceLibreTVPrefix = sourceLibreTV + ":"
	// libretvImportMaxSites 单次订阅导入的站点上限，防止异常配置刷爆列表。
	libretvImportMaxSites = 50
	// libretvProbeBudget 导入时探测总预算（所有站点共享）。
	libretvProbeBudget = 15 * time.Second
	// libretvProbePerSite 单个站点探测超时上限。
	libretvProbePerSite = 5 * time.Second
	// libretvSubscriptionMaxBytes 订阅内容大小上限（512KB）。
	libretvSubscriptionMaxBytes = 512 << 10
)

// libretvSite 一个已导入的 LibreTV 采集站点。
type libretvSite struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	API  string `json:"api"`
	// Type TVBox 站点类型：0/1/缺省 = MacCMS provide/vod JSON 采集接口，
	// 2 = MacCMS XML 采集接口（at/xml）。旧版 libretv.json 没有该字段，
	// 反序列化为 0，按 JSON 处理，向后兼容。
	Type int `json:"type,omitempty"`
}

// libretvNormalizeSiteType 归一化站点类型：只区分「XML(2)」与「JSON(其余)」。
func libretvNormalizeSiteType(siteType int) int {
	if siteType == 2 {
		return 2
	}
	return 1
}

// looksLikeLibreTVXMLAPI 由接口地址判断是否更像 XML 采集接口。
// 苹果CMS 的 XML 版本通常写成 /at/xml/ 路径或带 at=xml / ac=xml 参数。
func looksLikeLibreTVXMLAPI(raw string) bool {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(lowered, "/at/xml") ||
		strings.Contains(lowered, "at=xml") ||
		strings.Contains(lowered, "ac=xml")
}

// libretvStore 站源订阅表（进程内 + libretv.json 落盘）。
type libretvStore struct {
	directory string
	mu        chan struct{}
	sites     map[string]libretvSite
}

func newLibreTVStore(directory string) *libretvStore {
	return &libretvStore{directory: directory, mu: make(chan struct{}, 1), sites: map[string]libretvSite{}}
}

// loadLibreTVStore 从引擎目录读取已保存的订阅；文件缺失或损坏时返回空表。
func loadLibreTVStore(directory string) *libretvStore {
	store := newLibreTVStore(directory)
	body, err := os.ReadFile(filepath.Join(directory, "libretv.json"))
	if err != nil || len(body) > libretvSubscriptionMaxBytes {
		return store
	}
	var payload struct {
		Sites []libretvSite `json:"sites"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return store
	}
	for _, site := range payload.Sites {
		if key, name, api, ok := normalizeLibreTVSite(site.Key, site.Name, site.API, site.Type); ok {
			store.sites[key] = libretvSite{Key: key, Name: name, API: api, Type: libretvNormalizeSiteType(site.Type)}
		}
	}
	return store
}

// saveLocked 写盘（调用方需持有 mu）。
func (store *libretvStore) saveLocked() error {
	keys := make([]string, 0, len(store.sites))
	for key := range store.sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	payload := struct {
		Sites []libretvSite `json:"sites"`
	}{Sites: make([]libretvSite, 0, len(keys))}
	for _, key := range keys {
		payload.Sites = append(payload.Sites, store.sites[key])
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeNativeCacheFile(filepath.Join(store.directory, "libretv.json"), body)
}

func (store *libretvStore) lock()   { store.mu <- struct{}{} }
func (store *libretvStore) unlock() { <-store.mu }

// get 按完整源 ID（libretv:<key>）取站点。
func (store *libretvStore) get(source string) (libretvSite, bool) {
	key := libretvKeyFromSource(source)
	if key == "" {
		return libretvSite{}, false
	}
	store.lock()
	defer store.unlock()
	site, ok := store.sites[key]
	return site, ok
}

// put 登记站点并落盘；返回是否新增（false 表示已存在同 key 站点被覆盖）。
func (store *libretvStore) put(site libretvSite) (bool, error) {
	store.lock()
	defer store.unlock()
	_, existed := store.sites[site.Key]
	store.sites[site.Key] = site
	return !existed, store.saveLocked()
}

// remove 删除站点并落盘。
func (store *libretvStore) remove(key string) error {
	store.lock()
	defer store.unlock()
	if _, ok := store.sites[key]; !ok {
		return nil
	}
	delete(store.sites, key)
	return store.saveLocked()
}

// list 返回全部站点（按 key 排序，稳定输出）。
func (store *libretvStore) list() []libretvSite {
	store.lock()
	defer store.unlock()
	keys := make([]string, 0, len(store.sites))
	for key := range store.sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]libretvSite, 0, len(keys))
	for _, key := range keys {
		out = append(out, store.sites[key])
	}
	return out
}

// libretvSite 按完整源 ID（libretv:<key>）取站点；Downloader 便捷方法。
func (d *Downloader) libretvSite(source string) (libretvSite, bool) {
	if d.libretv == nil {
		return libretvSite{}, false
	}
	return d.libretv.get(source)
}

// libretvKeyFromSource 从完整源 ID 取出站点 key；不是 libretv 源时返回空串。
func libretvKeyFromSource(source string) string {
	trimmed := strings.TrimSpace(source)
	if !strings.HasPrefix(trimmed, sourceLibreTVPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, sourceLibreTVPrefix))
}

// libretvSourceID 站点 key → 完整源 ID。
func libretvSourceID(key string) string {
	return sourceLibreTVPrefix + strings.TrimSpace(key)
}

var libretvKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

// normalizeLibreTVSite 校验并归一化站点字段：key 缺省时由接口地址派生，
// name 缺省时回落为接口主机名，api 统一去掉末尾斜杠，type 归一为 JSON/XML 两档。
func normalizeLibreTVSite(rawKey, rawName, rawAPI string, siteType int) (key, name, api string, ok bool) {
	api = strings.TrimSpace(rawAPI)
	parsed, err := url.Parse(api)
	if err != nil || !isProviderHTTPMediaURL(api) || parsed.Path == "" {
		return "", "", "", false
	}
	api = strings.TrimRight(api, "/")
	name = strings.TrimSpace(rawName)
	if name == "" || len([]rune(name)) > 48 {
		if name == "" {
			name = parsed.Hostname()
		} else {
			name = string([]rune(name)[:48])
		}
	}
	if name == "" {
		name = "LibreTV 片源"
	}
	key = strings.TrimSpace(rawKey)
	if !libretvKeyPattern.MatchString(key) {
		key = libretvKeyFromAPI(api)
	}
	return key, name, api, true
}

// libretvKeyFromAPI 由接口地址派生稳定 key：主机名 slug + 地址哈希前 6 位。
func libretvKeyFromAPI(api string) string {
	host := "site"
	if parsed, err := url.Parse(api); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	}
	slug := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(host), "")
	if slug == "" {
		slug = "site"
	}
	if len(slug) > 16 {
		slug = slug[:16]
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(api))))
	return slug + "-" + hex.EncodeToString(sum[:])[:6]
}

// libretvConfigSite LibreTV 订阅配置里的 sites 条目。
type libretvConfigSite struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	API  string `json:"api"`
	Type int    `json:"type"`
}

// libretvDisplayName 配置条目的展示名（报错用）。
func libretvDisplayName(entry libretvConfigSite) string {
	if name := strings.TrimSpace(entry.Name); name != "" {
		return name
	}
	if key := strings.TrimSpace(entry.Key); key != "" {
		return key
	}
	if api := strings.TrimSpace(entry.API); api != "" {
		return api
	}
	return "未命名站点"
}

// libretvExtractJSONObject 从订阅内容里截取 JSON 对象：
// 兼容纯 JSON、var config = {...}; 之类的 JS 包裹、以及带前后缀杂文本的情况。
func libretvExtractJSONObject(body string) string {
	trimmed := strings.TrimSpace(body)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return ""
	}
	return trimmed[start : end+1]
}

// drpyCandidate 混合订阅里发现的 drpy 脚本候选：type≥3 且 api 是 .js
// 脚本地址。不自动导入（脚本需要单独校验），由用户在站源管理一键导入。
type drpyCandidate struct {
	Name string
	API  string
}

// looksLikeDrpyScriptURL 判断订阅里的 api 是否像 drpy 脚本地址
// （http(s) + 路径 .js 结尾，容忍查询串）。
func looksLikeDrpyScriptURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(parsed.Path), ".js")
}

// parseLibreTVSubscription 解析 LibreTV 订阅配置，返回可导入站点、drpy
// 脚本候选与被跳过原因。type≥3 且 api 是 .js 脚本地址的收成 drpy 候选
// （用户在站源管理一键导入）；type≥3 的非脚本地址（csp_ 规则等）与历史
// 行为一致带原因跳过；type=0/1/缺省按 JSON 采集接口导入，type=2 按 XML
// 采集接口导入（TVBox 的苹果CMS XML 版本）。
func parseLibreTVSubscription(body string) (sites []libretvSite, drpyCandidates []drpyCandidate, skipped []string, err error) {
	document := libretvExtractJSONObject(body)
	if document == "" {
		return nil, nil, nil, errors.New("未找到有效的 LibreTV 配置（需要包含 sites 的 JSON）")
	}
	var config struct {
		Sites []libretvConfigSite `json:"sites"`
	}
	if json.Unmarshal([]byte(document), &config) != nil {
		return nil, nil, nil, errors.New("配置解析失败，请确认是 LibreTV/TVBox 格式的 JSON")
	}
	if len(config.Sites) == 0 {
		return nil, nil, nil, errors.New("配置里没有 sites 条目")
	}
	seenAPI := map[string]bool{}
	usedKey := map[string]bool{}
	for _, entry := range config.Sites {
		api := strings.TrimSpace(entry.API)
		if api == "" {
			skipped = append(skipped, fmt.Sprintf("%s：缺少 api 地址", libretvDisplayName(entry)))
			continue
		}
		if entry.Type >= 3 {
			// drpy/spider 脚本：.js 地址收成候选，其余（csp_ 规则等）不支持。
			if looksLikeDrpyScriptURL(api) && !seenAPI[api] {
				drpyCandidates = append(drpyCandidates, drpyCandidate{Name: libretvDisplayName(entry), API: api})
				seenAPI[api] = true
				continue
			}
			if !looksLikeDrpyScriptURL(api) {
				skipped = append(skipped, fmt.Sprintf("%s：type=%d 类型暂不支持（仅支持 provide/vod 采集接口与 drpy 脚本）", libretvDisplayName(entry), entry.Type))
			}
			continue
		}
		key, name, normalized, ok := normalizeLibreTVSite(entry.Key, entry.Name, api, entry.Type)
		if !ok {
			skipped = append(skipped, fmt.Sprintf("%s：接口地址无效", libretvDisplayName(entry)))
			continue
		}
		if seenAPI[normalized] {
			continue // 同一接口重复出现，静默去重
		}
		for usedKey[key] { // key 冲突时退化为地址派生 key
			key = libretvKeyFromAPI(normalized + "/" + strconv.Itoa(len(usedKey)))
		}
		seenAPI[normalized] = true
		usedKey[key] = true
		sites = append(sites, libretvSite{Key: key, Name: name, API: normalized, Type: libretvNormalizeSiteType(entry.Type)})
		if len(sites) >= libretvImportMaxSites {
			skipped = append(skipped, fmt.Sprintf("站点数量超过 %d 个，仅导入前 %d 个", libretvImportMaxSites, libretvImportMaxSites))
			break
		}
	}
	// 只有 drpy 候选时不算失败：用户可一键导入脚本源。
	if len(sites) == 0 && len(drpyCandidates) == 0 {
		return nil, nil, skipped, errors.New("配置里没有可导入的采集接口站点")
	}
	return sites, drpyCandidates, skipped, nil
}

// looksLikeMacCMSAPI 判断裸地址是否像 MacCMS 采集接口（用于配置解析失败的兜底）。
func looksLikeMacCMSAPI(raw string) bool {
	lowered := strings.ToLower(raw)
	return strings.Contains(lowered, "provide/vod") ||
		strings.Contains(lowered, "api.php") ||
		strings.HasSuffix(lowered, "/api")
}

// importLibreTVSubscription 导入订阅：输入可以是配置链接、配置 JSON 文本，
// 或裸 provide/vod 接口地址。导入后按共享预算逐个探测，探测失败的站点同样保留
// （用户可在站源管理里更新/检测/删除），避免一次网络抖动整批丢弃。
func (engine *nativeEngine) importLibreTVSubscription(ctx context.Context, raw string) (any, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, errors.New("请输入 LibreTV 片源链接或配置内容")
	}
	if len(text) > libretvSubscriptionMaxBytes {
		return nil, errors.New("内容过大，请确认粘贴的是片源配置")
	}
	d := engine.downloader
	var body string
	if isProviderHTTPMediaURL(text) && !strings.ContainsAny(text, "\n\r") {
		fetched, err := d.fetchProviderText(ctx, text, text)
		if err != nil {
			return nil, fmt.Errorf("片源链接获取失败：%w", publicError(err))
		}
		body = fetched
	} else {
		body = text
	}
	sites, drpyCandidates, skipped, err := parseLibreTVSubscription(body)
	if err != nil && looksLikeLibreTVBase58(body) {
		// 社区配置工具（LunaTV-config 等）的 Base58 订阅：format=2 是原始 JSON、
		// format=3 是带代理前缀的 JSON，解码后复用同一套解析，客户端不需要
		// 实时解析 type≥3 脚本，也不需要自建后端。
		if decoded, decodeErr := libretvBase58Decode(body); decodeErr == nil {
			decodedSites, decodedDrpy, decodedSkipped, decodedErr := parseLibreTVSubscription(string(decoded))
			// 部分站点不可用时也保留跳过原因（例如配置里混了 drpy 源）。
			if decodedErr == nil || len(decodedSkipped) > 0 {
				sites, drpyCandidates, skipped, err = decodedSites, decodedDrpy, decodedSkipped, decodedErr
			}
		}
	}
	if err != nil {
		// 兜底：输入本身就是裸采集接口地址时，按单站点导入。
		// 地址形如 /at/xml 的按 XML 采集站处理，其余按 JSON 采集站处理。
		if isProviderHTTPMediaURL(text) && looksLikeMacCMSAPI(text) {
			siteType := 1
			if looksLikeLibreTVXMLAPI(text) {
				siteType = 2
			}
			if key, name, api, ok := normalizeLibreTVSite("", "", text, siteType); ok {
				sites, skipped, err = []libretvSite{{Key: key, Name: name, API: api, Type: siteType}}, nil, nil
			}
		}
		if err != nil {
			return nil, err
		}
	}
	store := d.libretv
	added := make([]map[string]any, 0, len(sites))
	verified := 0
	budget := libretvProbeBudget
	for _, site := range sites {
		if _, err := store.put(site); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s：保存失败（%s）", site.Name, publicError(err)))
			continue
		}
		ok := false
		if budget > 0 {
			per := libretvProbePerSite
			if per > budget {
				per = budget
			}
			started := time.Now()
			probeCtx, cancel := context.WithTimeout(ctx, per)
			// 直接走目录链路：既验证接口，又把首页写进缓存，
			// 导入完成后站源管理里立刻能看到剧集数。
			page, probeErr := engine.nativeCatalog(probeCtx, nativeInput{Source: libretvSourceID(site.Key), Page: 1, Force: true})
			cancel()
			budget -= time.Since(started)
			ok = probeErr == nil && len(page.Items) > 0 && page.Warning == ""
			if ok {
				verified++
			}
		}
		added = append(added, map[string]any{"key": site.Key, "name": site.Name, "api": site.API, "type": site.Type, "verified": ok})
	}
	// 订阅变更后 Dart 侧会重新拉取列表，无需额外广播。
	// drpy 候选不自动导入（脚本需要用户确认后才加入），仅透传给 Dart 侧展示。
	drpy := make([]map[string]any, 0, len(drpyCandidates))
	for _, candidate := range drpyCandidates {
		drpy = append(drpy, map[string]any{"name": candidate.Name, "api": candidate.API})
	}
	return map[string]any{
		"added":    added,
		"skipped":  skipped,
		"verified": verified,
		"total":    len(added),
		"drpy":     drpy,
	}, nil
}

// removeLibreTVSite 删除已导入站点，并清掉它的目录缓存与任务记录。
func (engine *nativeEngine) removeLibreTVSite(key string) error {
	key = strings.TrimSpace(key)
	if key == "" || !libretvKeyPattern.MatchString(key) {
		return errors.New("站点 key 无效")
	}
	store := engine.downloader.libretv
	if _, ok := store.get(libretvSourceID(key)); !ok {
		return errors.New("站点不存在或已删除")
	}
	engine.mu.Lock()
	source := libretvSourceID(key)
	if task := engine.sourceTasks[source]; task != nil {
		task.cancel()
		delete(engine.sourceTasks, source)
	}
	for catalogKey := range engine.catalogs {
		if catalogKey == source || strings.HasPrefix(catalogKey, source+"|") {
			delete(engine.catalogs, catalogKey)
		}
	}
	for stateKey := range engine.catalogStates {
		if stateKey == source || strings.HasPrefix(stateKey, source+"|") {
			delete(engine.catalogStates, stateKey)
		}
	}
	for optionKey := range engine.categoryOptions {
		if optionKey == source || strings.HasPrefix(optionKey, source+"|") {
			delete(engine.categoryOptions, optionKey)
		}
	}
	delete(engine.sourceRecords, source)
	saveErr := engine.writeCatalogDiskLocked()
	engine.mu.Unlock()
	if err := store.remove(key); err != nil {
		return err
	}
	engine.saveSourceRecordsFile()
	if saveErr != nil {
		return saveErr
	}
	// 订阅变更后 Dart 侧会重新拉取列表，无需额外广播。
	return nil
}

// saveSourceRecordsFile 立即持久化站源任务记录（删除站点后调用）。
func (engine *nativeEngine) saveSourceRecordsFile() {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.saveSourceRecordsLocked()
}

// libretvSiteList 供 libretvSites 操作返回：站点 + 当前缓存剧集数。
func (engine *nativeEngine) libretvSiteList() []map[string]any {
	store := engine.downloader.libretv
	engine.mu.Lock()
	counts := map[string]int{}
	for catalogKey, items := range engine.catalogs {
		key := libretvKeyFromSource(catalogKey)
		if key == "" {
			continue
		}
		counts[key] += len(items)
	}
	engine.mu.Unlock()
	out := make([]map[string]any, 0)
	for _, site := range store.list() {
		out = append(out, map[string]any{
			"key":   site.Key,
			"name":  site.Name,
			"api":   site.API,
			"type":  site.Type,
			"count": counts[site.Key],
		})
	}
	return out
}

// macCMSResponse MacCMS provide/vod 通用响应信封。
type macCMSResponse struct {
	Code      any    `json:"code"`
	Msg       string `json:"msg"`
	Page      any    `json:"page"`
	PageCount any    `json:"pagecount"`
	Limit     any    `json:"limit"`
	Total     any    `json:"total"`
	List      []struct {
		VodID       any    `json:"vod_id"`
		VodName     string `json:"vod_name"`
		VodPic      string `json:"vod_pic"`
		TypeName    string `json:"type_name"`
		VodRemarks  string `json:"vod_remarks"`
		VodYear     string `json:"vod_year"`
		VodArea     string `json:"vod_area"`
		VodLang     string `json:"vod_lang"`
		VodHits     any    `json:"vod_hits"`
		VodScore    any    `json:"vod_score"`
		VodActor    string `json:"vod_actor"`
		VodDirector string `json:"vod_director"`
		VodContent  string `json:"vod_content"`
		VodPlayFrom string `json:"vod_play_from"`
		VodPlayURL  string `json:"vod_play_url"`
		VodTime     string `json:"vod_time"`
		VodTag      string `json:"vod_tag"`
	} `json:"list"`
	// Class ac=list 的分类列表（type_id/type_name）。
	Class []struct {
		TypeID   any    `json:"type_id"`
		TypeName string `json:"type_name"`
	} `json:"class"`
}

// macCMSListRow 归一化后的列表行（detail/list 两处复用）。
type macCMSListRow struct {
	ID        string
	Name      string
	Pic       string
	TypeName  string
	Remarks   string
	Year      string
	Area      string
	Lang      string
	Hits      string
	Score     string
	Actor     string
	Director  string
	UpdatedAt string
	Tag       string
}

// libretvScoreText 评分归一化：MacCMS 的 vod_score 有的站返回数字、有的返回字符串。
// 不能复用 nativeText——它对 float64 走 strconv.Itoa(int(v)) 截断，8.6 会变成 "8"。
// 0 / 0.0 视为「未评分」返回空串，避免详情页显示「0 分」这种占位。
func libretvScoreText(value any) string {
	text := ""
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		text = strings.TrimSpace(v)
	case json.Number:
		text = strings.TrimSpace(string(v))
	case float64:
		// 'f' + -1 精度：8.6 → "8.6"，8 → "8"，不产生多余尾零。
		text = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		text = strconv.Itoa(v)
	case int64:
		text = strconv.FormatInt(v, 10)
	default:
		text = strings.TrimSpace(nativeText(value))
	}
	switch text {
	case "", "0", "0.0", "0.00", "<nil>":
		return ""
	}
	return text
}

func (response *macCMSResponse) rows() []macCMSListRow {
	out := make([]macCMSListRow, 0, len(response.List))
	for _, row := range response.List {
		id := strings.TrimSpace(nativeText(row.VodID))
		if id == "" {
			continue
		}
		out = append(out, macCMSListRow{
			ID:        id,
			Name:      strings.TrimSpace(row.VodName),
			Pic:       strings.TrimSpace(row.VodPic),
			TypeName:  strings.TrimSpace(row.TypeName),
			Remarks:   strings.TrimSpace(row.VodRemarks),
			Year:      strings.TrimSpace(row.VodYear),
			Area:      strings.TrimSpace(row.VodArea),
			Lang:      strings.TrimSpace(row.VodLang),
			Hits:      strings.TrimSpace(nativeText(row.VodHits)),
			Score:     libretvScoreText(row.VodScore),
			Actor:     strings.TrimSpace(row.VodActor),
			Director:  strings.TrimSpace(row.VodDirector),
			UpdatedAt: strings.TrimSpace(row.VodTime),
			Tag:       strings.TrimSpace(row.VodTag),
		})
	}
	return out
}

// macCMSSuccess MacCMS 成功码：规范是 code=1，部分新站用 200。
func (response *macCMSResponse) macCMSSuccess() bool {
	switch value := response.Code.(type) {
	case float64:
		return value == 1 || value == 200
	case string:
		return value == "1" || value == "200"
	}
	return false
}

// errLibreTVNotJSON 接口返回的不是 JSON —— 用于触发 XML 格式回退。
var errLibreTVNotJSON = errors.New("片源接口返回的不是有效 JSON，可能不是 provide/vod 采集接口")

// libretvAPI 请求一次 provide/vod 接口并解码信封。
func (d *Downloader) libretvAPI(ctx context.Context, site libretvSite, params map[string]string) (*macCMSResponse, error) {
	address, err := url.Parse(site.API)
	if err != nil {
		return nil, errors.New("片源接口地址无效")
	}
	query := address.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	address.RawQuery = query.Encode()
	referer := address.Scheme + "://" + address.Host + "/"
	body, err := d.fetchProviderText(ctx, address.String(), referer)
	if err != nil {
		return nil, err
	}
	var response macCMSResponse
	if json.Unmarshal([]byte(body), &response) != nil {
		return nil, errLibreTVNotJSON
	}
	if !response.macCMSSuccess() && len(response.List) == 0 {
		msg := strings.TrimSpace(response.Msg)
		if msg == "" {
			msg = "接口返回异常"
		}
		return nil, errors.New(msg)
	}
	return &response, nil
}

// libretvRemarksStatus 从「更新至10集 / 全30集 / 已完结」推断连载状态。
var libretvFinishedPattern = regexp.MustCompile(`(?i)(完结|全集|全\s*\d+\s*[集话期]|已完结)`)

func libretvRemarksStatus(remarks string) string {
	switch {
	case libretvFinishedPattern.MatchString(remarks):
		return "finished"
	case strings.Contains(remarks, "更新至") || strings.Contains(remarks, "更新"):
		return "ongoing"
	default:
		return ""
	}
}

// libretvDramaFromRow 列表行 → Drama（详情接口的同名字段直接复用）。
func libretvDramaFromRow(source string, row macCMSListRow, content string) Drama {
	count := episodeIndex(row.Remarks, 0)
	return Drama{
		ID:            providerDramaID(source, row.ID),
		Source:        source,
		SourceID:      row.ID,
		Title:         row.Name,
		Name:          row.Name,
		Cover:         row.Pic,
		TypeName:      row.TypeName,
		Remark:        row.Remarks,
		Score:         row.Score,
		Actor:         row.Actor,
		Director:      row.Director,
		Views:         row.Hits,
		OnlineDate:    row.Year,
		Episodes:      count,
		Desc:          strings.TrimSpace(content),
		Tags:          libretvTags(row),
		ReleaseStatus: libretvRemarksStatus(row.Remarks),
		UpdateTime:    libretvUpdateTime(row.UpdatedAt),
	}
}

// libretvTags 详情页标签：vod_tag 是站点自带的分类标签（逗号分隔），
// 优先保留；再补地区 / 语言 / 年份这些通用维度。去重后最多 12 个。
func libretvTags(row macCMSListRow) []string {
	var tags []string
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || strings.Contains(value, " ") || seen[value] {
			return
		}
		seen[value] = true
		tags = append(tags, value)
	}
	for _, tag := range strings.FieldsFunc(row.Tag, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == '/' || r == '|'
	}) {
		add(tag)
	}
	for _, value := range []string{row.Area, row.Lang, row.Year} {
		add(value)
	}
	if len(tags) > 12 {
		tags = tags[:12]
	}
	return tags
}

// libretvUpdateTime vod_time 归一化：站点常见 "2026-10-19 20:00:00"、
// "2026-10-19 20:00"、时间戳与空串。只保留展示需要的部分，
// 解析失败时原样返回（截断到 19 字符），供详情页直接展示。
func libretvUpdateTime(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	// 纯数字时间戳（秒或毫秒）。
	if digits := text; len(digits) >= 10 && len(digits) <= 13 {
		allDigits := true
		for _, r := range digits {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			var sec int64
			if len(digits) > 10 {
				sec = mustParseInt64(digits) / 1000
			} else {
				sec = mustParseInt64(digits)
			}
			if sec > 0 {
				return time.Unix(sec, 0).Format("2006-01-02 15:04")
			}
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, text, time.Local); err == nil {
			return parsed.Format("2006-01-02 15:04")
		}
	}
	if len(text) > 19 {
		return text[:19]
	}
	return text
}

// mustParseInt64 只用于已确认全为数字的串。
func mustParseInt64(text string) int64 {
	var value int64
	for _, r := range text {
		value = value*10 + int64(r-'0')
	}
	return value
}

// libretvHTMLText 去掉 vod_content 里的 HTML 标签与实体，压缩空白。
var libretvHTMLTag = regexp.MustCompile(`(?is)<[^>]+>`)

func libretvHTMLText(raw string) string {
	text := libretvHTMLTag.ReplaceAllString(raw, " ")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	return strings.Join(strings.Fields(text), " ")
}

// fetchLibreTVCatalogPage 拉取一页列表/搜索结果。
// category 为 MacCMS type_id（数字），query 非空时走 wd 搜索。
func (d *Downloader) fetchLibreTVCatalogPage(ctx context.Context, site libretvSite, page int, category, query string) ([]Drama, bool, error) {
	if site.Type == 2 {
		return d.fetchLibreTVCatalogPageXML(ctx, site, page, category, query)
	}
	params := map[string]string{"ac": "detail"}
	requestedPage := page
	if page > 1 {
		params["pg"] = strconv.Itoa(page)
	}
	if category != "" {
		params["t"] = category
	}
	if query != "" {
		// 搜索固定回到第一页（部分站点 pg 与 wd 并存时会错页）。
		params["wd"] = query
		params["pg"] = "1"
		requestedPage = 1
	}
	response, err := d.libretvAPI(ctx, site, params)
	if err != nil {
		// 站点类型未标注（例如裸地址导入的 XML 接口）时，按响应格式回退。
		if errors.Is(err, errLibreTVNotJSON) {
			return d.fetchLibreTVCatalogPageXML(ctx, site, page, category, query)
		}
		return nil, false, err
	}
	source := libretvSourceID(site.Key)
	var dramas []Drama
	for _, row := range response.rows() {
		if row.Name == "" {
			continue
		}
		dramas = append(dramas, libretvDramaFromRow(source, row, ""))
	}
	hasMore := false
	if pageCount, _ := strconv.Atoi(nativeText(response.PageCount)); pageCount > 0 {
		hasMore = requestedPage < pageCount
	} else {
		limit, _ := strconv.Atoi(nativeText(response.Limit))
		if limit <= 0 {
			limit = 20
		}
		hasMore = len(dramas) >= limit
	}
	return dramas, hasMore, nil
}

// fetchLibreTVCategories 拉取分类（ac=list → class 列表）；站点不支持时只有「全部」。
func (d *Downloader) fetchLibreTVCategories(ctx context.Context, site libretvSite) ([]nativeCategory, error) {
	if site.Type == 2 {
		return d.fetchLibreTVCategoriesXML(ctx, site)
	}
	response, err := d.libretvAPI(ctx, site, map[string]string{"ac": "list"})
	if err != nil {
		if errors.Is(err, errLibreTVNotJSON) {
			return d.fetchLibreTVCategoriesXML(ctx, site)
		}
		return nil, err
	}
	var categories []nativeCategory
	for _, entry := range response.Class {
		id := strings.TrimSpace(nativeText(entry.TypeID))
		name := strings.TrimSpace(entry.TypeName)
		if id == "" || name == "" || len([]rune(name)) > 24 {
			continue
		}
		categories = append(categories, nativeCategory{ID: id, Name: name})
	}
	return categories, nil
}

// fetchLibreTVDetail 拉取详情：剧集元数据 + vod_play_url 解析出的分集。
// vod_play_from / vod_play_url 用 $$$ 分隔多个播放源，集与集之间用 # 分隔，
// 每集格式为「集名$播放地址」。多个播放源都保留（首选源失效时可换备用源），
// 集号重复时在标题后追记播放源名以便区分。
func (d *Downloader) fetchLibreTVDetail(ctx context.Context, site libretvSite, vodID string) (Drama, []Chapter, error) {
	vodID = strings.TrimSpace(vodID)
	if vodID == "" {
		return Drama{}, nil, errors.New("片源 ID 无效")
	}
	if site.Type == 2 {
		return d.fetchLibreTVDetailXML(ctx, site, vodID)
	}
	response, err := d.libretvAPI(ctx, site, map[string]string{"ac": "detail", "ids": vodID})
	if err != nil {
		if errors.Is(err, errLibreTVNotJSON) {
			return d.fetchLibreTVDetailXML(ctx, site, vodID)
		}
		return Drama{}, nil, err
	}
	rows := response.rows()
	if len(rows) == 0 {
		return Drama{}, nil, errors.New("片源接口没有返回该剧的详情")
	}
	row := rows[0]
	source := libretvSourceID(site.Key)
	first := response.List[0]
	drama := libretvDramaFromRow(source, row, libretvHTMLText(first.VodContent))
	referer := site.API
	if parsed, err := url.Parse(site.API); err == nil {
		referer = parsed.Scheme + "://" + parsed.Host + "/"
	}
	playFrom := strings.Split(first.VodPlayFrom, "$$$")
	playGroups := strings.Split(first.VodPlayURL, "$$$")
	return drama, libretvChaptersFromGroups(source, vodID, referer, playFrom, playGroups), nil
}

// libretvChaptersFromGroups 把「播放源分组」解析成分集列表，JSON 与 XML 接口共用。
// JSON 接口用 $$$ 分隔多个播放源、# 分隔分集、$ 分隔集名与播放地址；
// XML 接口的每个 <dd flag="源名"> 对应一个播放源，内部同样是 # 与 $ 分隔。
// 多个播放源都保留（首选源失效时可换备用源），集号重复时在标题后追记播放源名。
func libretvChaptersFromGroups(source, vodID, referer string, playFrom, playGroups []string) []Chapter {
	var chapters []Chapter
	seenURL := map[string]bool{}
	seenEpisode := map[int]string{}
	for playIndex, group := range playGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		playName := ""
		if playIndex < len(playFrom) {
			playName = strings.TrimSpace(playFrom[playIndex])
		}
		episodeIndexInGroup := 0
		for _, entry := range strings.Split(group, "#") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			name, address, found := strings.Cut(entry, "$")
			if !found {
				continue
			}
			address = strings.TrimSpace(address)
			if !isProviderHTTPMediaURL(address) || seenURL[address] {
				continue
			}
			episodeIndexInGroup++
			seenURL[address] = true
			title := strings.TrimSpace(name)
			if title == "" {
				title = fmt.Sprintf("第%d集", episodeIndexInGroup)
			}
			number := episodeIndex(title, episodeIndexInGroup)
			if previous, duplicated := seenEpisode[number]; duplicated && playName != "" && previous != playName {
				// 同一集号来自不同播放源：追记源名，避免列表里出现完全相同的标题。
				title = fmt.Sprintf("%s·%s", title, playName)
			}
			seenEpisode[number] = playName
			chapters = append(chapters, Chapter{
				ID:             providerChapterID(source, vodID, fmt.Sprintf("%d-%d", playIndex, episodeIndexInGroup)),
				Source:         source,
				Title:          title,
				VideoURL:       address,
				CurrentEpisode: rawEpisode(number),
				Referer:        referer,
			})
		}
	}
	if len(chapters) == 0 {
		return nil
	}
	sortProviderChapters(chapters)
	return uniqueChapters(chapters)
}
