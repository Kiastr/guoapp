package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// 1x1 透明 PNG，用来当合法二维码载荷。
const hongguoLoginTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestHongguoLoginStripDataURI(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"data URI", "data:image/png;base64," + hongguoLoginTestPNG, hongguoLoginTestPNG},
		{"裸 base64", hongguoLoginTestPNG, hongguoLoginTestPNG},
		{"data URI 无 base64 标记", "data:image/png;charset=utf-8,xx", ""},
		{"空串", "", ""},
	}
	for _, c := range cases {
		if got := hongguoLoginStripDataURI(c.in); got != c.want {
			t.Errorf("%s: 得到 %q，期望 %q", c.name, got, c.want)
		}
	}
}

func TestHongguoLoginSafeToken(t *testing.T) {
	valid := []string{"bae32c066282e628ee57", "a-b_C9", strings.Repeat("a", 128)}
	for _, token := range valid {
		if !hongguoLoginSafeToken(token) {
			t.Errorf("合法 token 被判非法：%q", token)
		}
	}
	invalid := []string{"", strings.Repeat("a", 129), "a b", "a/b", "../../etc/passwd", "token&token=1"}
	for _, token := range invalid {
		if hongguoLoginSafeToken(token) {
			t.Errorf("非法 token 被判合法：%q", token)
		}
	}
}

func TestHongguoLoginExpireSeconds(t *testing.T) {
	now := json.Number("0")
	_ = now
	// 时间戳形态：服务端给的是 Unix 秒（远大于当前秒数才成立，这里造未来值）。
	future := json.Number("9999999999")
	if got := hongguoLoginExpireSeconds(future); got != 600 {
		t.Errorf("远期时间戳应被夹到 600 秒，得到 %d", got)
	}
	// 倒计时形态：直接给秒数。
	if got := hongguoLoginExpireSeconds(json.Number("180")); got != 180 {
		t.Errorf("180 秒倒计时应原样返回，得到 %d", got)
	}
	// 边界与异常。
	if got := hongguoLoginExpireSeconds(nil); got != 0 {
		t.Errorf("nil 应返回 0，得到 %d", got)
	}
	if got := hongguoLoginExpireSeconds("abc"); got != 0 {
		t.Errorf("脏字符串应返回 0，得到 %d", got)
	}
	if got := hongguoLoginExpireSeconds(json.Number("999")); got != 600 {
		t.Errorf("超过 600 秒应被夹到 600，得到 %d", got)
	}
}

func TestHongguoLoginHasSession(t *testing.T) {
	if !hongguoLoginHasSession(map[string]string{"sessionid": "abc"}) {
		t.Error("sessionid 应判定为已登录")
	}
	if !hongguoLoginHasSession(map[string]string{"sessionid_ss": "abc"}) {
		t.Error("sessionid_ss 应判定为已登录")
	}
	if hongguoLoginHasSession(map[string]string{"sessionid": "  "}) {
		t.Error("空白 sessionid 不该判定为已登录")
	}
	if hongguoLoginHasSession(nil) {
		t.Error("空 cookie 表不该判定为已登录")
	}
}

