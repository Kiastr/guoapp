package core

// 红果官网扫码登录。
//
// 用途：把「账号态」接进来——收藏、观看历史、云端续播这类账号接口需要它。
// **取流不依赖登录**：播放走匿名设备身份（provider_hongguo_anon_device.go），
// 登录不会解锁更多剧，也不改变画质分级。这一点必须说清楚，否则用户会以为
// 「登录了就能看 VIP」。
//
// 链路（红果官网 passport web 端点，网页端应用号 aid=513755）：
//
//	GET /passport/web/get_qrcode/?next=…      → token + 二维码 PNG（data URI）
//	GET /passport/web/check_qrconnect/?token=… → status: new / confirmed / expired
//	confirmed 时跟随 redirect_url 完成服务端会话建立，Set-Cookie 落 sessionid
//	GET /passport/account/info/v2/            → user_id_str / screen_name
//
// 三个容易踩的点：
//
//  1. **aid 必须是 513755**。App 用 8662、番茄小说用 1967，这两个在 web
//     passport 上都是「该应用无权限」（error_code 16）——web 侧的应用号是
//     按网页产品单独登记的，不能跟着 App 的 aid 走。改这个常量前先实测。
//  2. **中间 cookie 必须跨请求保持**。get_qrcode 会下发 passport_csrf_token，
//     check_qrconnect 要带上它（cookie 与 x-tt-passport-csrf-token 头双提交），
//     confirmed 之后的回跳还要继续用。所以扫码会话是一个带 cookiejar 的
//     独立 client，以 token 为键暂存在内存里。
//  3. **redirect_url 是远端可控输入**。带着 cookie 去 GET 它之前必须校验
//     与登录入口同 scheme + 同 host，否则就是「任意 URL + 凭据」的 SSRF。
//     跟随也必须逐跳校验——Go 默认的自动重定向会把这个校验绕过去。
//
// 只做扫码，不做短信：短信要走 App passport（设备注册 + 请求签名 + 滑块
// 风控），链路长且易变；扫码走网页 passport，只要浏览器 UA 与 cookie 即可
// 完成，是当前更稳的一条路。要补短信登录，另行排期。

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// hongguoLoginBase 登录入口主机（红果官网）。
	hongguoLoginBase = "https://hongguoduanju.com"
	// hongguoLoginAid 官网网页端的应用号（不是 App 的 8662）。
	hongguoLoginAid = "513755"
	// hongguoLoginSDK passport-jssdk 版本，官网前端当前版本。
	hongguoLoginSDK = "3.4.10"
	// hongguoLoginUA 浏览器 UA。web 端接口与 App 端 UA 严格分开，
	// 混用会被判成「不支持 web passport 的客户端」。
	hongguoLoginUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	// hongguoLoginFile 登录态落盘文件名（放在应用数据目录下）。
	hongguoLoginFile = "hongguo-login.json"

	// hongguoLoginMaxQR 二维码 base64 长度上限（实测 29KB，留 3 倍余量）。
	hongguoLoginMaxQR = 96 << 10
	// hongguoLoginPendingMax 同时在内存里的扫码会话上限。
	hongguoLoginPendingMax = 8
	// hongguoLoginPendingTTL 扫码中间态存活时长（服务端二维码约 3~10 分钟）。
	hongguoLoginPendingTTL = 10 * time.Minute
	// hongguoLoginHops 回跳跟随上限（真实链路 1~2 跳）。
	hongguoLoginHops = 5
	// hongguoLoginTimeout 单次 HTTP 请求超时。
	hongguoLoginTimeout = 20 * time.Second
)

// hongguoLoginTicket 一次扫码的中间态：token → 已下发的 cookie + 创建时间。
type hongguoLoginTicket struct {
	cookies   []*http.Cookie
	createdAt time.Time
}

// hongguoLoginRuntime 扫码会话池（挂在 engine 上，随进程生命周期）。
type hongguoLoginRuntime struct {
	mu      sync.Mutex
	pending map[string]*hongguoLoginTicket
}

// hongguoLoginSession 落盘的登录态。cookie 只存官网域的 name=value，
// 不含任何设备指纹——登录态与设备身份是两套东西，不要混进同一个文件。
type hongguoLoginSession struct {
	Cookies   map[string]string `json:"cookies"`
	UserID    string            `json:"userId,omitempty"`
	Nickname  string            `json:"nickname,omitempty"`
	UpdatedAt int64             `json:"updatedAt,omitempty"`
}

