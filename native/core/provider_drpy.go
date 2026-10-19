package core

// drpy 脚本片源（TVBox drpy2 / spider 规则的 goja 运行时实现）。
//
// 背景：LibreTV / TVBox 订阅里 type=3 的站点是「drpy/spider 脚本源」——
// 接口逻辑不是固定的 MacCMS provide/vod，而是一段 JS（var rule = {...} +
// init/home/category/detail/play/search 函数），需要 JS 引擎执行。
// 本文件把这类脚本源归一成运行时站源：源 ID 为 "drpy:<key>"，
// 剧集 ID 为 "drpy:<key>:<base64(vodId)>"，
// 分集 ID 为 "drpy:<key>:<base64(vodId)>:<base64(flag \x00 episodeKey)>"
// （RawURL 字符集无冒号，两段 strings.Cut 天然兼容）。
//
// 脚本正文落盘在引擎目录 scripts/<key>.js，订阅表在 drpy.json；
// 编译结果（goja.Program）按 key 缓存，删除时失效。

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const (
	// sourceDrpy drpy 脚本源家族名（完整源 ID 为 sourceDrpy:<key>）。
	sourceDrpy = "drpy"
	// sourceDrpyPrefix 运行时 drpy 脚本源的 ID 前缀。
	sourceDrpyPrefix = sourceDrpy + ":"
	// drpyScriptMaxBytes 脚本体上限（1MB）：drpy 脚本普遍几十 KB。
	drpyScriptMaxBytes = 1 << 20
	// drpyRespMaxBytes req 响应体上限（4MB）：列表 JSON/播放页 HTML。
	drpyRespMaxBytes = 4 << 20
	// drpyImportMaxSites 单次导入的脚本源上限。
	drpyImportMaxSites = 50
	// drpyCallTimeout 单次脚本调用看门狗（含 req 同步等待）。
	drpyCallTimeout = 30 * time.Second
	// drpyStoreFile 订阅表文件名。
	drpyStoreFile = "drpy.json"
	// drpyScriptsDir 脚本正文目录（相对引擎目录）。
	drpyScriptsDir = "scripts"
)

// drpySite 一个已导入的 drpy 脚本源。
type drpySite struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// ScriptURL 导入来源（粘贴 JS 时为空）。
	ScriptURL string `json:"scriptUrl,omitempty"`
	// File scripts/<key>.js（相对引擎目录）。
	File string `json:"file"`
	// Title rule.title（导入时静态提取，可能为空）。
	Title string `json:"title,omitempty"`
	// Host rule.host（静态提取，UI 展示用）。
	Host string `json:"host,omitempty"`
	// Count 探测缓存的剧集数（展示用）。
	Count int `json:"count,omitempty"`
}

// drpyStore 脚本源订阅表（进程内 + drpy.json 落盘 + Program 编译缓存）。
type drpyStore struct {
	directory string
	mu        chan struct{}
	sites     map[string]drpySite
	programs  map[string]*goja.Program
}

func newDrpyStore(directory string) *drpyStore {
	return &drpyStore{
		directory: directory,
		mu:        make(chan struct{}, 1),
		sites:     map[string]drpySite{},
		programs:  map[string]*goja.Program{},
	}
}

