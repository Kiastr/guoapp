package core

// Kazumi 规则源的持久化与导入（+155）。
//
// 形态对齐 drpyStore / ruleStore：
//   - 索引 kazumi.json：{key, name, version, host, file, sourceUrl, updatedAt}；
//   - 正文 kazumi/<key>.json：规则原样快照（远程导入时先下载再保存，
//     官方库下线不影响已导入的源）；
//   - 源 ID 为 kazumi:<key>，与 libretv / drpy 同层概念；
//   - 规则正文解析失败的条目只跳过自己，不让整张表加载失败。
//
// 出厂内置两条官方 api 模式规则（xfdmnext / giriGiriLove），只在索引文件
// 尚不存在时写入——用户删掉就是删掉了，重启不会复活。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// kazumiStoreFile 规则索引文件名（相对引擎目录）。
	kazumiStoreFile = "kazumi.json"
	// kazumiRulesDir 规则正文目录（相对引擎目录）。
	kazumiRulesDir = "kazumi"
	// kazumiRuleMaxBytes 单条规则上限（官方规则普遍 1-2KB，留足余量）。
	kazumiRuleMaxBytes = 256 << 10
	// kazumiStoreMaxBytes 索引上限。
	kazumiStoreMaxBytes = 512 << 10
)

// kazumiSite 一个已登记的 Kazumi 规则源。
type kazumiSite struct {
	// Key 站点 key（规则名，通常与官方库文件名一致）。
	Key string
	// Name 展示名（规则里的 name）。
	Name string
	// Version 规则版本。
	Version string
	// Host 数据来源域名（取搜索接口的域名，站源管理里展示）。
	Host string
	// File kazumi/<key>.json（相对引擎目录）。
	File string
	// SourceURL 远程规则地址（粘贴导入时为空；按它重拉可升级）。
	SourceURL string
	// UpdatedAt 最近一次入库时间（RFC3339）。
	UpdatedAt string
	// Rule 解析后的规则（进程内缓存，不落盘）。
	Rule kazumiRule
	// Raw 规则原文（落盘快照，避免反复marshal）。
	Raw map[string]any
}

// kazumiStore 规则源订阅表（进程内 + 索引文件 + 正文目录）。
type kazumiStore struct {
	directory string
	mu        chan struct{}
	sites     map[string]kazumiSite
}

func newKazumiStore(directory string) *kazumiStore {
	return &kazumiStore{
		directory: directory,
		mu:        make(chan struct{}, 1),
		sites:     map[string]kazumiSite{},
	}
}

func (store *kazumiStore) lock()   { store.mu <- struct{}{} }
func (store *kazumiStore) unlock() { <-store.mu }

// kazumiRuleFile 规则正文相对路径。
func kazumiRuleFile(key string) string {
	return kazumiRulesDir + "/" + key + ".json"
}

// loadKazumiStore 从引擎目录读取索引与正文；索引缺失时写入出厂内置规则。
func loadKazumiStore(directory string) *kazumiStore {
	store := newKazumiStore(directory)
	body, err := os.ReadFile(filepath.Join(directory, kazumiStoreFile))
	if err != nil || len(body) > kazumiStoreMaxBytes {
		// 首次使用（或索引损坏）：给两条出厂源，其余交给用户导入。
		kazumiSeedDefaults(store)
		return store
	}
	var payload struct {
		Sites []kazumiSiteSnapshot `json:"sites"`
	}
	if json.Unmarshal(body, &payload) != nil {
		kazumiSeedDefaults(store)
		return store
	}
	for _, snapshot := range payload.Sites {
		key := strings.TrimSpace(snapshot.Key)
		if key == "" {
			continue
		}
		raw, ok := readKazumiRuleBody(filepath.Join(directory, filepath.FromSlash(
			firstNonEmpty(strings.TrimSpace(snapshot.File), kazumiRuleFile(key)))))
		if !ok {
			continue
		}
		rule, err := kazumiRuleFromMap(raw)
		if err != nil {
			// 规则升级改坏了 / 官方删库导致拉取到的是错误页：跳过而不是
			// 让整张表加载失败（其它源还能用）。
			continue
		}
		store.sites[key] = kazumiSite{
			Key:       key,
			Name:      firstNonEmpty(strings.TrimSpace(snapshot.Name), rule.Name),
			Version:   firstNonEmpty(strings.TrimSpace(snapshot.Version), rule.Version),
			Host:      strings.TrimSpace(snapshot.Host),
			File:      firstNonEmpty(strings.TrimSpace(snapshot.File), kazumiRuleFile(key)),
			SourceURL: strings.TrimSpace(snapshot.SourceURL),
			UpdatedAt: strings.TrimSpace(snapshot.UpdatedAt),
			Rule:      rule,
			Raw:       raw,
		}
	}
	return store
}

