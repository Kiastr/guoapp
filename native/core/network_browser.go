package core

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

type browserHTTPClient interface {
	Do(*fhttp.Request) (*fhttp.Response, error)
	CloseIdleConnections()
}

// browserFingerprintProfile 是「伪装成哪个浏览器」的一组常量。
//
// UA / Client Hints / TLS profile 三者必须指向同一个 Chrome 主版本：CF 会
// 做 UA ↔ JA4 的一致性检查，Chrome/126 的 UA 配 Chrome/150 的握手反而更
// 可疑。tlsclient 侧统一用 profiles.Chrome_150，所以这里也只写 150。
type browserFingerprintProfile struct {
	// label 只用于诊断日志，方便对照是哪个身份发出的请求。
	label      string
	userAgent  string
	platform   string
	clientHint string
	// mobile 决定 sec-ch-ua-mobile 与 Accept 的取值。桌面 UA 配 ?0、移动
	// UA 配 ?1，两者对不上时 CF 会把请求当成「被篡改过的客户端」。
	mobile bool
}

var huangguoFingerprintProfile = browserFingerprintProfile{
	label:      "Chrome 150 macOS",
	userAgent:  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
	platform:   "macOS",
	clientHint: `"Chromium";v="150", "Google Chrome";v="150", "Not_A Brand";v="24"`,
}

// browserFingerprintSources 是需要「浏览器级 TLS/HTTP2 指纹」的源。
//
// 为什么必须按源登记而不是全站打开：这里的 UA 会被写进请求，而 cf_clearance
// 绑 (IP, UA)。站点验证页用的也是同一份 UA，两边只要差一个字，验证就白做。
//
// 为什么 hanime 非走这里不可：裸 Go Transport 的 ClientHello 是公认的非
// 浏览器指纹（JA3/JA4 一望即知），Cloudflare 对它不发挑战页，而是直接
// 403 + "you have been blocked"。没有挑战页 = 站点验证永远拿不到
// cf_clearance，那条路是死的。只有把 TLS 层也伪装成 Chrome，请求才会落进
// 「正常访客」分支：要么直接放行，要么给一个能解的挑战。
var browserFingerprintSources = map[string]browserFingerprintProfile{
	sourceHanime: {
		label:      "Chrome 150 Windows",
		userAgent:  hanimeUserAgent,
		platform:   "Windows",
		clientHint: `"Chromium";v="150", "Google Chrome";v="150", "Not_A Brand";v="24"`,
	},
}

type huangguoBrowserTransport struct {
	base      http.RoundTripper
	router    *proxyRouter
	host      string
	insecure  bool
	record    func(diagnosticEvent)
	mu        sync.Mutex
	clients   map[string]browserHTTPClient
	newClient func(string) (browserHTTPClient, error)
	// verifications 延迟取站点验证凭据表：transport 可能比 store 先构造，
	// 直接存指针会拿到 nil。
	verifications func() *siteVerificationStore
}

func newHuangguoBrowserTransport(base http.RoundTripper, downloader *Downloader) *huangguoBrowserTransport {
	configured, _ := url.Parse(downloader.providerBaseURL(sourceHuangguoVideo))
	transport := &huangguoBrowserTransport{
		base: base, router: downloader.proxyRouter, host: configured.Host,
		insecure: downloader.cfg.InsecureTLS, record: downloader.recordDiagnostic,
		clients:       make(map[string]browserHTTPClient),
		verifications: func() *siteVerificationStore { return downloader.siteVerifications },
	}
	transport.newClient = transport.createClient
	return transport
}

func (transport *huangguoBrowserTransport) matches(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
		return false
	}
	if strings.EqualFold(request.URL.Host, transport.host) || strings.EqualFold(request.URL.Hostname(), "huangguo.video") {
		return true
	}
	_, registered := transport.profileFor(request)
	return registered
}

// profileFor 返回该请求要伪装的浏览器身份。黄果家族走默认 identity，
// 其他登记在 browserFingerprintSources 的源各用各的 UA。
func (transport *huangguoBrowserTransport) profileFor(request *http.Request) (browserFingerprintProfile, bool) {
	profile, ok := browserFingerprintSources[providerSourceForURL(request.URL.String())]
	return profile, ok
}

// profileName 取诊断日志用的身份标签。
func (transport *huangguoBrowserTransport) profileName(request *http.Request) string {
	if profile, ok := transport.profileFor(request); ok {
		return profile.label
	}
	return huangguoFingerprintProfile.label
}

// uaChromeVersion 从 UA 里取 Chrome 主版本。没有就返回 0。
func uaChromeVersion(userAgent string) int {
	match := uaChromeVersionPattern.FindStringSubmatch(userAgent)
	if len(match) != 2 {
		return 0
	}
	version, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return version
}

var uaChromeVersionPattern = regexp.MustCompile(`Chrome/(\d+)\.`)

