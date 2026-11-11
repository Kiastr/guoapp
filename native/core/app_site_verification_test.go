package core

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// [站点验证] 通用骨架的存储、校验与注入。

func TestSiteVerificationRejectsUnsafeInput(t *testing.T) {
	valid := nativeSiteVerification{
		Source: sourceHanime, Host: "hanime1.me",
		UserAgent: "Mozilla/5.0 test", Cookie: "cf_clearance=abc; __cf_bm=def",
	}
	if err := valid.validateShape(); err != nil {
		t.Fatalf("合法凭据不应被拒绝：%v", err)
	}
	cases := []struct {
		name  string
		entry nativeSiteVerification
	}{
		// Cookie 直接写进请求头，CR/LF 必须挡掉，否则可注入任意头。
		{"cookie 含换行", nativeSiteVerification{Source: sourceHanime, Host: "hanime1.me", Cookie: "cf_clearance=abc\r\nX-Evil: 1"}},
		{"cookie 为空", nativeSiteVerification{Source: sourceHanime, Host: "hanime1.me"}},
		{"cookie 过长", nativeSiteVerification{Source: sourceHanime, Host: "hanime1.me", Cookie: strings.Repeat("a", siteVerificationMaxCookie+1)}},
		{"ua 含换行", nativeSiteVerification{Source: sourceHanime, Host: "hanime1.me", Cookie: "a=b", UserAgent: "x\r\ny"}},
		{"host 带路径", nativeSiteVerification{Source: sourceHanime, Host: "hanime1.me/evil", Cookie: "a=b"}},
		{"host 为空", nativeSiteVerification{Source: sourceHanime, Cookie: "a=b"}},
		{"缺 source", nativeSiteVerification{Host: "hanime1.me", Cookie: "a=b"}},
	}
	for _, test := range cases {
		if err := test.entry.validateShape(); err == nil {
			t.Fatalf("%s 应被拒绝", test.name)
		}
	}
}

func TestSiteVerificationStoreRoundTrip(t *testing.T) {
	directory := t.TempDir()
	store := loadSiteVerificationStore(directory)
	if err := store.save(nativeSiteVerification{
		Source: sourceHanime, Host: "hanime1.me",
		UserAgent: "Mozilla/5.0 test", Cookie: "cf_clearance=abc; __cf_bm=def",
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(directory, siteVerificationFileName))
	if err != nil {
		t.Fatal(err)
	}
	// 凭据文件必须只有本用户可读。
	if permission := info.Mode().Perm(); permission != 0600 {
		t.Fatalf("凭据文件权限应为 0600，实际 %o", permission)
	}

	reloaded := loadSiteVerificationStore(directory)
	for _, host := range []string{"hanime1.me", "www.hanime1.me"} {
		if entry, ok := reloaded.lookup(sourceHanime, host); !ok || !strings.Contains(entry.Cookie, "cf_clearance=abc") {
			t.Fatalf("%s 应命中已存凭据：%+v %v", host, entry, ok)
		}
	}
	// 不同 host、不同源都不能命中：cookie 绑域名，串源会泄凭据。
	if _, ok := reloaded.lookup(sourceHanime, "evil.example"); ok {
		t.Fatal("跨域名不应命中")
	}
	if _, ok := reloaded.lookup(sourceHuangguoAI, "hanime1.me"); ok {
		t.Fatal("跨源不应命中")
	}

	// 清空后文件里不能残留 cookie。
	if err := reloaded.clear(sourceHanime); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, siteVerificationFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "cf_clearance") {
		t.Fatalf("清除后仍残留凭据：%s", data)
	}
	if entries := loadSiteVerificationStore(directory).list(); len(entries) != 0 {
		t.Fatalf("清除后不应有记录：%+v", entries)
	}
}

func TestSiteVerificationInjectsCookieAndUserAgent(t *testing.T) {
	var seen *http.Request
	downloader := sourceFixtureDownloader(t, func(request *http.Request) (*http.Response, error) {
		seen = request
		return sourceFixtureResponse(request, 200, "<html><body>ok</body></html>"), nil
	})
	downloader.siteVerifications = loadSiteVerificationStore(t.TempDir())
	const verifiedAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Verified/1.0"
	if err := downloader.siteVerifications.save(nativeSiteVerification{
		Source: sourceHanime, Host: "hanime1.me",
		UserAgent: verifiedAgent, Cookie: "cf_clearance=abc; __cf_bm=def",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := downloader.fetchProviderText(context.Background(), hanimeBaseURL+"/search?page=1", ""); err != nil {
		t.Fatal(err)
	}
	if seen == nil {
		t.Fatal("请求未发出")
	}
	if cookie := seen.Header.Get("Cookie"); cookie != "cf_clearance=abc; __cf_bm=def" {
		t.Fatalf("Cookie 未注入：%q", cookie)
	}
	// UA 必须被验证记录覆盖：cf_clearance 绑的是验证时的 UA。
	if agent := seen.Header.Get("User-Agent"); agent != verifiedAgent {
		t.Fatalf("User-Agent 未覆盖：%q", agent)
	}

	// 未验证的源不能凭空带 Cookie。
	seen = nil
	downloader.siteVerifications.clear(sourceHanime)
	if _, err := downloader.fetchProviderText(context.Background(), hanimeBaseURL+"/search?page=2", ""); err != nil {
		t.Fatal(err)
	}
	if cookie := seen.Header.Get("Cookie"); cookie != "" {
		t.Fatalf("清除后不应再带 Cookie：%q", cookie)
	}
}

func TestSiteVerificationSaveRejectsUnavailableSource(t *testing.T) {
	previous := buildAllSources
	buildAllSources = "true"
	defer func() { buildAllSources = previous }()

	engine := &nativeEngine{downloader: &Downloader{
		siteVerifications: loadSiteVerificationStore(t.TempDir()),
	}}
	if _, err := engine.saveSiteVerification(nativeSiteVerification{
		Source: "not-a-source", Host: "hanime1.me", Cookie: "a=b",
	}); err == nil {
		t.Fatal("未知源应被拒绝")
	}
	view, err := engine.saveSiteVerification(nativeSiteVerification{
		Source: sourceHanime, Host: "hanime1.me", UserAgent: "ua", Cookie: "a=b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || len(view.Targets) == 0 {
		t.Fatalf("保存后应返回最新状态：%+v", view)
	}
}