// kazumiSiteSnapshot 索引文件的落盘结构（与内存结构解耦：正文不入库）。
type kazumiSiteSnapshot struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Host      string `json:"host,omitempty"`
	File      string `json:"file"`
	SourceURL string `json:"sourceUrl,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// readKazumiRuleBody 读取并解析规则正文。
func readKazumiRuleBody(path string) (map[string]any, bool) {
	body, err := os.ReadFile(path)
	if err != nil || len(body) == 0 || len(body) > kazumiRuleMaxBytes {
		return nil, false
	}
	var rule map[string]any
	if json.Unmarshal(body, &rule) != nil || len(rule) == 0 {
		return nil, false
	}
	return rule, true
}

// saveLocked 写索引（调用方需持有 mu）。
func (store *kazumiStore) saveLocked() error {
	keys := make([]string, 0, len(store.sites))
	for key := range store.sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	payload := struct {
		Sites []kazumiSiteSnapshot `json:"sites"`
	}{Sites: make([]kazumiSiteSnapshot, 0, len(keys))}
	for _, key := range keys {
		site := store.sites[key]
		payload.Sites = append(payload.Sites, kazumiSiteSnapshot{
			Key:       site.Key,
			Name:      site.Name,
			Version:   site.Version,
			Host:      site.Host,
			File:      site.File,
			SourceURL: site.SourceURL,
			UpdatedAt: site.UpdatedAt,
		})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeNativeCacheFile(filepath.Join(store.directory, kazumiStoreFile), body)
}

// get 按完整源 ID（kazumi:<key>）取站点。
func (store *kazumiStore) get(source string) (kazumiSite, bool) {
	key := kazumiKeyFromSource(source)
	if key == "" {
		return kazumiSite{}, false
	}
	store.lock()
	defer store.unlock()
	site, ok := store.sites[key]
	return site, ok
}

// put 登记站点（写正文 + 索引）；返回是否新增。
func (store *kazumiStore) put(site kazumiSite) (bool, error) {
	store.lock()
	defer store.unlock()
	_, existed := store.sites[site.Key]
	if err := os.MkdirAll(filepath.Join(store.directory, kazumiRulesDir), 0o755); err != nil {
		return false, fmt.Errorf("规则目录创建失败：%w", publicError(err))
	}
	encoded, err := json.Marshal(site.Raw)
	if err != nil {
		return false, errors.New("规则内容无法序列化")
	}
	if len(encoded) > kazumiRuleMaxBytes {
		return false, fmt.Errorf("规则过大（上限 %d KB）", kazumiRuleMaxBytes>>10)
	}
	if err := writeNativeCacheFile(filepath.Join(store.directory, filepath.FromSlash(site.File)), encoded); err != nil {
		return false, fmt.Errorf("规则保存失败：%w", publicError(err))
	}
	store.sites[site.Key] = site
	return !existed, store.saveLocked()
}

// remove 删除站点与规则正文。
func (store *kazumiStore) remove(key string) error {
	store.lock()
	defer store.unlock()
	site, ok := store.sites[key]
	if !ok {
		return nil
	}
	delete(store.sites, key)
	if err := store.saveLocked(); err != nil {
		return err
	}
	if site.File != "" {
		_ = os.Remove(filepath.Join(store.directory, filepath.FromSlash(site.File)))
	}
	return nil
}

// list 全部站点（按 key 排序，输出稳定）。
func (store *kazumiStore) list() []kazumiSite {
	store.lock()
	defer store.unlock()
	keys := make([]string, 0, len(store.sites))
	for key := range store.sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]kazumiSite, 0, len(keys))
	for _, key := range keys {
		out = append(out, store.sites[key])
	}
	return out
}

// kazumiSeedDefaults 写入出厂内置规则（只在索引缺失时调用）。
//
// 这两条是官方库里目前可直接使用的 api 模式规则（其余要么已废弃、
// 要么章节取值依赖 XPath）。规则正文原样保存，保留 sourceUrl 便于后续升级。
func kazumiSeedDefaults(store *kazumiStore) {
	for _, seed := range []struct{ name, url, raw string }{
		{name: "xfdmnext", url: kazumiRuleURL("xfdmnext"), raw: kazumiSeedXfdmnext},
		{name: "giriGiriLove", url: kazumiRuleURL("giriGiriLove"), raw: kazumiSeedGiriGiriLove},
	} {
		var raw map[string]any
		if json.Unmarshal([]byte(seed.raw), &raw) != nil {
			continue
		}
		rule, err := kazumiRuleFromMap(raw)
		if err != nil {
			continue
		}
		site := kazumiSite{
			Key:       kazumiKeyForRule(rule.Name),
			Name:      firstNonEmpty(rule.Name, seed.name),
			Version:   rule.Version,
			Host:      kazumiHostFromRule(rule),
			File:      kazumiRuleFile(kazumiKeyForRule(rule.Name)),
			SourceURL: seed.url,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			Rule:      rule,
			Raw:       raw,
		}
		if _, err := store.put(site); err != nil {
			continue
		}
	}
}

// kazumiRuleURL 官方库里某条规则的地址（生态保护：升级时用）。
func kazumiRuleURL(name string) string {
	return strings.ReplaceAll(kazumiOfficialRuleURLTemplate, "@name", strings.TrimSpace(name))
}

// kazumiKeyForRule 规则名 → 站点 key。
//
// key 必须匹配 libretvKeyPattern（与 LibreTV / drpy 同一套约束：源 ID 里
// 不能有冒号，也不能太长），不匹配时按字符集收敛再截断。
func kazumiKeyForRule(name string) string {
	key := strings.TrimSpace(name)
	if libretvKeyPattern.MatchString(key) {
		return key
	}
	builder := strings.Builder{}
	for _, char := range key {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9',
			char == '_', char == '-':
			builder.WriteRune(char)
		default:
			builder.WriteRune('_')
		}
	}
	sanitized := builder.String()
	if len(sanitized) > 32 {
		sanitized = sanitized[:32]
	}
	if sanitized == "" {
		sanitized = "kazumi"
	}
	return sanitized
}

// kazumiHostFromRule 规则主域名（取搜索接口，其次 baseURL）。
func kazumiHostFromRule(rule kazumiRule) string {
	for _, candidate := range []string{rule.Search.Request.URL, rule.BaseURL} {
		parsed, err := url.Parse(strings.TrimSpace(candidate))
		if err != nil {
			continue
		}
		if host := parsed.Hostname(); host != "" {
			return host
		}
	}
	return ""
}

// importKazumiRule 导入一条规则：支持粘贴 JSON 正文或填写规则地址。
func (engine *nativeEngine) importKazumiRule(ctx context.Context, raw, sourceURL string) (any, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, errors.New("请粘贴 Kazumi 规则内容或填写规则地址")
	}
	if len(text) > kazumiRuleMaxBytes {
		return nil, fmt.Errorf("内容过大（上限 %d KB），请确认粘贴的是 Kazumi 规则", kazumiRuleMaxBytes>>10)
	}
	origin := ""
	if isProviderHTTPMediaURL(text) && !strings.ContainsAny(text, "\n\r") {
		fetched, err := engine.downloader.fetchProviderText(ctx, text, text)
		if err != nil {
			return nil, fmt.Errorf("规则获取失败：%w", publicError(err))
		}
		origin, text = text, fetched
	} else if trimmed := strings.TrimSpace(sourceURL); trimmed != "" {
		if !isProviderHTTPMediaURL(trimmed) {
			return nil, errors.New("规则地址无效，请填写 http(s) 地址或不填")
		}
		origin = trimmed
	}
	var rule map[string]any
	if json.Unmarshal([]byte(text), &rule) != nil || len(rule) == 0 {
		return nil, errors.New("规则不是有效 JSON，请确认粘贴的是 Kazumi 规则全文")
	}
	parsed, err := kazumiRuleFromMap(rule)
	if err != nil {
		return nil, err
	}
	key := kazumiKeyForRule(parsed.Name)
	previous, existed := engine.downloader.kazumi.get(kazumiSourceID(key))
	site := kazumiSite{
		Key:       key,
		Name:      parsed.Name,
		Version:   parsed.Version,
		Host:      kazumiHostFromRule(parsed),
		File:      kazumiRuleFile(key),
		SourceURL: firstNonEmpty(origin, previous.SourceURL),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Rule:      parsed,
		Raw:       rule,
	}
	if _, err := engine.downloader.kazumi.put(site); err != nil {
		return nil, err
	}
	return map[string]any{
		"key":       site.Key,
		"name":      site.Name,
		"version":   site.Version,
		"host":      site.Host,
		"file":      site.File,
		"sourceUrl": site.SourceURL,
		"webview":   parsed.UseWebview,
		"added":     !existed,
		"total":     1,
	}, nil
}

// removeKazumiSite 删除已导入规则，并清掉它的目录缓存与任务记录
// （与 removeDrpySite 同一套自清理，避免残留幽灵缓存）。
func (engine *nativeEngine) removeKazumiSite(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("规则 key 无效")
	}
	store := engine.downloader.kazumi
	if _, ok := store.get(kazumiSourceID(key)); !ok {
		return errors.New("该规则源不存在或已删除")
	}
	engine.mu.Lock()
	source := kazumiSourceID(key)
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
	return saveErr
}

// kazumiSiteList 供 kazumiSites 操作返回：规则源列表。
func (engine *nativeEngine) kazumiSiteList() []map[string]any {
	store := engine.downloader.kazumi
	out := []map[string]any{}
	for _, site := range store.list() {
		out = append(out, map[string]any{
			"key":       site.Key,
			"name":      site.Name,
			"version":   site.Version,
			"host":      site.Host,
			"file":      site.File,
			"sourceUrl": site.SourceURL,
			// webview=true 表示分集要经 WebView 嗅探才能拿到真实地址，
			// UI 据此提示「下载不可用」。
			"webview": site.Rule.UseWebview,
		})
	}
	return out
}

// kazumiOfficialRules 拉取 Kazumi 官方规则库索引，供「从官方库导入」使用。
//
// 官方库 86 条里多数是非 api 模式或已废弃，这里把 searchMode/chapterMode
// 一并返回，客户端据此标记「可用 / 需要 XPath」，避免用户导入了才发现不支持。
func (engine *nativeEngine) kazumiOfficialRules(ctx context.Context) (any, error) {
	status, body, err := ruleGetWithHeaders(ctx, engine.downloader, kazumiOfficialIndexURL,
		map[string]string{"User-Agent": userAgent})
	if err != nil {
		return nil, fmt.Errorf("官方规则库获取失败：%w", publicError(err))
	}
	if status < 200 || status >= 300 {
		return nil, errors.New("官方规则库获取失败，请稍后重试")
	}
	var entries []struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		Deprecated bool   `json:"deprecated"`
	}
	if json.Unmarshal([]byte(body), &entries) != nil {
		return nil, errors.New("官方规则库格式无效")
	}
	out := []map[string]any{}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":       name,
			"version":    strings.TrimSpace(entry.Version),
			"url":        kazumiRuleURL(name),
			"deprecated": entry.Deprecated || strings.EqualFold(strings.TrimSpace(entry.Version), "deprecated"),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("官方规则库为空，请稍后重试")
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i]["deprecated"] == true, out[j]["deprecated"] == true
		if left != right {
			return right
		}
		return strings.ToLower(kazumiText(out[i]["name"])) < strings.ToLower(kazumiText(out[j]["name"]))
	})
	return map[string]any{"rules": out}, nil
}

// ---------------------------------------------------------------------------
// 出厂内置规则（原始快照，取自 Kazumi 官方规则库 Predidit/KazumiRules）。
//
// 官方库 86 条规则目前只有 4 条是完整 api 模式，其中 milimili 已标记废弃、
// sorani 与我们内置的同名站源重复，因此这里只内置 xfdmnext 与 giriGiriLove：
//   - xfdmnext：1080p 优先，分集给的是播放页（需 WebView 嗅探）；
//   - giriGiriLove：直链 mp4，纯 Go 即可播放 / 下载。
//
// 规则里的 apikey / Authorization 是上游站点的公开匿名凭证（官方仓库明文
// 提供，Kazumi App 也是照抄整份规则），不属于本项目凭据；此处原样保存，
// 便于后续按 sourceUrl 升级。
const kazumiSeedXfdmnext = `{
  "api": "8",
  "type": "anime",
  "name": "xfdmnext",
  "version": "1.1",
  "muliSources": false,
  "useWebview": true,
  "useNativePlayer": true,
  "usePost": false,
  "useLegacyParser": false,
  "adBlocker": false,
  "userAgent": "",
  "baseURL": "https://next.xifanacg.com/",
  "searchURL": "",
  "searchList": "",
  "searchName": "",
  "searchResult": "",
  "chapterRoads": "",
  "chapterResult": "",
  "searchMode": "api",
  "chapterMode": "api",
  "searchApiConfig": {
    "request": {
      "method": "POST",
      "url": "https://api.xifanacg.com/rest/v1/rpc/search_animes",
      "headers": {
        "apikey": "sb_publishable_OBIVAWACIX6lPXrO98_z24_HcsmalkA",
        "Authorization": "Bearer sb_publishable_OBIVAWACIX6lPXrO98_z24_HcsmalkA"
      },
      "bodyType": "json",
      "body": {
        "search_term": "@keyword",
        "page_number": 1,
        "items_per_page": 24
      }
    },
    "listPath": "$[*]",
    "namePath": "$.title",
    "sourcePath": "$.id"
  },
  "chapterApiConfig": {
    "request": {
      "method": "POST",
      "url": "https://api.xifanacg.com/rest/v1/rpc/get_anime_detail",
      "headers": {
        "apikey": "sb_publishable_OBIVAWACIX6lPXrO98_z24_HcsmalkA",
        "Authorization": "Bearer sb_publishable_OBIVAWACIX6lPXrO98_z24_HcsmalkA"
      },
      "bodyType": "json",
      "body": {
        "p_id": "@source"
      }
    },
    "format": "nested",
    "roadsPath": "$.sources[*]",
    "roadNamePath": "$.name",
    "episodesPath": "$.episodes[*]",
    "episodeNamePath": "$.title",
    "episodeUrlPath": "$.id",
    "episodePage": {
      "url": "https://next.xifanacg.com/anime/@source/play/@episodeUrl",
      "query": {}
    }
  },
  "antiCrawlerConfig": {
    "enabled": false,
    "captchaType": 1,
    "captchaImage": "",
    "captchaInput": "",
    "captchaButton": "",
    "captchaDetectType": 1,
    "captchaDetectValue": "",
    "captchaScript": ""
  }
}`

const kazumiSeedGiriGiriLove = `{
  "api": "9",
  "type": "anime",
  "name": "giriGiriLove",
  "version": "2.6",
  "muliSources": true,
  "useWebview": true,
  "useNativePlayer": true,
  "usePost": false,
  "useLegacyParser": false,
  "adBlocker": false,
  "userAgent": "",
  "baseURL": "https://ani.girigirilove.com",
  "searchMode": "api",
  "chapterMode": "api",
  "searchApiConfig": {
    "request": {
      "method": "GET",
      "url": "https://m3u8.girigirilove.com/api.php/Vod/get_list",
      "query": {
        "vod_name": "@keyword",
        "offset": "0",
        "limit": "24"
      }
    },
    "listPath": "$.info.rows[*]",
    "namePath": "$.vod_name",
    "sourcePath": "$.vod_id",
    "sourceTemplate": ""
  },
  "chapterApiConfig": {
    "request": {
      "method": "GET",
      "url": "https://m3u8.girigirilove.com/api.php/Vod/get_detail",
      "query": {
        "vod_id": "@source"
      }
    },
    "format": "delimited",
    "roadNamesPath": "$.info.vod_play_from",
    "roadEpisodesPath": "$.info.vod_play_url",
    "roadSeparator": "$$$",
    "episodeSeparator": "#",
    "fieldSeparator": "$"
  },
  "searchURL": "",
  "searchList": "",
  "searchName": "",
  "searchResult": "",
  "chapterRoads": "",
  "chapterResult": "",
  "referer": ""
}`