// profileForUserAgent 按 UA 推导 Client Hints。
//
// 为什么必须跟着 UA 走：sec-ch-ua-platform 是 UA 的「结构化副本」，浏览器
// 自己保证两者一致。我们一旦手写死 "Windows"/?0，而 UA 里写着 Android，
// Cloudflare 的 Turnstile 立刻能看出这是被改过头的环境——它宁可一直挑战，
// 也不肯下发 cf_clearance。这正是「点很多次也过不了」的其中一个原因。
func profileForUserAgent(base browserFingerprintProfile, userAgent string) browserFingerprintProfile {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" || userAgent == base.userAgent {
		return base
	}
	profile := base
	profile.userAgent = userAgent
	profile.mobile = strings.Contains(userAgent, "Mobile") || strings.Contains(userAgent, "Android")
	switch {
	case strings.Contains(userAgent, "Android"):
		profile.platform = "Android"
	case strings.Contains(userAgent, "iPhone") || strings.Contains(userAgent, "iPad"):
		profile.platform = "iOS"
	case strings.Contains(userAgent, "Macintosh"):
		profile.platform = "macOS"
	case strings.Contains(userAgent, "Windows"):
		profile.platform = "Windows"
	case strings.Contains(userAgent, "X11") || strings.Contains(userAgent, "Linux"):
		profile.platform = "Linux"
	default:
		profile.platform = base.platform
	}
	if version := uaChromeVersion(userAgent); version > 0 {
		profile.clientHint = `"Chromium";v="` + strconv.Itoa(version) +
			`", "Google Chrome";v="` + strconv.Itoa(version) + `", "Not_A Brand";v="24"`
		profile.label = profile.platform + " " + strconv.Itoa(version)
	} else {
		profile.label = profile.platform
	}
	return profile
}

func (transport *huangguoBrowserTransport) createClient(proxy string) (browserHTTPClient, error) {
	idleTimeout := 90 * time.Second
	options := []tlsclient.HttpClientOption{
		tlsclient.WithClientProfile(profiles.Chrome_150),
		tlsclient.WithRandomTLSExtensionOrder(),
		tlsclient.WithDisableHttp3(),
		tlsclient.WithNotFollowRedirects(),
		tlsclient.WithTimeoutSeconds(45),
		tlsclient.WithProxyUrl(proxy),
		tlsclient.WithCookieJar(tlsclient.NewCookieJar()),
		tlsclient.WithTransportOptions(&tlsclient.TransportOptions{
			IdleConnTimeout: &idleTimeout, MaxIdleConns: 8, MaxIdleConnsPerHost: 4,
			MaxResponseHeaderBytes: 1 << 20,
		}),
	}
	if transport.insecure {
		options = append(options, tlsclient.WithInsecureSkipVerify())
	}
	return tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
}

func (transport *huangguoBrowserTransport) client(request *http.Request) (browserHTTPClient, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	proxy, err := transport.router.proxy(request)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if proxy != nil {
		proxyURL = proxy.String()
	}
	key := request.URL.Scheme + "://" + strings.ToLower(request.URL.Host) + "|" + proxyURL
	if client := transport.clients[key]; client != nil {
		return client, nil
	}
	client, err := transport.newClient(proxyURL)
	if err != nil {
		return nil, err
	}
	if len(transport.clients) >= 8 {
		for key, old := range transport.clients {
			old.CloseIdleConnections()
			delete(transport.clients, key)
			break
		}
	}
	transport.clients[key] = client
	return client, nil
}

func huangguoBrowserHeaders(request *http.Request) fhttp.Header {
	return browserHeaders(request, huangguoFingerprintProfile)
}