// hongguoLoginView 返回给客户端的登录态视图（不含 cookie 明文）。
type hongguoLoginView struct {
	SignedIn  bool   `json:"signedIn"`
	UserID    string `json:"userId,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

// hongguoLoginQRCode 创建扫码会话的结果。
type hongguoLoginQRCode struct {
	Token     string `json:"token"`
	Image     string `json:"image"`     // PNG 的纯 base64（客户端直接 base64Decode）
	ExpiresIn int64  `json:"expiresIn"` // 剩余有效秒数；0 = 服务端未给
}

// hongguoLoginPollResult 单次轮询结果。Status 语义照搬服务端：
// new=待扫描；confirmed=已确认（本次调用内已完成落盘）；expired=过期/失效。
type hongguoLoginPollResult struct {
	Status      string            `json:"status"`
	Description string            `json:"description,omitempty"`
	Session     *hongguoLoginView `json:"session,omitempty"`
}

// hongguoLoginStore 取（惰性初始化）扫码会话池。
func (engine *nativeEngine) hongguoLoginStore() *hongguoLoginRuntime {
	engine.loginMu.Lock()
	defer engine.loginMu.Unlock()
	if engine.login == nil {
		engine.login = &hongguoLoginRuntime{pending: map[string]*hongguoLoginTicket{}}
	}
	return engine.login
}

// hongguoLoginParams 组装公共查询参数。
//
// is_from_ttaccountsdk=1 是官网前端的固定标记：缺它时服务端会退化成
// 「旧版 web 客户端」分支，部分字段（expire_time、copywriting）不再下发。
func hongguoLoginParams(extra url.Values) url.Values {
	params := url.Values{}
	params.Set("aid", hongguoLoginAid)
	params.Set("language", "zh")
	params.Set("account_sdk_source", "web")
	params.Set("passport_jssdk_version", hongguoLoginSDK)
	params.Set("passport_jssdk_type", "normal")
	params.Set("is_from_ttaccountsdk", "1")
	for key, values := range extra {
		for _, value := range values {
			params.Set(key, value)
		}
	}
	return params
}

// hongguoLoginCookieValue 从 jar 里按名取一个 cookie 值。
func hongguoLoginCookieValue(jar http.CookieJar, base, name string) string {
	if jar == nil {
		return ""
	}
	target, err := url.Parse(base)
	if err != nil {
		return ""
	}
	for _, cookie := range jar.Cookies(target) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

// hongguoLoginRequest 发一次带浏览器特征的 GET，返回 data 节点。
//
// CSRF 双提交：cookie 里的 passport_csrf_token 同步进 x-tt-passport-csrf-token。
// 未扫码阶段服务端不校验，但 confirmed 之后的账号接口要校验，提前带上省一次
// 「扫码成功却拿不到昵称」的排查。
func hongguoLoginRequest(client *http.Client, base, path string, params url.Values) (map[string]any, error) {
	request, err := http.NewRequest(http.MethodGet, base+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", hongguoLoginUA)
	request.Header.Set("Referer", base+"/")
	request.Header.Set("Origin", base)
	request.Header.Set("Accept", "application/json, text/javascript")
	if token := hongguoLoginCookieValue(client.Jar, base, "passport_csrf_token"); token != "" {
		request.Header.Set("x-tt-passport-csrf-token", token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("红果官网登录接口返回 HTTP %d", response.StatusCode)
	}
	payload := map[string]any{}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.UseNumber() // token / expire_time 是大整数，别走 float64
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("红果官网登录接口返回无法解析")
	}
	if message := mapString(payload, "message"); message != "" && message != "success" {
		data, _ := payload["data"].(map[string]any)
		detail := mapString(data, "description")
		if detail == "" {
			detail = message
		}
		return nil, errors.New("红果官网登录：" + detail)
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	if code := mapString(data, "error_code"); code != "" && code != "0" {
		detail := mapString(data, "description")
		if detail == "" {
			detail = "服务端拒绝（错误码 " + code + "）"
		}
		return nil, errors.New("红果官网登录：" + detail)
	}
	return data, nil
}

// hongguoLoginJarSnapshot 导出 jar 里指定主机的全部 cookie。
func hongguoLoginJarSnapshot(jar http.CookieJar, base string) []*http.Cookie {
	if jar == nil {
		return nil
	}
	target, err := url.Parse(base)
	if err != nil {
		return nil
	}
	return jar.Cookies(target)
}

// hongguoLoginClient 造一个带 cookie jar 的独立 client。
//
// 不复用 Downloader 的 transport：那是给取流用的（带限速器、代理路由、
// 站源 UA 覆盖），登录链路要的是干净的浏览器身份，混在一起会把 Referer、
// UA 覆盖成站源那套，直接触发风控。
func hongguoLoginClient(jar http.CookieJar) *http.Client {
	return &http.Client{Timeout: hongguoLoginTimeout, Jar: jar}
}

// hongguoLoginStripDataURI 把 data URI 形式剥成纯 base64。
// 官网给的是 `data:image/png;base64,iVBOR…`，也可能直接给裸 base64。
func hongguoLoginStripDataURI(value string) string {
	if !strings.HasPrefix(value, "data:") {
		return value
	}
	const marker = "base64,"
	index := strings.Index(value, marker)
	if index < 0 {
		return ""
	}
	return value[index+len(marker):]
}

// hongguoLoginSafeToken token 只允许安全字符：它要进 map 键，也要拼进
// 查询串，脏字符既能撑内存也能改语义。
func hongguoLoginSafeToken(token string) bool {
	if token == "" || len(token) > 128 {
		return false
	}
	for _, r := range token {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// hongguoLoginExpireSeconds 把服务端的 expire_time 归一成「剩余秒数」。
//
// 服务端给的是 **Unix 秒级时间戳**（实测 1793378585），不是倒计时；
// 但也见过小数值形态（直接给秒数），两种都兼容。
func hongguoLoginExpireSeconds(value any) int64 {
	var raw int64
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			raw = parsed
		}
	case float64:
		raw = int64(typed)
	case string:
		if parsed, err := json.Number(strings.TrimSpace(typed)).Int64(); err == nil {
			raw = parsed
		}
	}
	if raw <= 0 {
		return 0
	}
	if raw > 1_000_000_000 { // Unix 时间戳
		raw -= time.Now().Unix()
	}
	if raw < 0 {
		return 0
	}
	if raw > 600 {
		return 600
	}
	return raw
}

// hongguoLoginPrune 清理过期中间态（调用方持锁）。
func (runtime *hongguoLoginRuntime) pruneLocked(now time.Time) {
	for token, ticket := range runtime.pending {
		if now.Sub(ticket.createdAt) > hongguoLoginPendingTTL {
			delete(runtime.pending, token)
		}
	}
}

// Create 创建扫码会话：拿 token + 二维码，中间 cookie 留在内存。
func (engine *nativeEngine) hongguoLoginCreate(ctx context.Context) (hongguoLoginQRCode, error) {
	return engine.hongguoLoginCreateAt(ctx, hongguoLoginBase)
}

// hongguoLoginCreateAt 同 Create，base 可注入（测试用 httptest 替换主机）。
func (engine *nativeEngine) hongguoLoginCreateAt(ctx context.Context, base string) (hongguoLoginQRCode, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return hongguoLoginQRCode{}, err
	}
	client := hongguoLoginClient(jar)

	extra := url.Values{}
	extra.Set("next", base+"/")
	extra.Set("need_logo", "true")
	data, err := hongguoLoginRequest(client, base, "/passport/web/get_qrcode/", hongguoLoginParams(extra))
	if err != nil {
		return hongguoLoginQRCode{}, err
	}
	if err := ctx.Err(); err != nil {
		return hongguoLoginQRCode{}, err
	}

	token := mapString(data, "token")
	if !hongguoLoginSafeToken(token) {
		return hongguoLoginQRCode{}, errors.New("获取二维码失败：服务端未返回有效令牌")
	}
	image := hongguoLoginStripDataURI(mapString(data, "qrcode"))
	if image == "" {
		return hongguoLoginQRCode{}, errors.New("获取二维码失败：服务端未返回二维码")
	}
	if len(image) > hongguoLoginMaxQR {
		return hongguoLoginQRCode{}, errors.New("获取二维码失败：二维码数据异常")
	}
	if _, err := base64.StdEncoding.DecodeString(image); err != nil {
		return hongguoLoginQRCode{}, errors.New("获取二维码失败：二维码数据无法解码")
	}

	store := engine.hongguoLoginStore()
	store.mu.Lock()
	now := time.Now()
	store.pruneLocked(now)
	if len(store.pending) >= hongguoLoginPendingMax {
		store.mu.Unlock()
		return hongguoLoginQRCode{}, errors.New("扫码会话过多，请稍后再试")
	}
	store.pending[token] = &hongguoLoginTicket{
		cookies:   hongguoLoginJarSnapshot(jar, base),
		createdAt: now,
	}
	store.mu.Unlock()

	return hongguoLoginQRCode{
		Token:     token,
		Image:     image,
		ExpiresIn: hongguoLoginExpireSeconds(data["expire_time"]),
	}, nil
}

// Poll 轮询一次扫码状态。confirmed 时在本函数内完成回跳 + 落盘。
func (engine *nativeEngine) hongguoLoginPoll(ctx context.Context, token string) (hongguoLoginPollResult, error) {
	return engine.hongguoLoginPollAt(ctx, hongguoLoginBase, token)
}

// hongguoLoginPollAt 同 Poll，base 可注入。
func (engine *nativeEngine) hongguoLoginPollAt(ctx context.Context, base, token string) (hongguoLoginPollResult, error) {
	if !hongguoLoginSafeToken(token) {
		return hongguoLoginPollResult{Status: "expired"}, nil
	}
	store := engine.hongguoLoginStore()
	store.mu.Lock()
	ticket := store.pending[token]
	store.mu.Unlock()
	if ticket == nil {
		return hongguoLoginPollResult{Status: "expired"}, nil
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return hongguoLoginPollResult{}, err
	}
	// 还原 get_qrcode 阶段下发的中间 cookie（passport_csrf_token 等）。
	target, err := url.Parse(base)
	if err != nil {
		return hongguoLoginPollResult{}, err
	}
	for _, cookie := range ticket.cookies {
		jar.SetCookies(target, []*http.Cookie{{
			Name: cookie.Name, Value: cookie.Value, Path: "/",
		}})
	}
	client := hongguoLoginClient(jar)

	extra := url.Values{}
	extra.Set("next", "/")
	extra.Set("token", token)
	data, err := hongguoLoginRequest(client, base, "/passport/web/check_qrconnect/", hongguoLoginParams(extra))
	if err != nil {
		// 明确的服务端拒绝（过期/被取消）按失效处理，不向上抛错误。
		engine.hongguoLoginForget(token)
		return hongguoLoginPollResult{Status: "expired"}, nil
	}
	if err := ctx.Err(); err != nil {
		return hongguoLoginPollResult{}, err
	}

	status := mapString(data, "status")
	if status == "" {
		status = "new"
	}
	// 过期语义：服务端用 "5"，也见过字面 "expired"。
	if status == "5" || status == "expired" {
		engine.hongguoLoginForget(token)
		return hongguoLoginPollResult{Status: "expired"}, nil
	}
	redirect := mapString(data, "redirect_url")
	// 确认语义：服务端用 "3"，也见过 "confirmed"/"complete"/"success"；
	// 只要带了 redirect_url 就按已确认处理（不同版本的字段不完全一致）。
	confirmed := status == "3" || status == "confirmed" ||
		status == "complete" || status == "success" || redirect != ""
	if !confirmed {
		return hongguoLoginPollResult{Status: status, Description: mapString(data, "description")}, nil
	}

	if redirect != "" {
		if err := hongguoLoginFollow(client, base, redirect); err != nil {
			return hongguoLoginPollResult{}, err
		}
	}
	cookies := map[string]string{}
	for _, cookie := range hongguoLoginJarSnapshot(jar, base) {
		if cookie.Value != "" {
			cookies[cookie.Name] = cookie.Value
		}
	}
	if !hongguoLoginHasSession(cookies) {
		engine.hongguoLoginForget(token)
		return hongguoLoginPollResult{}, errors.New("扫码确认失败：服务端未下发登录凭证")
	}

	session := hongguoLoginSession{Cookies: cookies, UpdatedAt: time.Now().Unix()}
	// 昵称/uid 是加分项：拿不到不影响登录态落盘（有人账号就是没设昵称）。
	if profile, err := hongguoLoginProfile(client, base); err == nil {
		session.UserID = profile.UserID
		session.Nickname = profile.Nickname
	}
	if err := engine.hongguoLoginSave(session); err != nil {
		return hongguoLoginPollResult{}, err
	}
	engine.hongguoLoginForget(token)
	view := hongguoLoginView{
		SignedIn: true, UserID: session.UserID,
		Nickname: session.Nickname, UpdatedAt: session.UpdatedAt,
	}
	return hongguoLoginPollResult{Status: "confirmed", Session: &view}, nil
}

// hongguoLoginFollow 逐跳跟随回跳地址，把沿途 Set-Cookie 收进 jar。
//
// 两道保险，缺一不可：
//  1. 只跟与登录入口同 scheme + 同 host 的地址（redirect_url 来自响应体，
//     是远端可控输入，带 cookie 去 GET 任意地址 = 把凭据交出去）。
//  2. 关掉 http.Client 的自动重定向，由这里逐跳校验后再走——否则自动跟随
//     会绕过第 1 条（Go 默认最多 10 跳且不限目标域）。
func hongguoLoginFollow(client *http.Client, base, redirect string) error {
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Host == "" {
		return errors.New("扫码确认失败：登录入口地址无效")
	}
	target, err := baseURL.Parse(redirect)
	if err != nil {
		return errors.New("扫码确认失败：回跳地址无效")
	}
	// 浅拷贝：借原 client 的 Jar/Timeout，只改重定向策略（原 client 还要
	// 继续用于 check/account 接口，不能就地改）。
	scoped := *client
	scoped.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	current := target
	for hop := 0; hop < hongguoLoginHops; hop++ {
		if current.Scheme != baseURL.Scheme || current.Host != baseURL.Host {
			return errors.New("扫码确认失败：回跳地址不属于红果官网")
		}
		request, err := http.NewRequest(http.MethodGet, current.String(), nil)
		if err != nil {
			return errors.New("扫码确认失败：回跳地址无效")
		}
		request.Header.Set("User-Agent", hongguoLoginUA)
		request.Header.Set("Referer", base+"/")
		response, err := scoped.Do(request)
		if err != nil {
			return err
		}
		next := ""
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			next = response.Header.Get("Location")
		}
		status := response.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		response.Body.Close()
		if next == "" {
			if status >= 400 {
				return fmt.Errorf("扫码确认失败（HTTP %d）", status)
			}
			return nil
		}
		resolved, err := current.Parse(next)
		if err != nil {
			return errors.New("扫码确认失败：回跳地址无效")
		}
		current = resolved
	}
	return errors.New("扫码确认失败：回跳次数过多")
}

// hongguoLoginHasSession 判断 cookie 里有没有真正的登录态。
// sessionid 是主会话，sessionid_ss 是它的同域副本，两个有一个就算登录。
func hongguoLoginHasSession(cookies map[string]string) bool {
	for _, name := range []string{"sessionid", "sessionid_ss"} {
		if strings.TrimSpace(cookies[name]) != "" {
			return true
		}
	}
	return false
}

// hongguoLoginProfile 拉账号信息。
func hongguoLoginProfile(client *http.Client, base string) (hongguoLoginView, error) {
	data, err := hongguoLoginRequest(client, base, "/passport/account/info/v2/", hongguoLoginParams(nil))
	if err != nil {
		return hongguoLoginView{}, err
	}
	return hongguoLoginView{
		SignedIn: true,
		UserID:   mapString(data, "user_id_str", "user_id"),
		Nickname: mapString(data, "screen_name", "nickname", "name"),
	}, nil
}

// Forget 清理指定 token 的中间态。
func (engine *nativeEngine) hongguoLoginForget(token string) {
	store := engine.hongguoLoginStore()
	store.mu.Lock()
	delete(store.pending, token)
	store.mu.Unlock()
}

// hongguoLoginPath 登录态文件路径。
func (engine *nativeEngine) hongguoLoginPath() string {
	return filepath.Join(engine.directory, hongguoLoginFile)
}

// hongguoLoginLoad 读登录态；不存在或格式坏了都返回空结构（不报错——
// 一个坏文件不该让设置页打不开）。
func (engine *nativeEngine) hongguoLoginLoad() hongguoLoginSession {
	raw, err := os.ReadFile(engine.hongguoLoginPath())
	if err != nil {
		return hongguoLoginSession{}
	}
	session := hongguoLoginSession{}
	if err := json.Unmarshal(raw, &session); err != nil {
		return hongguoLoginSession{}
	}
	return session
}

// hongguoLoginSave 原子写登录态，权限 0600（里面是会话凭据）。
func (engine *nativeEngine) hongguoLoginSave(session hongguoLoginSession) error {
	body, err := json.Marshal(session)
	if err != nil {
		return err
	}
	if err := writeNativeCacheFile(engine.hongguoLoginPath(), body); err != nil {
		return errors.New("保存登录态失败：" + err.Error())
	}
	return nil
}

// hongguoLoginStatus 读本地登录态（不发网络请求）。
func (engine *nativeEngine) hongguoLoginStatus() hongguoLoginView {
	session := engine.hongguoLoginLoad()
	if !hongguoLoginHasSession(session.Cookies) {
		return hongguoLoginView{}
	}
	return hongguoLoginView{
		SignedIn:  true,
		UserID:    session.UserID,
		Nickname:  session.Nickname,
		UpdatedAt: session.UpdatedAt,
	}
}

// hongguoLoginVerify 拿本地 cookie 打一次账号接口，确认会话还有效并刷新昵称。
//
// 会话失效（服务端 401/未登录）时清掉本地文件，返回 signedIn=false——
// 界面据此提示「登录已过期」，而不是继续显示一个假在线状态。
func (engine *nativeEngine) hongguoLoginVerify(ctx context.Context) (hongguoLoginView, error) {
	return engine.hongguoLoginVerifyAt(ctx, hongguoLoginBase)
}

// hongguoLoginVerifyAt 同 Verify，base 可注入。
func (engine *nativeEngine) hongguoLoginVerifyAt(ctx context.Context, base string) (hongguoLoginView, error) {
	session := engine.hongguoLoginLoad()
	if !hongguoLoginHasSession(session.Cookies) {
		return hongguoLoginView{}, nil
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return hongguoLoginView{}, err
	}
	target, err := url.Parse(base)
	if err != nil {
		return hongguoLoginView{}, err
	}
	for name, value := range session.Cookies {
		jar.SetCookies(target, []*http.Cookie{{Name: name, Value: value, Path: "/"}})
	}
	client := hongguoLoginClient(jar)
	if err := ctx.Err(); err != nil {
		return hongguoLoginView{}, err
	}
	profile, err := hongguoLoginProfile(client, base)
	if err != nil {
		// 网络抖动不该判定为失效：只有服务端明确说没登录才清。
		if strings.Contains(err.Error(), "未登录") || strings.Contains(err.Error(), "no login") {
			_ = os.Remove(engine.hongguoLoginPath())
			return hongguoLoginView{}, nil
		}
		return engine.hongguoLoginStatus(), nil
	}
	session.UserID = profile.UserID
	session.Nickname = profile.Nickname
	session.UpdatedAt = time.Now().Unix()
	if err := engine.hongguoLoginSave(session); err != nil {
		return hongguoLoginView{}, err
	}
	return hongguoLoginView{
		SignedIn: true, UserID: session.UserID,
		Nickname: session.Nickname, UpdatedAt: session.UpdatedAt,
	}, nil
}

// hongguoLoginLogout 退出登录：删本地文件 + 清内存中间态。
//
// 只清本地，不调服务端登出接口——官网登出要带完整 CSRF 链路，而本地
// 清了之后那份 sessionid 已经不会再用，收益不抵风险。
func (engine *nativeEngine) hongguoLoginLogout() error {
	err := os.Remove(engine.hongguoLoginPath())
	if err != nil && !os.IsNotExist(err) {
		return errors.New("退出登录失败：" + err.Error())
	}
	store := engine.hongguoLoginStore()
	store.mu.Lock()
	store.pending = map[string]*hongguoLoginTicket{}
	store.mu.Unlock()
	return nil
}
