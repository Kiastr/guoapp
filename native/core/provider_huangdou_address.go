package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// [+201] 黄豆「获取最新地址」：复刻黄豆地址发布页的清单发现逻辑，
// 让用户在设置页一键把黄豆 API 域名更新到当前官方线路，不再依赖写死的旧域名。
//
// 发布页（hddj30.com / hddj50.com）本身只是一段 JS：从多个固定源拉 AES-GCM 加密的
// d.txt（或 DoH TXT 分片兜底），用页面里硬编码的密钥解密出域名清单 {ts, dl:[...]}，
// 只取 ts 最大的最新清单，逐条 GET /probe.png 探活，跳到第一个通的线路。
// 这里用 Go 标准库完整复现「取清单 → 解密 → 选最新 → 探活」，再把选中的域名写回，
// 使 providerBaseURL(sourceHuangdou) 与新发起的黄豆请求都走它。不跳转、不写死域名、
// 不跳过证书校验（新线路证书有效，按标准 TLS 校验即可）。
const (
	huangdouAddressKeyHex = "7127fe3cb8ef5bea708306ee7ae109f3c121aad09590f27e3bcdc07e1b255b54"
	huangdouProbePath     = "/probe.png"
	huangdouAddrFileName  = "huangdou-address.json"

	huangdouAddrSourceTimeout = 6 * time.Second
	huangdouAddrProbeTimeout  = 4 * time.Second
)

var huangdouAddressFileSources = []string{
	"https://cdn.jsdelivr.net/gh/npskle/assets@8d1ce55c8a3b85c7bd8c63f20e22dd98f91d7e21/d.txt",
	"https://fastly.jsdelivr.net/gh/npskle/assets@8d1ce55c8a3b85c7bd8c63f20e22dd98f91d7e21/d.txt",
	"https://gcore.jsdelivr.net/gh/npskle/assets@8d1ce55c8a3b85c7bd8c63f20e22dd98f91d7e21/d.txt",
	"https://gitlab.com/api/v4/projects/kle-group3%2Fassets/repository/files/d.txt/raw?ref=main",
}

var huangdouAddressDoH = "https://dns.alidns.com/resolve?name=s.fbstudy.wiki&type=TXT"

// huangdouAddressStore 持久化在应用数据目录，随配置备份一起走。
type huangdouAddressStore struct {
	Address   string `json:"address"`
	UpdatedAt int64  `json:"updatedAt,omitempty"` // Unix 秒
	Source    string `json:"source,omitempty"`    // 命中的线路域名（清单来源）
}

// huangdouAddressResult 返回给设置页的可见结果。
type huangdouAddressResult struct {
	Address    string   `json:"address"`
	Candidates []string `json:"candidates,omitempty"` // 本次清单里探活通过的线路，便于用户手动换
	UpdatedAt  int64    `json:"updatedAt,omitempty"`
	Message    string   `json:"message,omitempty"`
}

func huangdouAddressPath(directory string) string {
	return filepath.Join(directory, huangdouAddrFileName)
}

func loadHuangdouAddress(directory string) huangdouAddressStore {
	var store huangdouAddressStore
	raw, err := os.ReadFile(huangdouAddressPath(directory))
	if err != nil || json.Unmarshal(raw, &store) != nil {
		return huangdouAddressStore{}
	}
	if !isProviderHTTPMediaURL(store.Address) {
		return huangdouAddressStore{}
	}
	return store
}

func saveHuangdouAddress(directory string, store huangdouAddressStore) error {
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	return nativeDownloadWrite(huangdouAddressPath(directory), data)
}

// huangdouDecryptList 解密 d.txt / DoH 分片拼接后的 base64 清单。
// 与发布页 WebCrypto 一致：base64 解码后 [0:12] 为 GCM nonce，其余为密文+tag，无 AAD。
func huangdouDecryptList(b64 string) ([]string, int64, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		// 兼容 URL-safe base64
		raw, err = base64.URLEncoding.DecodeString(strings.TrimSpace(b64))
	}
	if err != nil || len(raw) < 28 {
		return nil, 0, errors.New("黄豆地址清单格式无效")
	}
	key, err := hex.DecodeString(huangdouAddressKeyHex)
	if err != nil {
		return nil, 0, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, 0, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return nil, 0, errors.New("黄豆地址清单无法解密")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return nil, 0, errors.New("黄豆地址清单解密失败")
	}
	var doc struct {
		TS int64    `json:"ts"`
		DL []string `json:"dl"`
	}
	if err := json.Unmarshal(plain, &doc); err != nil {
		return nil, 0, errors.New("黄豆地址清单内容无效")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(doc.DL))
	for _, item := range doc.DL {
		domain := normalizeHuangdouDomain(item)
		if domain != "" && !seen[domain] {
			seen[domain] = true
			out = append(out, domain)
		}
	}
	if len(out) == 0 {
		return nil, 0, errors.New("黄豆地址清单为空")
	}
	return out, doc.TS, nil
}

