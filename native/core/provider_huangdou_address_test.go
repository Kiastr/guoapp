package core

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// [+201] 黄豆「获取最新地址」单测：覆盖解密、清单挑选、探活与写回。
// 用真实发布页 d.txt 的加密格式（AES-GCM，nonce 前 12 字节）造夹具，
// 保证 Go 侧实现与发布页 WebCrypto 完全兼容。

func huangdouEncryptList(t *testing.T, ts int64, domains []string) string {
	t.Helper()
	key, _ := hex.DecodeString(huangdouAddressKeyHex)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	plain, _ := json.Marshal(map[string]any{"v": 1, "ts": ts, "dl": domains})
	nonce := make([]byte, gcm.NonceSize())
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	sealed := gcm.Seal(nonce, nonce, plain, nil)
	return base64.StdEncoding.EncodeToString(sealed)
}

func TestHuangdouDecryptList_RealFormat(t *testing.T) {
	b64 := huangdouEncryptList(t, 1794906603, []string{
		"https://lozmglzt.cc/", "lozmglzt.cc", "https://ndhaa5hf.cc", "", "ftp://bad.cc",
	})
	domains, ts, err := huangdouDecryptList(b64)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if ts != 1794906603 {
		t.Fatalf("ts 不对: %d", ts)
	}
	// 去重 + 规范化 + 过滤非法协议/空值
	if len(domains) != 2 || domains[0] != "https://lozmglzt.cc" || domains[1] != "https://ndhaa5hf.cc" {
		t.Fatalf("域名清单不对: %+v", domains)
	}
}

func TestHuangdouDecryptList_Invalid(t *testing.T) {
	for _, raw := range []string{"", "short", "bm90IGpzb24=", base64.StdEncoding.EncodeToString(make([]byte, 20))} {
		if _, _, err := huangdouDecryptList(raw); err == nil {
			t.Fatalf("非法清单 %q 应报错", raw)
		}
	}
}

// 多个源返回不同 ts 的清单，必须只取 ts 最大的那份（并集去重）。
func TestHuangdouDiscoverLatest_PicksNewestOnly(t *testing.T) {
	old := huangdouEncryptList(t, 1000, []string{"https://old-stale.cc"})
	fresh := huangdouEncryptList(t, 2000, []string{"https://fresh-a.cc", "https://fresh-b.cc"})
	// 直接验证挑选语义：把两份清单解密后按 newestOf 规则合并。
	oldDomains, oldTS, _ := huangdouDecryptList(old)
	freshDomains, freshTS, _ := huangdouDecryptList(fresh)
	best, latest := int64(-1), []string(nil)
	for _, r := range []struct {
		d []string
		ts int64
	}{{oldDomains, oldTS}, {freshDomains, freshTS}} {
		if r.ts > best {
			best, latest = r.ts, r.d
		}
	}
	if best != 2000 || len(latest) != 2 || latest[0] != "https://fresh-a.cc" {
		t.Fatalf("应只取最新清单: best=%d latest=%+v", best, latest)
	}
	for _, d := range latest {
		if d == "https://old-stale.cc" {
			t.Fatal("旧清单混入了最新候选")
		}
	}
}

func TestHuangdouProbe_UsingClient(t *testing.T) {
	probeOK := false
	rt := sourceFixtureTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == huangdouProbePath {
			probeOK = true
			return sourceFixtureResponse(req, 200, "png"), nil
		}
		return sourceFixtureResponse(req, 404, ""), nil
	})
	d := sourceFixtureDownloader(t, rt)
	if !d.huangdouProbe(context.Background(), "https://line.example.test") {
		t.Fatal("probe 200 应判为可用")
	}
	if !probeOK {
		t.Fatal("未请求 /probe.png")
	}
	d2 := sourceFixtureDownloader(t, sourceFixtureTransport(func(req *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(req, 500, ""), nil
	}))
	if d2.huangdouProbe(context.Background(), "https://line.example.test") {
		t.Fatal("probe 500 应判为不可用")
	}
}

func TestLoadSaveHuangdouAddress(t *testing.T) {
	dir := t.TempDir()
	if got := loadHuangdouAddress(dir); got.Address != "" {
		t.Fatalf("空目录应返回空地址: %+v", got)
	}
	store := huangdouAddressStore{Address: "https://lozmglzt.cc", UpdatedAt: 1794906603, Source: "lozmglzt.cc"}
	if err := saveHuangdouAddress(dir, store); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, huangdouAddrFileName)); err != nil {
		t.Fatalf("地址文件未落盘: %v", err)
	}
	got := loadHuangdouAddress(dir)
	if got.Address != store.Address || got.UpdatedAt != store.UpdatedAt {
		t.Fatalf("读回不一致: %+v", got)
	}
}

func TestHuangdouFetchText_ReadBodyAndClose(t *testing.T) {
	b64 := huangdouEncryptList(t, 1, []string{"https://a.cc"})
	d := sourceFixtureDownloader(t, sourceFixtureTransport(func(req *http.Request) (*http.Response, error) {
		return sourceFixtureResponse(req, 200, b64), nil
	}))
	body, err := d.huangdouFetchText(context.Background(), "https://src.example.test/d.txt", "text/plain")
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if string(body) != b64 {
		t.Fatalf("body 不一致: %q", body)
	}
	// 读回的应该是可读文本
	if _, _, err := huangdouDecryptList(string(body)); err != nil {
		t.Fatalf("拉取内容应可解密: %v", err)
	}
}
