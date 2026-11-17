package core

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// [+200] playback 接口兑换的单元测试。
// 站点改版后详情页不再内嵌明文 m3u8，改由 POST /api/videos/{id}/episodes/{ep}/playback
// 返回 streams.main/hevc/preview。这里用夹具 transport 拦截请求，验证：
//   - 方法为 POST、endpoint 拼对（含 id/ep）、body 为空 JSON、带主域 Referer/Origin
//   - main → hevc → preview 依次兜底
//   - 空响应 / HTTP 错误 / 非 URL 流 / 参数非法时返回空串（调用方据此回退旧 HTML 解析）

func huangguoPlaybackDownloader(t *testing.T, rt sourceFixtureTransport) *Downloader {
	t.Helper()
	return &Downloader{
		cfg:                   Config{dataDir: t.TempDir(), Retries: 1, PageSize: 30, MaxPagesPerSort: 1, HuangguoAIURL: "https://huangguoai.com"},
		drpyAllowLocalTargets: true,
		client:                &http.Client{Transport: rt},
		limiter:               newRequestLimiter(3, 0),
		providerHosts:         map[string]string{},
	}
}

func TestResolveHuangguoAIPlayback_MainFirst(t *testing.T) {
	var seen *http.Request
	var seenBody string
	d := huangguoPlaybackDownloader(t, func(req *http.Request) (*http.Response, error) {
		seen = req
		raw, _ := io.ReadAll(req.Body)
		seenBody = string(raw)
		return sourceFixtureResponse(req, 200, `{"data":{"streams":{"main":"https://cdn.example.test/main.m3u8?auth_key=x","hevc":"https://cdn.example.test/hevc.m3u8","preview":"https://cdn.example.test/preview.m3u8"}}}`), nil
	})

	got := d.resolveHuangguoAIPlayback(context.Background(), "12560", 3)
	if got != "https://cdn.example.test/main.m3u8?auth_key=x" {
		t.Fatalf("应优先返回 main，实际 %q", got)
	}
	if seen == nil || seen.Method != http.MethodPost {
		t.Fatalf("请求方法应为 POST，实际 %+v", seen)
	}
	if want := "https://huangguoai.com/api/videos/12560/episodes/3/playback"; seen.URL.String() != want {
		t.Fatalf("endpoint 不对：want %s, got %s", want, seen.URL.String())
	}
	if seenBody != "{}" {
		t.Fatalf("body 应为空 JSON {}，实际 %q", seenBody)
	}
	if seen.Header.Get("Referer") != "https://huangguoai.com/" {
		t.Fatalf("Referer 应为主域首页，实际 %q", seen.Header.Get("Referer"))
	}
}

func TestResolveHuangguoAIPlayback_FallbackOrder(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"hevc兜底", `{"data":{"streams":{"hevc":"https://cdn.example.test/hevc.m3u8","preview":"https://cdn.example.test/p.m3u8"}}}`, "https://cdn.example.test/hevc.m3u8"},
		{"preview兜底", `{"data":{"streams":{"preview":"https://cdn.example.test/p.m3u8"}}}`, "https://cdn.example.test/p.m3u8"},
		{"无有效流返回空", `{"data":{"streams":{"main":"", "hevc":"not-a-url"}}}`, ""},
		{"空data返回空", `{}`, ""},
		{"非法JSON返回空", `not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := huangguoPlaybackDownloader(t, func(req *http.Request) (*http.Response, error) {
				return sourceFixtureResponse(req, 200, tc.body), nil
			})
			if got := d.resolveHuangguoAIPlayback(context.Background(), "12560", 1); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestResolveHuangguoAIPlayback_FailuresReturnEmpty(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		badArgs bool
	}{
		{"HTTP错误", 403, false},
		{"参数非法-id空", 200, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			d := huangguoPlaybackDownloader(t, func(req *http.Request) (*http.Response, error) {
				called = true
				return sourceFixtureResponse(req, tc.status, `{"data":{"streams":{"main":"https://cdn.example.test/m.m3u8"}}}`), nil
			})
			id, ep := "12560", 1
			if tc.badArgs {
				id, ep = "", 1
			}
			got := d.resolveHuangguoAIPlayback(context.Background(), id, ep)
			if got != "" {
				t.Fatalf("失败场景应返回空串让调用方回退，实际 %q", got)
			}
			if tc.badArgs && called {
				t.Fatalf("参数非法时不应发起网络请求")
			}
		})
	}
}

func TestResolveHuangguoAIPlayback_UsesConfiguredBaseURL(t *testing.T) {
	var seenURL string
	d := huangguoPlaybackDownloader(t, func(req *http.Request) (*http.Response, error) {
		seenURL = req.URL.String()
		return sourceFixtureResponse(req, 200, `{"data":{"streams":{"main":"https://cdn.example.test/m.m3u8"}}}`), nil
	})
	d.cfg.HuangguoAIURL = "https://custom.example.test"
	d.resolveHuangguoAIPlayback(context.Background(), "42", 7)
	if !strings.HasPrefix(seenURL, "https://custom.example.test/api/videos/42/episodes/7/playback") {
		t.Fatalf("应走配置的 baseURL，实际 %q", seenURL)
	}
}
