package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// [站点验证] 通用骨架
//
// Cloudflare 一类的挑战页无法在 HTTP 客户端里解开：站点要求真实浏览器
// 完成一次交互（勾选/等待），然后下发 cf_clearance 等 cookie。这里把
// 「浏览器里通过后拿到的凭据」按源存下来，由 HTTP 层在后续请求里复用：
//
//	source ──► {host, userAgent, cookie}
//
// 三条硬约束（改之前先想清楚）：
//
//  1. cookie 与 (host, 出口 IP, UA) 绑定。换域名、换代理节点、换 UA 都会
//     让它失效，UI 必须提示重新验证；
//  2. UA 必须与验证时浏览器用的完全一致，否则 cookie 形同虚设——所以注入
//     时验证记录里的 UA 优先于源自身的 UA；
//  3. cookie 属于凭据：只写本机 0600 文件，不进 resource-settings.json、
//     不进 WebDAV 同步、不进诊断日志。
//
// 新增一个可验证源时只做两件事：在 nativeSiteVerificationTargets 里加一行
// （源 id / 名称 / 验证入口 URL / 该源请求实际使用的 UA），然后在 Dart 侧
// 不需要任何改动——验证页与状态列表都由这张表驱动。

const (
	siteVerificationFileName = "site-verifications.json"
	// Cookie 上限按「整站 cookie 串」估：cf_clearance + __cf_bm + 站点自身
	// 登录态通常不到 2 KB，16 KiB 足够宽松，又能挡住误粘贴整段请求头。
	siteVerificationMaxCookie = 16 << 10
	siteVerificationMaxAgent  = 512
	siteVerificationMaxFile   = 256 << 10
)

// nativeSiteVerification 是一条站点验证凭据。Host 记录验证时的站点域名，
// 注入前会与实际请求的 host 比对，避免把 A 站的 cookie 发给 B 站。
type nativeSiteVerification struct {
	Source    string `json:"source"`
	Host      string `json:"host"`
	UserAgent string `json:"userAgent"`
	Cookie    string `json:"cookie"`
	UpdatedAt int64  `json:"updatedAt"`
}