// loadDrpyStore 从引擎目录读取已保存的脚本源；文件缺失或损坏时返回空表。
// 脚本文件缺失的条目会被跳过（防手工删文件后留下死条目）。
func loadDrpyStore(directory string) *drpyStore {
	store := newDrpyStore(directory)
	body, err := os.ReadFile(filepath.Join(directory, drpyStoreFile))
	if err != nil || len(body) > libretvSubscriptionMaxBytes {
		return store
	}
	var payload struct {
		Sites []drpySite `json:"sites"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return store
	}
	for _, site := range payload.Sites {
		key, name, file, ok := normalizeDrpySite(site.Key, site.Name, site.File)
		if !ok {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(directory, filepath.FromSlash(file))); statErr != nil {
			continue
		}
		store.sites[key] = drpySite{Key: key, Name: name, ScriptURL: site.ScriptURL, File: file, Title: site.Title, Host: site.Host}
	}
	return store
}

// saveLocked 写盘（调用方需持有 mu）。
func (store *drpyStore) saveLocked() error {
	keys := make([]string, 0, len(store.sites))
	for key := range store.sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	payload := struct {
		Sites []drpySite `json:"sites"`
	}{Sites: make([]drpySite, 0, len(keys))}
	for _, key := range keys {
		payload.Sites = append(payload.Sites, store.sites[key])
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeNativeCacheFile(filepath.Join(store.directory, drpyStoreFile), body)
}

func (store *drpyStore) lock()   { store.mu <- struct{}{} }
func (store *drpyStore) unlock() { <-store.mu }

// get 按完整源 ID（drpy:<key>）取站点。
func (store *drpyStore) get(source string) (drpySite, bool) {
	key := drpyKeyFromSource(source)
	if key == "" {
		return drpySite{}, false
	}
	store.lock()
	defer store.unlock()
	site, ok := store.sites[key]
	return site, ok
}

// put 登记站点并落盘；返回是否新增（false 表示已存在同 key 站点被覆盖）。
func (store *drpyStore) put(site drpySite) (bool, error) {
	store.lock()
	defer store.unlock()
	_, existed := store.sites[site.Key]
	store.sites[site.Key] = site
	return !existed, store.saveLocked()
}

// remove 删除站点、脚本文件与编译缓存。
func (store *drpyStore) remove(key string) error {
	store.lock()
	defer store.unlock()
	site, ok := store.sites[key]
	if !ok {
		return nil
	}
	delete(store.sites, key)
	delete(store.programs, key)
	if err := store.saveLocked(); err != nil {
		return err
	}
	if site.File != "" {
		_ = os.Remove(filepath.Join(store.directory, filepath.FromSlash(site.File)))
	}
	return nil
}

// list 返回全部站点（按 key 排序，稳定输出）。
func (store *drpyStore) list() []drpySite {
	store.lock()
	defer store.unlock()
	keys := make([]string, 0, len(store.sites))
	for key := range store.sites {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]drpySite, 0, len(keys))
	for _, key := range keys {
		out = append(out, store.sites[key])
	}
	return out
}

// scriptPath 脚本正文的磁盘路径。
func (store *drpyStore) scriptPath(file string) string {
	return filepath.Join(store.directory, filepath.FromSlash(strings.TrimSpace(file)))
}

// readScript 读取脚本正文（上限 drpyScriptMaxBytes）。
func (store *drpyStore) readScript(file string) (string, error) {
	body, err := os.ReadFile(store.scriptPath(file))
	if err != nil {
		return "", errors.New("脚本文件读取失败，请重新导入")
	}
	if len(body) > drpyScriptMaxBytes {
		return "", errors.New("脚本文件过大")
	}
	return string(body), nil
}

// programFor 取（或编译并缓存）脚本的 goja.Program。编译失败不缓存，
// 下次重试，便于用户修正脚本文件后即时恢复。
func (store *drpyStore) programFor(site drpySite) (*goja.Program, error) {
	store.lock()
	defer store.unlock()
	if program, ok := store.programs[site.Key]; ok {
		return program, nil
	}
	script, err := store.readScript(site.File)
	if err != nil {
		return nil, err
	}
	program, err := goja.Compile(site.Key+".js", script, false)
	if err != nil {
		return nil, fmt.Errorf("脚本语法错误：%s", drpyCompileError(err))
	}
	store.programs[site.Key] = program
	return program, nil
}

// invalidateProgram 脚本内容变化后丢弃编译缓存。
func (store *drpyStore) invalidateProgram(key string) {
	store.lock()
	defer store.unlock()
	delete(store.programs, key)
}

// drpyCompileError 提取 goja 编译错误的可读信息（去掉文件名与行列噪音）。
func drpyCompileError(err error) string {
	text := err.Error()
	if idx := strings.Index(text, ": "); idx >= 0 && strings.HasPrefix(text, "drpy") {
		text = text[idx+2:]
	}
	if len([]rune(text)) > 160 {
		text = string([]rune(text)[:160]) + "…"
	}
	return strings.TrimSpace(text)
}

// drpySite 按完整源 ID（drpy:<key>）取站点；Downloader 便捷方法。
func (d *Downloader) drpySite(source string) (drpySite, bool) {
	if d.drpy == nil {
		return drpySite{}, false
	}
	return d.drpy.get(source)
}

// drpyKeyFromSource 从完整源 ID 取出站点 key；不是 drpy 源时返回空串。
func drpyKeyFromSource(source string) string {
	trimmed := strings.TrimSpace(source)
	if !strings.HasPrefix(trimmed, sourceDrpyPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, sourceDrpyPrefix))
}

// drpySourceID 站点 key → 完整源 ID。
func drpySourceID(key string) string {
	return sourceDrpyPrefix + strings.TrimSpace(key)
}

// normalizeDrpySite 校验并归一化站点字段：key 需匹配 libretvKeyPattern，
// name 缺省时回落「drpy 脚本源」，file 统一 scripts/<key>.js。
func normalizeDrpySite(rawKey, rawName, rawFile string) (key, name, file string, ok bool) {
	key = strings.TrimSpace(rawKey)
	if !libretvKeyPattern.MatchString(key) {
		return "", "", "", false
	}
	name = strings.TrimSpace(rawName)
	if name == "" {
		name = "drpy 脚本源"
	}
	if len([]rune(name)) > 48 {
		name = string([]rune(name)[:48])
	}
	file = strings.TrimSpace(rawFile)
	if file == "" {
		file = drpyScriptsDir + "/" + key + ".js"
	}
	if !strings.HasPrefix(file, drpyScriptsDir+"/") || strings.Contains(file, "..") {
		return "", "", "", false
	}
	return key, name, file, true
}

// drpy 脚本静态提取：title / host / key。drpy2 标准形态是
// `var rule = { title:'...', host:'...', ... }`，部分脚本写成 let/const 或无引号键。
var (
	drpyRulePattern  = regexp.MustCompile(`(?is)\b(?:var|let|const)\s+rule\b`)
	drpyTitlePattern = regexp.MustCompile(`(?is)\btitle\s*:\s*['"]([^'"]{1,80})['"]`)
	drpyHostPattern  = regexp.MustCompile(`(?is)\bhost\s*:\s*['"]([^'"]{1,120})['"]`)
	drpyKeyPattern   = regexp.MustCompile(`(?is)\bkey\s*:\s*['"]([^'"]{1,32})['"]`)
)

// drpySlug 从展示名派生 URL 安全片段。
func drpySlug(name string) string {
	slug := regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(name), "")
	if slug == "" {
		slug = "rule"
	}
	if len(slug) > 16 {
		slug = slug[:16]
	}
	return slug
}

// drpyKeyFromScript 由脚本内容派生稳定 key：title/host slug + 内容哈希前 6 位。
func drpyKeyFromScript(script string) string {
	seed := script
	if match := drpyTitlePattern.FindStringSubmatch(script); match != nil {
		seed = match[1]
	} else if match := drpyHostPattern.FindStringSubmatch(script); match != nil {
		seed = match[1]
	}
	sum := sha256.Sum256([]byte(script))
	return drpySlug(seed) + "-" + hex.EncodeToString(sum[:])[:6]
}

// drpyScriptMeta 静态提取的脚本元信息。
type drpyScriptMeta struct {
	Title string
	Host  string
}

func drpyExtractMeta(script string) drpyScriptMeta {
	meta := drpyScriptMeta{}
	if match := drpyTitlePattern.FindStringSubmatch(script); match != nil {
		meta.Title = strings.TrimSpace(match[1])
	}
	if match := drpyHostPattern.FindStringSubmatch(script); match != nil {
		meta.Host = strings.TrimSpace(match[1])
	}
	return meta
}

// looksLikeDrpyScript 判断输入是否像 drpy 脚本（JS 文本）。
func looksLikeDrpyScript(raw string) bool {
	if !drpyRulePattern.MatchString(raw) {
		return false
	}
	return strings.Contains(raw, "function") || strings.Contains(raw, "=>") ||
		strings.Contains(raw, "async") || strings.Contains(raw, "home")
}

// validateDrpyScript 语法校验 + 形态检查，返回可读错误。
func validateDrpyScript(script string) error {
	if strings.TrimSpace(script) == "" {
		return errors.New("脚本内容为空")
	}
	if len(script) > drpyScriptMaxBytes {
		return fmt.Errorf("脚本过大（上限 %d KB）", drpyScriptMaxBytes>>10)
	}
	if !drpyRulePattern.MatchString(script) {
		return errors.New("未找到 drpy 脚本（需要包含 var rule 定义的 JS）")
	}
	if _, err := goja.Compile("drpy.js", script, false); err != nil {
		return fmt.Errorf("脚本语法错误：%s", drpyCompileError(err))
	}
	return nil
}

// importDrpyScript 导入 drpy 脚本源：输入可以是 JS 文本（粘贴）或脚本 URL
// （http/https 的 .js 地址，拉取后同一套校验）。已存在的同 key 源被覆盖。
func (engine *nativeEngine) importDrpyScript(ctx context.Context, raw string) (any, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, errors.New("请输入 drpy 脚本内容或脚本地址")
	}
	if len(text) > drpyScriptMaxBytes {
		return nil, fmt.Errorf("内容过大（上限 %d KB），请确认粘贴的是 drpy 脚本", drpyScriptMaxBytes>>10)
	}
	scriptURL := ""
	script := text
	if isProviderHTTPMediaURL(text) && !strings.ContainsAny(text, "\n\r") {
		fetched, err := engine.downloader.fetchProviderText(ctx, text, text)
		if err != nil {
			return nil, fmt.Errorf("脚本获取失败：%w", publicError(err))
		}
		script, scriptURL = fetched, text
	}
	if err := validateDrpyScript(script); err != nil {
		return nil, err
	}
	meta := drpyExtractMeta(script)
	key := drpyKeyFromScript(script)
	if match := drpyKeyPattern.FindStringSubmatch(script); match != nil && libretvKeyPattern.MatchString(match[1]) {
		key = match[1]
	}
	name := meta.Title
	if name == "" {
		name = "drpy 脚本源"
	}
	site := drpySite{Key: key, Name: name, ScriptURL: scriptURL, File: drpyScriptsDir + "/" + key + ".js", Title: meta.Title, Host: meta.Host}
	if err := os.MkdirAll(filepath.Join(engine.directory, drpyScriptsDir), 0o755); err != nil {
		return nil, fmt.Errorf("脚本目录创建失败：%w", publicError(err))
	}
	if err := os.WriteFile(filepath.Join(engine.directory, filepath.FromSlash(site.File)), []byte(script), 0o600); err != nil {
		return nil, fmt.Errorf("脚本保存失败：%w", publicError(err))
	}
	added, err := engine.downloader.drpy.put(site)
	if err != nil {
		return nil, fmt.Errorf("脚本源保存失败：%w", publicError(err))
	}
	engine.downloader.drpy.invalidateProgram(key)
	return map[string]any{
		"key":    site.Key,
		"name":   site.Name,
		"title":  site.Title,
		"host":   site.Host,
		"file":   site.File,
		"added":  added,
		"total":  1,
	}, nil
}

// removeDrpySite 删除已导入脚本源，并清掉它的目录缓存与任务记录。
func (engine *nativeEngine) removeDrpySite(key string) error {
	key = strings.TrimSpace(key)
	if key == "" || !libretvKeyPattern.MatchString(key) {
		return errors.New("脚本源 key 无效")
	}
	store := engine.downloader.drpy
	if _, ok := store.get(drpySourceID(key)); !ok {
		return errors.New("脚本源不存在或已删除")
	}
	engine.mu.Lock()
	source := drpySourceID(key)
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

// drpySiteList 供 drpySites 操作返回：脚本源 + 当前缓存剧集数。
func (engine *nativeEngine) drpySiteList() []map[string]any {
	store := engine.downloader.drpy
	engine.mu.Lock()
	counts := map[string]int{}
	for catalogKey, items := range engine.catalogs {
		key := drpyKeyFromSource(catalogKey)
		if key == "" {
			continue
		}
		counts[key] += len(items)
	}
	engine.mu.Unlock()
	out := make([]map[string]any, 0)
	for _, site := range store.list() {
		out = append(out, map[string]any{
			"key":       site.Key,
			"name":      site.Name,
			"scriptUrl": site.ScriptURL,
			"file":      site.File,
			"title":     site.Title,
			"host":      site.Host,
			"count":     counts[site.Key],
		})
	}
	return out
}

// base64RawURL 编解码工具（ID 编码用；RawURL 字符集无冒号，规避两段 Cut 冲突）。
func drpyEncodeSegment(text string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(text))
}

func drpyDecodeSegment(segment string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(segment))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// drpyBlobFromParts 分集 blob：flag + \x00 + episodeKey（\x00 不会出现在两者中）。
func drpyBlobFromParts(flag, episodeKey string) string {
	return drpyEncodeSegment(flag + "\x00" + episodeKey)
}

// drpyPartsFromBlob 解出 (flag, episodeKey)。
func drpyPartsFromBlob(blob string) (flag, episodeKey string, ok bool) {
	decoded, valid := drpyDecodeSegment(blob)
	if !valid {
		return "", "", false
	}
	flag, episodeKey, found := strings.Cut(decoded, "\x00")
	if !found {
		return "", "", false
	}
	return flag, episodeKey, true
}