func normalizeHuangdouDomain(raw string) string {
	d := strings.TrimSpace(raw)
	if d == "" {
		return ""
	}
	if !strings.Contains(d, "://") {
		d = "https://" + d
	}
	parsed, err := url.Parse(d)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// huangdouFetchText 用应用请求链（含代理/限流）拉文本，拒绝超大响应。
func (d *Downloader) huangdouFetchText(ctx context.Context, rawURL string, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := d.doCatalogRequestWithTimeout(req, huangdouAddrSourceTimeout)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// huangdouListFromDOH 阿里 DoH 的 TXT 记录兜底：v=1;i=<序号>;n=<总数>;d=<base64> 分片。
func (d *Downloader) huangdouListFromDOH(ctx context.Context) ([]string, int64, error) {
	body, err := d.huangdouFetchText(ctx, huangdouAddressDoH, "application/dns-json")
	if err != nil {
		return nil, 0, err
	}
	var doc struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, 0, err
	}
	parts := map[int]string{}
	total := 0
	for _, ans := range doc.Answer {
		t := strings.ReplaceAll(ans.Data, `"`, "")
		at := strings.Index(t, "v=1;")
		if at < 0 {
			continue
		}
		t = strings.ReplaceAll(t[at:], " ", "")
		kv := map[string]string{}
		for _, seg := range strings.Split(t, ";") {
			if p := strings.Index(seg, "="); p > 0 {
				kv[seg[:p]] = seg[p+1:]
			}
		}
		if n, ok := kv["n"]; ok {
			total = atoiDefault(n, total)
		}
		if val, ok := kv["d"]; ok {
			parts[atoiDefault(kv["i"], 0)] = val
		}
	}
	keys := make([]int, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	if total > 0 && len(keys) != total {
		return nil, 0, errors.New("黄豆地址 TXT 分片不完整")
	}
	var b64 strings.Builder
	for _, k := range keys {
		b64.WriteString(parts[k])
	}
	if b64.Len() == 0 {
		return nil, 0, errors.New("黄豆地址 TXT 为空")
	}
	return huangdouDecryptList(b64.String())
}

// huangdouDiscoverLatest 取所有源的清单，只取 ts 最大的最新一份（并集去重）。
// 与发布页 newestOf 一致：避免被换下但服务仍活的旧线路混入候选。
func (d *Downloader) huangdouDiscoverLatest(ctx context.Context) ([]string, int64, error) {
	type result struct {
		domains []string
		ts      int64
	}
	ch := make(chan result, len(huangdouAddressFileSources)+1)
	fetch := func(rawURL, accept string) {
		body, err := d.huangdouFetchText(ctx, rawURL, accept)
		if err != nil {
			return
		}
		domains, ts, err := huangdouDecryptList(string(body))
		if err == nil {
			ch <- result{domains, ts}
		}
	}
	var wg sync.WaitGroup
	for _, src := range huangdouAddressFileSources {
		wg.Add(1)
		go func(u string) { defer wg.Done(); fetch(u, "text/plain,application/octet-stream,*/*") }(src)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		domains, ts, err := d.huangdouListFromDOH(ctx)
		if err == nil {
			ch <- result{domains, ts}
		}
	}()
	go func() { wg.Wait(); close(ch) }()

	var best int64 = -1
	seen := map[string]bool{}
	var latest []string
	for r := range ch {
		if r.ts > best {
			best = r.ts
			seen = map[string]bool{}
			latest = nil
		}
		if r.ts == best {
			for _, domain := range r.domains {
				if !seen[domain] {
					seen[domain] = true
					latest = append(latest, domain)
				}
			}
		}
	}
	if len(latest) == 0 {
		return nil, 0, errors.New("未能获取黄豆最新地址，请稍后重试或检查网络")
	}
	return latest, best, nil
}

// huangdouProbe 探活：GET /probe.png 成功（任意 2xx）即视为该线路可用。
func (d *Downloader) huangdouProbe(ctx context.Context, domain string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, huangdouAddrProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(domain, "/")+huangdouProbePath, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// resolveHuangdouLatestAddress 一键获取：发现最新清单 → 逐条探活 → 写回第一条可用线路。
func (engine *nativeEngine) resolveHuangdouLatestAddress(ctx context.Context) (huangdouAddressResult, error) {
	domains, ts, err := engine.downloader.huangdouDiscoverLatest(ctx)
	if err != nil {
		return huangdouAddressResult{}, err
	}
	var okDomains []string
	for _, domain := range domains {
		if engine.downloader.huangdouProbe(ctx, domain) {
			okDomains = append(okDomains, domain)
		}
	}
	if len(okDomains) == 0 {
		return huangdouAddressResult{Message: "已取到最新线路，但当前网络下均探活失败，请检查代理后重试"}, nil
	}
	picked := okDomains[0]
	store := huangdouAddressStore{Address: picked, UpdatedAt: ts, Source: picked}
	engine.settingsMu.Lock()
	defer engine.settingsMu.Unlock()
	if err := saveHuangdouAddress(engine.directory, store); err != nil {
		return huangdouAddressResult{}, errors.New("已找到可用线路，但保存失败，请重试")
	}
	engine.downloader.cfg.HuangdouURL = picked
	engine.downloader.providerMu.Lock()
	engine.downloader.providerHosts[sourceHuangdou] = picked
	engine.downloader.providerMu.Unlock()
	engine.downloader.client.CloseIdleConnections()
	return huangdouAddressResult{
		Address: picked, Candidates: okDomains, UpdatedAt: ts,
		Message: fmt.Sprintf("已更新黄豆地址：%s（%d 条线路可用）", picked, len(okDomains)),
	}, nil
}

// applyHuangdouAddress 启动时把持久化的地址灌回运行配置。
func (engine *nativeEngine) applyHuangdouAddress() {
	store := loadHuangdouAddress(engine.directory)
	if store.Address == "" {
		return
	}
	engine.downloader.cfg.HuangdouURL = store.Address
	engine.downloader.providerMu.Lock()
	engine.downloader.providerHosts[sourceHuangdou] = store.Address
	engine.downloader.providerMu.Unlock()
}
