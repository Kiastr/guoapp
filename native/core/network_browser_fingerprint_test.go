package core

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
)

// [指纹] hanime 必须走浏览器 TLS/HTTP2 指纹，而不是裸 Go Transport。
//
// 为什么值得单独立测：这一组全是「静默失效」的参数——UA 退回旧版本、profile
// 表漏登记 hanime、头生成取错 profile，编译期都看不出来，运行时只表现为
// 「站点还是 403」。线上排查一次要换好几个节点，不如在这里钉死。

func newFingerprintTestTransport(t *testing.T) *huangguoBrowserTransport {
	t.Helper()
	downloader := &Downloader{cfg: defaultConfig(), providerHosts: map[string]string{}}
	base := http.DefaultTransport.(*http.Transport).Clone()
	return newHuangguoBrowserTransport(newCDNTransport(base, newDNSResolver(base)), downloader)
}

// headerValue 大小写无关地取 fhttp 头的第一个值。
//
// 不能直接用 Header.Get：指纹头是按全小写 key 存的（为了 HeaderOrderKey
// 顺序输出），Get 走 canonical key 会查不到。
func headerValue(headers fhttp.Header, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func TestBrowserFingerprintTransportCoversHanime(t *testing.T) {
	transport := newFingerprintTestTransport(t)

	hanime, _ := http.NewRequest(http.MethodGet, hanimeBaseURL+"/search?page=1", nil)
	if !transport.matches(hanime) {
		t.Fatal("hanime 请求必须走指纹通道：裸 Go ClientHello 会被 Cloudflare 直接判 1020，连挑战页都拿不到")
	}

	// 不能把任意第三方源也拖进来：指纹通道会改写 UA / sec-fetch-*，
	// 别的源可能依赖原样请求（签名、Referer 校验）。
	other, _ := http.NewRequest(http.MethodGet, "https://example.com/page", nil)
	if transport.matches(other) {
		t.Fatal("无关站点不应走指纹通道")
	}

	// 非 GET/HEAD 不走：fhttp 转换会丢 body 语义。
	post, _ := http.NewRequest(http.MethodPost, hanimeBaseURL+"/search", strings.NewReader("q=1"))
	if transport.matches(post) {
		t.Fatal("POST 不应走指纹通道")
	}
}

func TestBrowserFingerprintHeadersFollowSource(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, hanimeBaseURL+"/", nil)
	headers := browserHeaders(request, browserFingerprintSources[sourceHanime])

	// UA 必须与站点验证页、cf_clearance 绑定的那个字符串逐字一致。
	if got := headerValue(headers, "user-agent"); got != hanimeUserAgent {
		t.Fatalf("hanime UA 应为 %q，实际 %q", hanimeUserAgent, got)
	}
	if got := headerValue(headers, "sec-ch-ua-platform"); got != `"Windows"` {
		t.Fatalf("hanime 平台声明应为 Windows，实际 %q", got)
	}
	// UA 说 Chrome 150，Client Hints 也得说 150，否则 UA ↔ 指纹自相矛盾。
	if got := headerValue(headers, "sec-ch-ua"); !strings.Contains(got, `v="150"`) {
		t.Fatalf("Client Hints 主版本应与 TLS profile 一致（150），实际 %q", got)
	}
	if !strings.Contains(headerValue(headers, "user-agent"), "Chrome/150") {
		t.Fatalf("UA 主版本应与 TLS profile 一致（150），实际 %q", headerValue(headers, "user-agent"))
	}

	// 黄果保持 macOS 身份：改它等于动一条已经跑通的生产链路。
	huangguo := browserHeaders(request, huangguoFingerprintProfile)
	if got := headerValue(huangguo, "sec-ch-ua-platform"); got != `"macOS"` {
		t.Fatalf("黄果 profile 不应被 hanime 覆盖，实际 %q", got)
	}

	// 头顺序本身是 h2 指纹的一部分，不能被丢。
	if order := headers[fhttp.HeaderOrderKey]; len(order) == 0 || order[0] != "sec-ch-ua" {
		t.Fatalf("HeaderOrderKey 丢失或被改写：%v", order)
	}
}

func TestForbiddenBackoffIsShortButRateLimitIsNot(t *testing.T) {
	limiter := newRequestLimiter(3, 0)
	request, _ := http.NewRequest(http.MethodGet, hanimeBaseURL+"/", nil)
	limiter.observe(request, &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}})
	backoff := limiter.backoffs["hanime1.me"]
	if backoff == nil {
		t.Fatal("403 应记录退避（防止连点加重风控）")
	}
	// 换节点、刚做完站点验证都不该被一分钟退避挡住——原来固定 60 秒，
	// 用户看到的就是「验证成功了但还是打不开」。
	if remaining := time.Until(backoff.until); remaining > 10*time.Second {
		t.Fatalf("403 只应短退避，实际还要等 %v", remaining)
	}

	rateLimited := newRequestLimiter(3, 0)
	request, _ = http.NewRequest(http.MethodGet, hanimeBaseURL+"/", nil)
	rateLimited.observe(request, &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": {"120"}},
	})
	if got := rateLimited.backoffs["hanime1.me"]; got == nil || time.Until(got.until) < 60*time.Second {
		t.Fatalf("429 应按 Retry-After 全量退避：%+v", got)
	}
}

func TestCatalogBlockReasonSeparatesBanFromChallenge(t *testing.T) {
	banBody := []byte(`<html><head><title>Attention Required! | Cloudflare</title></head>` +
		`<body>Sorry, you have been blocked</body></html>`)
	ban := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}}
	if reason := catalogResponseBlockReason(ban, banBody); !strings.Contains(reason, "封禁") {
		t.Fatalf("1020 应判定为 IP 封禁，实际 %q", reason)
	}

	challenge := &http.Response{StatusCode: http.StatusForbidden,
		Header: http.Header{"Cf-Mitigated": {"challenge"}}}
	if reason := catalogResponseBlockReason(challenge, []byte("<html></html>")); !strings.Contains(reason, "浏览器验证") {
		t.Fatalf("挑战页应单独识别，实际 %q", reason)
	}
}

func TestHanimeRequestErrorPointsToTheRightFix(t *testing.T) {
	banned := hanimeRequestError(errors.New("hanime1.me HTTP 403：当前节点 IP 被 Cloudflare 封禁（Error 1020）"))
	if banned == nil || !strings.Contains(banned.Error(), "更换代理节点") {
		t.Fatalf("封禁应提示换节点：%v", banned)
	}
	// 封禁时把用户引去站点验证是错的：没有挑战页就永远拿不到 cf_clearance。
	if strings.Contains(banned.Error(), "站点验证 → Han1me") {
		t.Fatalf("封禁文案不该引导去做站点验证：%v", banned)
	}

	challenged := hanimeRequestError(errors.New("hanime1.me HTTP 403：站点要求浏览器验证（Cloudflare 挑战）"))
	if challenged == nil || !strings.Contains(challenged.Error(), "站点验证") {
		t.Fatalf("挑战应提示去站点验证：%v", challenged)
	}

	// 与 Cloudflare 无关的错误原样返回，别再套一层会误导人的建议。
	plain := errors.New("hanime1.me HTTP 500")
	if got := hanimeRequestError(plain); got != plain {
		t.Fatalf("无关错误不应被改写：%v", got)
	}
}