// 回跳白名单：redirect_url 来自响应体，是远端可控输入。跨主机 / 跨端口的
// 地址必须在发出任何请求之前就被拒——否则等于「拿着 cookie 去 GET 任意 URL」。
func TestHongguoLoginFollowRejectsForeignHost(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 另一台服务器 = 不同端口，用来验证「同 scheme 但不同 host:port」也被拒。
	var foreignHits int32
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&foreignHits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer foreign.Close()

	client := hongguoLoginClient(nil)
	cases := []string{
		"https://evil.example.com/steal", // 跨主机 + 跨 scheme
		"//evil.example.com/steal",       // 协议相对写法
		foreign.URL + "/steal",           // 同 scheme、不同 host:port
		"javascript:alert(1)",            // 非法 scheme
	}
	for _, redirect := range cases {
		if err := hongguoLoginFollow(client, server.URL, redirect); err == nil {
			t.Errorf("跨站回跳应被拒绝：%s", redirect)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("被拒的回跳不该打到本机服务，实际发了 %d 次", got)
	}
	if got := atomic.LoadInt32(&foreignHits); got != 0 {
		t.Errorf("被拒的回跳不该打到外部服务，实际发了 %d 次", got)
	}

	// 同主机相对路径放行。
	if err := hongguoLoginFollow(client, server.URL, "/passport/ok"); err != nil {
		t.Errorf("同主机回跳应放行：%v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("同主机回跳应发出 1 次请求，实际 %d 次", got)
	}
}

// 完整链路：get_qrcode → poll(new) → poll(confirmed) → 落盘 → status → logout。
// 重点验两件容易坏的事：中间 cookie 有没有跨请求带上、confirmed 后有没有落盘。
func TestHongguoLoginFullFlow(t *testing.T) {
	var pollCount int32
	var csrfSeenOnPoll int32
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/passport/web/get_qrcode/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("aid"); got != hongguoLoginAid {
			t.Errorf("get_qrcode 的 aid=%q，期望 %q", got, hongguoLoginAid)
		}
		if got := r.Header.Get("Origin"); got != server.URL {
			t.Errorf("Origin=%q，期望 %q", got, server.URL)
		}
		http.SetCookie(w, &http.Cookie{Name: "passport_csrf_token", Value: "csrf-abc", Path: "/"})
		_, _ = w.Write([]byte(`{"message":"success","data":{"error_code":0,"token":"tok123456","qrcode":"data:image/png;base64,` + hongguoLoginTestPNG + `","expire_time":180}}`))
	})

	mux.HandleFunc("/passport/web/check_qrconnect/", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("passport_csrf_token"); err != nil || cookie.Value != "csrf-abc" {
			t.Errorf("poll 没带上中间 cookie，err=%v", err)
		}
		if r.Header.Get("x-tt-passport-csrf-token") != "csrf-abc" {
			t.Errorf("poll 没带上 CSRF 头")
		}
		atomic.StoreInt32(&csrfSeenOnPoll, 1)
		if atomic.AddInt32(&pollCount, 1) == 1 {
			_, _ = w.Write([]byte(`{"message":"success","data":{"error_code":0,"status":"new"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":"success","data":{"error_code":0,"status":"3","redirect_url":"/passport/login/ok"}}`))
	})

	mux.HandleFunc("/passport/login/ok", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "sess-xyz", Path: "/"})
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/passport/account/info/v2/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"success","data":{"error_code":0,"user_id_str":"42","screen_name":"红果用户"}}`))
	})

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatalf("创建引擎失败: %v", err)
	}
	ctx := context.Background()

	qr, err := engine.hongguoLoginCreateAt(ctx, server.URL)
	if err != nil {
		t.Fatalf("创建扫码会话失败: %v", err)
	}
	if qr.Token != "tok123456" || qr.Image != hongguoLoginTestPNG || qr.ExpiresIn != 180 {
		t.Fatalf("扫码会话字段不对: %+v", qr)
	}

	first, err := engine.hongguoLoginPollAt(ctx, server.URL, qr.Token)
	if err != nil {
		t.Fatalf("首次轮询失败: %v", err)
	}
	if first.Status != "new" {
		t.Fatalf("首次轮询应返回 new，得到 %q", first.Status)
	}
	if atomic.LoadInt32(&csrfSeenOnPoll) != 1 {
		t.Fatal("轮询没沿用 get_qrcode 下发的中间 cookie")
	}

	second, err := engine.hongguoLoginPollAt(ctx, server.URL, qr.Token)
	if err != nil {
		t.Fatalf("二次轮询失败: %v", err)
	}
	if second.Status != "confirmed" {
		t.Fatalf("二次轮询应确认登录，得到 %q", second.Status)
	}
	if second.Session == nil || !second.Session.SignedIn {
		t.Fatalf("确认结果应带回登录态: %+v", second.Session)
	}
	if second.Session.Nickname != "红果用户" || second.Session.UserID != "42" {
		t.Errorf("账号信息没取到: %+v", second.Session)
	}

	// 落盘文件必须存在且权限收紧。
	info, err := os.Stat(engine.hongguoLoginPath())
	if err != nil {
		t.Fatalf("登录态没落盘: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0077 != 0 {
		t.Errorf("登录态文件权限过宽: %v", mode)
	}

	status := engine.hongguoLoginStatus()
	if !status.SignedIn || status.Nickname != "红果用户" {
		t.Fatalf("本地状态读取不对: %+v", status)
	}

	// 同一个 token 再轮询应该失效（中间态已在确认时清掉）。
	third, err := engine.hongguoLoginPollAt(ctx, server.URL, qr.Token)
	if err != nil {
		t.Fatalf("确认后重复轮询不该报错: %v", err)
	}
	if third.Status != "expired" {
		t.Errorf("确认后重复轮询应为 expired，得到 %q", third.Status)
	}

	if err := engine.hongguoLoginLogout(); err != nil {
		t.Fatalf("退出登录失败: %v", err)
	}
	if _, err := os.Stat(engine.hongguoLoginPath()); !os.IsNotExist(err) {
		t.Error("退出登录后文件应被删除")
	}
	if engine.hongguoLoginStatus().SignedIn {
		t.Error("退出后不该再报告已登录")
	}
}

// 服务端说二维码过期（status=5）时应清掉中间态并按 expired 返回，不抛错。
func TestHongguoLoginPollExpired(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/passport/web/get_qrcode/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"success","data":{"error_code":0,"token":"tokexpire","qrcode":"` + hongguoLoginTestPNG + `"}}`))
	})
	mux.HandleFunc("/passport/web/check_qrconnect/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"message":"success","data":{"error_code":0,"status":"5"}}`))
	})

	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatalf("创建引擎失败: %v", err)
	}
	ctx := context.Background()
	qr, err := engine.hongguoLoginCreateAt(ctx, server.URL)
	if err != nil {
		t.Fatalf("创建扫码会话失败: %v", err)
	}
	result, err := engine.hongguoLoginPollAt(ctx, server.URL, qr.Token)
	if err != nil {
		t.Fatalf("过期不该报错: %v", err)
	}
	if result.Status != "expired" {
		t.Errorf("status=5 应归一成 expired，得到 %q", result.Status)
	}
	if _, err := engine.hongguoLoginPollAt(ctx, server.URL, qr.Token); err != nil {
		t.Errorf("过期后重复轮询不该报错: %v", err)
	}
}

// 非法 token 直接按 expired 处理，不打网络。
func TestHongguoLoginPollRejectsDirtyToken(t *testing.T) {
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatalf("创建引擎失败: %v", err)
	}
	result, err := engine.hongguoLoginPollAt(context.Background(), "http://127.0.0.1:1", "../../etc/passwd")
	if err != nil {
		t.Fatalf("脏 token 不该报错: %v", err)
	}
	if result.Status != "expired" {
		t.Errorf("脏 token 应为 expired，得到 %q", result.Status)
	}
}

// 登录态文件损坏时按「未登录」处理，不能让设置页打不开。
func TestHongguoLoginCorruptFile(t *testing.T) {
	directory := t.TempDir()
	engine, err := newNativeEngine(directory)
	if err != nil {
		t.Fatalf("创建引擎失败: %v", err)
	}
	if err := os.WriteFile(engine.hongguoLoginPath(), []byte("{not json"), 0600); err != nil {
		t.Fatalf("写入坏文件失败: %v", err)
	}
	if engine.hongguoLoginStatus().SignedIn {
		t.Error("坏文件不该被读成已登录")
	}
	if err := engine.hongguoLoginLogout(); err != nil {
		t.Errorf("坏文件下退出登录不该报错: %v", err)
	}
}