// nativeSiteVerificationTarget 描述一个「支持站点验证」的源。UserAgent
// 必须与 Go 侧该源请求实际使用的 UA 逐字一致，否则 cf_clearance 失效。
type nativeSiteVerificationTarget struct {
	Source    string `json:"source"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UserAgent string `json:"userAgent"`
	Note      string `json:"note,omitempty"`
}

type nativeSiteVerificationView struct {
	Targets []nativeSiteVerificationTarget `json:"targets"`
	Entries []nativeSiteVerification       `json:"entries"`
}

// nativeSiteVerificationTargets 是目前接入验证流程的源。只列「确实会被
// 挑战页拦住」的站点：没遇到过的源不加，避免 UI 上出现无意义的按钮。
func nativeSiteVerificationTargets() []nativeSiteVerificationTarget {
	all := []nativeSiteVerificationTarget{
		{
			Source:    sourceHanime,
			Name:      "Han1me",
			URL:       hanimeBaseURL,
			UserAgent: hanimeUserAgent,
			Note:      "站点用 Cloudflare 挑战页拦截脚本请求；换代理节点或换域名后需要重新验证。",
		},
	}
	available := make([]nativeSiteVerificationTarget, 0, len(all))
	for _, target := range all {
		if nativeSourceAvailable(target.Source) {
			available = append(available, target)
		}
	}
	return available
}

func validSiteVerificationHost(host string) bool {
	if host == "" || len(host) > 253 || strings.Contains(host, "..") ||
		strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
}

// validateShape 只校验结构，不校验 source 是否属于当前构建：验证记录要能
// 跨构建版本保留（例如全源包写入、精简包读取时不能把条目丢掉）。
func (entry nativeSiteVerification) validateShape() error {
	source := strings.TrimSpace(entry.Source)
	if source == "" || len(source) > 64 {
		return errors.New("站点验证缺少来源标识")
	}
	if !validSiteVerificationHost(strings.TrimSpace(entry.Host)) {
		return errors.New("站点验证的域名无效")
	}
	if entry.Cookie == "" || len(entry.Cookie) > siteVerificationMaxCookie {
		return errors.New("站点验证的 Cookie 为空或过长")
	}
	// 防 HTTP 头注入：cookie 会被直接写进 Cookie 头，CR/LF 必须拒绝。
	if strings.ContainsAny(entry.Cookie, "\r\n\x00") {
		return errors.New("站点验证的 Cookie 含非法字符")
	}
	if len(entry.UserAgent) > siteVerificationMaxAgent ||
		strings.ContainsAny(entry.UserAgent, "\r\n\x00") {
		return errors.New("站点验证的 User-Agent 无效")
	}
	return nil
}

func siteVerificationHostMatches(recorded, requested string) bool {
	recorded = strings.ToLower(strings.TrimSpace(recorded))
	requested = strings.ToLower(strings.TrimSpace(requested))
	if recorded == "" || requested == "" {
		return false
	}
	// www 前缀等子域共享同一份站点 cookie；父域不反向匹配子域。
	return requested == recorded || strings.HasSuffix(requested, "."+recorded)
}

type siteVerificationStore struct {
	mu      sync.RWMutex
	entries map[string]nativeSiteVerification
	file    string
}

func loadSiteVerificationStore(directory string) *siteVerificationStore {
	store := &siteVerificationStore{
		entries: map[string]nativeSiteVerification{},
		file:    filepath.Join(directory, siteVerificationFileName),
	}
	info, err := os.Stat(store.file)
	if err != nil || !info.Mode().IsRegular() || info.Size() > siteVerificationMaxFile {
		return store
	}
	data, err := os.ReadFile(store.file)
	if err != nil {
		return store
	}
	var stored []nativeSiteVerification
	if json.Unmarshal(data, &stored) != nil {
		return store
	}
	for _, entry := range stored {
		if entry.validateShape() != nil {
			continue
		}
		store.entries[entry.Source] = entry
	}
	return store
}

func (store *siteVerificationStore) lookup(source, host string) (nativeSiteVerification, bool) {
	if store == nil {
		return nativeSiteVerification{}, false
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	entry, ok := store.entries[source]
	if !ok || !siteVerificationHostMatches(entry.Host, host) {
		return nativeSiteVerification{}, false
	}
	return entry, true
}

func (store *siteVerificationStore) list() []nativeSiteVerification {
	if store == nil {
		return nil
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	entries := make([]nativeSiteVerification, 0, len(store.entries))
	for _, entry := range store.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Source < entries[j].Source })
	return entries
}

func (store *siteVerificationStore) save(entry nativeSiteVerification) error {
	if err := entry.validateShape(); err != nil {
		return err
	}
	entry.Source = strings.TrimSpace(entry.Source)
	entry.Host = strings.ToLower(strings.TrimSpace(entry.Host))
	entry.UpdatedAt = time.Now().Unix()
	store.mu.Lock()
	defer store.mu.Unlock()
	previous, existed := store.entries[entry.Source]
	store.entries[entry.Source] = entry
	if err := store.flushLocked(); err != nil {
		if existed {
			store.entries[entry.Source] = previous
		} else {
			delete(store.entries, entry.Source)
		}
		return err
	}
	return nil
}

func (store *siteVerificationStore) clear(source string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	previous, existed := store.entries[source]
	if !existed {
		return nil
	}
	delete(store.entries, source)
	if err := store.flushLocked(); err != nil {
		store.entries[source] = previous
		return err
	}
	return nil
}

// flushLocked 原子替换：先写临时文件再 rename，权限固定 0600（含凭据）。
func (store *siteVerificationStore) flushLocked() error {
	entries := make([]nativeSiteVerification, 0, len(store.entries))
	for _, entry := range store.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Source < entries[j].Source })
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > siteVerificationMaxFile {
		return errors.New("站点验证数据过大")
	}
	temp := store.file + ".tmp"
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	if err := os.Chmod(temp, 0600); err != nil {
		os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, store.file); err != nil {
		os.Remove(temp)
		return err
	}
	return nil
}

// applySiteVerification 在发请求前注入该源已验证的 Cookie 与 UA。只对
// 记录里的 host 生效；UA 覆盖源自身 UA 是必须的——cookie 绑 UA hash。
func (d *Downloader) applySiteVerification(request *http.Request, source string) {
	if d == nil || request == nil || source == "" || d.siteVerifications == nil {
		return
	}
	entry, ok := d.siteVerifications.lookup(source, request.URL.Hostname())
	if !ok {
		return
	}
	request.Header.Set("Cookie", entry.Cookie)
	if entry.UserAgent != "" {
		request.Header.Set("User-Agent", entry.UserAgent)
	}
}

func (engine *nativeEngine) siteVerifications() nativeSiteVerificationView {
	view := nativeSiteVerificationView{Targets: nativeSiteVerificationTargets()}
	if engine != nil && engine.downloader != nil {
		view.Entries = engine.downloader.siteVerifications.list()
	}
	if view.Entries == nil {
		view.Entries = []nativeSiteVerification{}
	}
	return view
}

func (engine *nativeEngine) saveSiteVerification(input nativeSiteVerification) (nativeSiteVerificationView, error) {
	// 只允许写当前构建可用的源，避免桥接层被用来给任意 source 塞凭据。
	if !nativeSourceAvailable(input.Source) {
		return nativeSiteVerificationView{}, errNativeBuildSource
	}
	if err := engine.downloader.siteVerifications.save(input); err != nil {
		return nativeSiteVerificationView{}, err
	}
	return engine.siteVerifications(), nil
}

func (engine *nativeEngine) clearSiteVerification(source string) (nativeSiteVerificationView, error) {
	if source = strings.TrimSpace(source); source == "" {
		return nativeSiteVerificationView{}, errors.New("站点验证缺少来源标识")
	}
	if err := engine.downloader.siteVerifications.clear(source); err != nil {
		return nativeSiteVerificationView{}, err
	}
	return engine.siteVerifications(), nil
}