// browserHeaders 生成一套完整的 Chrome 导航请求头。
//
// HeaderOrderKey 必须保留：h2 的伪头/普通头顺序本身是指纹的一部分，
// 让 fhttp 按固定顺序写出，才和真浏览器一致。
//
// 注意 UA / sec-ch-ua 来自 profile 而不是 request：验证页、HTTP 客户端、
// 挑战下发三方必须共用同一个 UA 字符串，否则 cf_clearance 绑不上。
func browserHeaders(request *http.Request, profile browserFingerprintProfile) fhttp.Header {
	if profile.userAgent == "" {
		profile = huangguoFingerprintProfile
	}
	mobile := "?0"
	if profile.mobile {
		mobile = "?1"
	}
	headers := fhttp.Header{
		"sec-ch-ua":                 {profile.clientHint},
		"sec-ch-ua-mobile":          {mobile},
		"sec-ch-ua-platform":        {`"` + profile.platform + `"`},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {profile.userAgent},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
		"sec-fetch-site":            {"same-origin"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-language":           {"zh-CN,zh;q=0.9"},
		fhttp.HeaderOrderKey:        {"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "upgrade-insecure-requests", "user-agent", "accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest", "referer", "accept-encoding", "accept-language", "cookie"},
	}
	if mode := request.Header.Get("Sec-Fetch-Mode"); mode != "" && mode != "navigate" {
		headers["accept"] = []string{firstNonEmpty(request.Header.Get("Accept"), "*/*")}
		headers["sec-fetch-mode"] = []string{mode}
		headers["sec-fetch-dest"] = []string{firstNonEmpty(request.Header.Get("Sec-Fetch-Dest"), "empty")}
		delete(headers, "upgrade-insecure-requests")
		delete(headers, "sec-fetch-user")
	}
	for key, values := range request.Header {
		lower := strings.ToLower(key)
		if _, fixed := headers[lower]; fixed || lower == "host" || lower == "connection" {
			continue
		}
		headers[lower] = append([]string(nil), values...)
	}
	if referer := request.Header.Get("Referer"); referer == "" {
		headers["sec-fetch-site"] = []string{"none"}
	} else if parsed, err := url.Parse(referer); err == nil && !strings.EqualFold(parsed.Host, request.URL.Host) {
		headers["sec-fetch-site"] = []string{"cross-site"}
	}
	return headers
}

func standardBrowserHeaders(headers fhttp.Header) http.Header {
	result := make(http.Header, len(headers))
	for key, values := range headers {
		result[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return result
}

func (transport *huangguoBrowserTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if !transport.matches(request) {
		return transport.base.RoundTrip(request)
	}
	client, err := transport.client(request)
	if err != nil {
		return nil, fmt.Errorf("黄果浏览器客户端初始化失败：%w", publicError(err))
	}
	upstream, err := fhttp.NewRequestWithContext(request.Context(), request.Method, request.URL.String(), request.Body)
	if err != nil {
		return nil, err
	}
	profile := huangguoFingerprintProfile
	if registered, ok := transport.profileFor(request); ok {
		profile = registered
	}
	// 只有「站点验证记录」里的 UA 才该改写身份：那是用户在浏览器里过挑战
	// 时用的那一串，cf_clearance 绑的就是它。
	//
	// 不能直接拿 request.Header 的 UA：fetchProviderText 会给所有源默认塞
	// 一个 iPhone Safari 的 UA，跟着它走会把黄果这类已经跑通的链路也一起
	// 改成移动端身份。
	if transport.verifications != nil {
		if store := transport.verifications(); store != nil {
			if source := providerSourceForURL(request.URL.String()); source != "" {
				if entry, found := store.lookup(source, request.URL.Hostname()); found && entry.UserAgent != "" {
					profile = profileForUserAgent(profile, entry.UserAgent)
				}
			}
		}
	}
	upstream.Header = browserHeaders(request, profile)
	upstream.Host = request.Host
	upstream.ContentLength = request.ContentLength
	upstream.GetBody = request.GetBody
	response, err := client.Do(upstream)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, publicError(err)
	}
	result := &http.Response{
		Status: response.Status, StatusCode: response.StatusCode,
		Proto: response.Proto, ProtoMajor: response.ProtoMajor, ProtoMinor: response.ProtoMinor,
		Header: standardBrowserHeaders(response.Header), Body: response.Body,
		ContentLength: response.ContentLength, TransferEncoding: response.TransferEncoding,
		Close: response.Close, Uncompressed: response.Uncompressed,
		Trailer: standardBrowserHeaders(response.Trailer), Request: request,
	}
	if state := response.TLS; state != nil {
		result.TLS = &tls.ConnectionState{Version: state.Version, HandshakeComplete: state.HandshakeComplete,
			DidResume: state.DidResume, CipherSuite: state.CipherSuite, NegotiatedProtocol: state.NegotiatedProtocol,
			ServerName: state.ServerName, PeerCertificates: state.PeerCertificates, VerifiedChains: state.VerifiedChains}
	}
	if transport.record != nil {
		level := "info"
		if result.StatusCode >= 400 || strings.EqualFold(result.Header.Get("Cf-Mitigated"), "challenge") {
			level = "warning"
		}
		source := providerSourceForURL(request.URL.String())
		if source == "" {
			source = sourceHuangguoVideo
		}
		transport.record(diagnosticEvent{Event: "network.browser_request", Level: level, Source: source,
			Host: request.URL.Hostname(), HTTPStatus: result.StatusCode, Client: transport.profileName(request),
			Protocol: result.Proto, CFRay: truncate(result.Header.Get("Cf-Ray"), 128),
			ResponseType: truncate(result.Header.Get("Content-Type"), 128), Message: "浏览器指纹请求完成"})
	}
	return result, nil
}

func (transport *huangguoBrowserTransport) CloseIdleConnections() {
	transport.mu.Lock()
	for key, client := range transport.clients {
		client.CloseIdleConnections()
		delete(transport.clients, key)
	}
	transport.mu.Unlock()
	if closer, ok := transport.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
