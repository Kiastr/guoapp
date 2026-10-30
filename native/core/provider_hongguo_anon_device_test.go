package core

// 匿名身份池的单测：加密结构、响应解析、落盘复用、失效重注册、空 body 换号。
//
// 全部用 httptest 夹具，不请求真实端点（真实链路见 provider_hongguo_live_test.go 的
// TestLiveHongguoPublicRecovery，需 CHECK_LIVE_PROVIDERS=true）。

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

// resetHongguoAnonPool 清空包级身份缓存，避免测试互相污染。
func resetHongguoAnonPool() {
	hongguoAnonPool.mu.Lock()
	hongguoAnonPool.device = hongguoAnonDevice{}
	hongguoAnonPool.loaded = false
	hongguoAnonPool.mu.Unlock()
}

// openHongguoAnonSealed 按协议反向解开注册请求体（仅供测试校验）。
func openHongguoAnonSealed(t *testing.T, sealed []byte) []byte {
	t.Helper()
	if len(sealed) < 6+32+aes.BlockSize || !bytes.Equal(sealed[:6], []byte{0x74, 0x63, 0x05, 0x10, 0x00, 0x00}) {
		t.Fatalf("magic 头不符合协议: % x", sealed[:min(8, len(sealed))])
	}
	random := sealed[6:38]
	seeded := sha512.Sum512(random)
	keyed := sha512.New()
	keyed.Write(seeded[:])
	keyed.Write(hongguoAnonRegisterSalt)
	derived := keyed.Sum(nil)
	block, err := aes.NewCipher(derived[:16])
	if err != nil {
		t.Fatalf("构造 AES: %v", err)
	}
	ciphertext := sealed[38:]
	if len(ciphertext)%aes.BlockSize != 0 {
		t.Fatalf("密文长度不是块整数倍: %d", len(ciphertext))
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, derived[16:32]).CryptBlocks(plain, ciphertext)
	padding := int(plain[len(plain)-1])
	if padding <= 0 || padding > aes.BlockSize || padding > len(plain) {
		t.Fatalf("填充非法: %d", padding)
	}
	plain = plain[:len(plain)-padding]
	if len(plain) < sha512.Size {
		t.Fatalf("解出内容过短: %d", len(plain))
	}
	digest, payload := plain[:sha512.Size], plain[sha512.Size:]
	want := sha512.Sum512(payload)
	if !bytes.Equal(digest, want[:]) {
		t.Fatal("明文前缀的 sha512 与内容不符")
	}
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("gzip 解压失败: %v", err)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip 读取失败: %v", err)
	}
	return raw
}

// TestHongguoAnonSealRegisterBody 校验加密形态与容器内容可完整还原。
func TestHongguoAnonSealRegisterBody(t *testing.T) {
	plain := []byte(`{"magic_tag":"ss_app_log","header":{"aid":8662},"_gen_time":1793350000000}`)
	sealed, err := hongguoAnonSealRegisterBody(plain)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	recovered := openHongguoAnonSealed(t, sealed)
	if !bytes.Equal(recovered, plain) {
		t.Fatalf("解密结果不一致\n want=%s\n got =%s", plain, recovered)
	}
	// 两次加密必须不同（random 每次随机）。
	again, err := hongguoAnonSealRegisterBody(plain)
	if err != nil {
		t.Fatalf("二次加密失败: %v", err)
	}
	if bytes.Equal(sealed, again) {
		t.Fatal("两次加密结果相同，random 未生效")
	}
}

// TestHongguoAnonRegisterBodyStructure 校验注册容器的字段齐备且类型正确。
func TestHongguoAnonRegisterBodyStructure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Content-Type"); got != "application/octet-stream;tt-data=a" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := request.Header.Get("log-encode-type"); got != "gzip" {
			t.Errorf("log-encode-type = %q", got)
		}
		if request.Header.Get("x-ss-stub") == "" {
			t.Error("缺少 x-ss-stub")
		}
		body, _ := io.ReadAll(request.Body)
		raw := openHongguoAnonSealed(t, body)
		var container struct {
			MagicTag string         `json:"magic_tag"`
			Header   map[string]any `json:"header"`
			GenTime  int64          `json:"_gen_time"`
		}
		if err := json.Unmarshal(raw, &container); err != nil {
			t.Fatalf("容器不是 JSON: %v", err)
		}
		if container.MagicTag != "ss_app_log" {
			t.Errorf("magic_tag = %q", container.MagicTag)
		}
		if container.GenTime == 0 {
			t.Error("_gen_time 缺失")
		}
		for _, key := range []string{"device_id", "install_id"} {
			if _, ok := container.Header[key]; ok {
				t.Errorf("注册容器不应携带 %s", key)
			}
		}
		for _, key := range []string{"aid", "cdid", "openudid", "clientudid", "req_id", "custom", "apk_first_install_time", "device_platform"} {
			if _, ok := container.Header[key]; !ok {
				t.Errorf("缺少字段 %s", key)
			}
		}
		if container.Header["aid"] != float64(8662) {
			t.Errorf("aid = %v", container.Header["aid"])
		}
		io.WriteString(writer, `{"device_id":2117033498771802,"install_id":2117033498775898,"device_id_str":"2117033498771802","install_id_str":"2117033498775898"}`)
	}))
	defer server.Close()

	resetHongguoAnonPool()
	downloader := &Downloader{cfg: Config{dataDir: t.TempDir()}}
	device, err := downloader.hongguoAnonDeviceAt(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if device.DeviceID != "2117033498771802" || device.InstallID != "2117033498775898" {
		t.Fatalf("设备号解析错误: %+v", device)
	}
	if device.CDID == "" || device.OpenUDID == "" {
		t.Fatal("身份缺少 CDID / OpenUDID")
	}
}

// TestHongguoAnonResponseID 覆盖注册响应的 5 种形态。
func TestHongguoAnonResponseID(t *testing.T) {
	decode := func(raw string) map[string]any {
		var result map[string]any
		decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil {
			t.Fatalf("解析夹具: %v", err)
		}
		return result
	}
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"同时有 str 与 number", `{"device_id":2117033498771802,"device_id_str":"2117033498771802"}`, "2117033498771802"},
		{"只有 number", `{"device_id":2117033498771802}`, "2117033498771802"},
		{"只有 str", `{"device_id_str":"2117033498771802"}`, "2117033498771802"},
		{"str 为空回落 number", `{"device_id":123456789,"device_id_str":""}`, "123456789"},
		{"str 非法回落 number", `{"device_id":123456789,"device_id_str":"abc"}`, "123456789"},
		{"全零视为无效", `{"device_id":0,"device_id_str":"0"}`, ""},
		{"缺失字段", `{"install_id":1}`, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := hongguoAnonResponseID(decode(testCase.raw), "device_id"); got != testCase.want {
				t.Fatalf("got %q want %q", got, testCase.want)
			}
		})
	}
}

// TestHongguoAnonDevicePoolReuse 校验「内存 → 落盘 → 注册」的取号顺序。
func TestHongguoAnonDevicePoolReuse(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&calls, 1)
		io.WriteString(writer, `{"device_id_str":"9001","install_id_str":"9002"}`)
	}))
	defer server.Close()

	resetHongguoAnonPool()
	dataDir := t.TempDir()
	downloader := &Downloader{cfg: Config{dataDir: dataDir}}
	first, err := downloader.hongguoAnonDeviceAt(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if first.DeviceID != "9001" {
		t.Fatalf("device = %+v", first)
	}
	second, err := downloader.hongguoAnonDeviceAt(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("复用失败: %v", err)
	}
	if second.DeviceID != first.DeviceID {
		t.Fatal("内存复用应返回同一身份")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("内存命中不应重新注册，实际注册 %d 次", got)
	}

	// 清空内存后应落盘复用，依然不注册。
	resetHongguoAnonPool()
	third, err := downloader.hongguoAnonDeviceAt(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("落盘复用失败: %v", err)
	}
	if third.DeviceID != first.DeviceID {
		t.Fatal("落盘复用应返回同一身份")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("落盘命中不应重新注册，实际注册 %d 次", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, hongguoAnonDeviceFilename)); err != nil {
		t.Fatalf("落盘文件缺失: %v", err)
	}
}

// TestHongguoAnonDeviceInvalidate 校验失效只清当前身份，且下一次取号会重新注册。
func TestHongguoAnonDeviceInvalidate(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		index := atomic.AddInt32(&calls, 1)
		io.WriteString(writer, `{"device_id_str":"`+string(rune('0'+index))+`001","install_id_str":"`+string(rune('0'+index))+`002"}`)
	}))
	defer server.Close()

	resetHongguoAnonPool()
	dataDir := t.TempDir()
	downloader := &Downloader{cfg: Config{dataDir: dataDir}}
	first, err := downloader.hongguoAnonDeviceAt(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	// 用别的 ID 失效不应影响当前身份。
	downloader.hongguoAnonDeviceInvalidate("not-the-current-one")
	if _, err := os.Stat(filepath.Join(dataDir, hongguoAnonDeviceFilename)); err != nil {
		t.Fatalf("无关失效不应删除落盘: %v", err)
	}
	// 用当前 ID 失效后应删除落盘并注册新号。
	downloader.hongguoAnonDeviceInvalidate(first.DeviceID)
	if _, err := os.Stat(filepath.Join(dataDir, hongguoAnonDeviceFilename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("失效后落盘应被删除，stat err = %v", err)
	}
	second, err := downloader.hongguoAnonDeviceAt(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("重新注册失败: %v", err)
	}
	if second.DeviceID == first.DeviceID {
		t.Fatal("失效后应拿到新身份")
	}
}

// TestHongguoAnonPlaybackRetriesEmptyBody 校验空 body 会换号重试，且每次换的都是新号。
func TestHongguoAnonPlaybackRetriesEmptyBody(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		index := atomic.AddInt32(&calls, 1)
		io.WriteString(writer, `{"device_id_str":"`+strconv.Itoa(int(index))+`100","install_id_str":"`+strconv.Itoa(int(index))+`200"}`)
	}))
	defer server.Close()

	resetHongguoAnonPool()
	downloader := &Downloader{cfg: Config{dataDir: t.TempDir()}}
	var seen []string
	body, identity, err := hongguoAnonPlaybackAt(context.Background(), downloader, server.URL,
		func(ctx context.Context, candidate hongguoAnonDevice) (map[string]any, error) {
			seen = append(seen, candidate.DeviceID)
			if len(seen) < hongguoAnonPlaybackAttempts {
				return nil, errHongguoEmptyBody
			}
			return map[string]any{"ok": true}, nil
		})
	if err != nil {
		t.Fatalf("编排应成功: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("编排结果错误: %v", body)
	}
	if len(seen) != hongguoAnonPlaybackAttempts {
		t.Fatalf("应尝试 %d 次，实际 %d 次: %v", hongguoAnonPlaybackAttempts, len(seen), seen)
	}
	for index := 1; index < len(seen); index++ {
		if seen[index] == seen[index-1] {
			t.Fatalf("空 body 后应换新号，实际重复使用 %q", seen[index])
		}
	}
	if identity.DeviceID != seen[len(seen)-1] {
		t.Fatalf("返回的身份应是最后一次使用的身份: %+v", identity)
	}
	if got := atomic.LoadInt32(&calls); got != int32(hongguoAnonPlaybackAttempts) {
		t.Fatalf("注册次数 = %d，期望 %d", got, hongguoAnonPlaybackAttempts)
	}
}

// TestHongguoAnonPlaybackNonEmptyErrorNoRetry 校验非空 body 错误不触发换号。
func TestHongguoAnonPlaybackNonEmptyErrorNoRetry(t *testing.T) {
	var registrations int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&registrations, 1)
		io.WriteString(writer, `{"device_id_str":"7001","install_id_str":"7002"}`)
	}))
	defer server.Close()

	resetHongguoAnonPool()
	downloader := &Downloader{cfg: Config{dataDir: t.TempDir()}}
	var calls int
	_, _, err := hongguoAnonPlaybackAt(context.Background(), downloader, server.URL,
		func(ctx context.Context, candidate hongguoAnonDevice) (map[string]any, error) {
			calls++
			return nil, errors.New("网络炸了")
		})
	if err == nil {
		t.Fatal("应返回错误")
	}
	if calls != 1 {
		t.Fatalf("非空 body 错误不应重试，实际 %d 次", calls)
	}
	if got := atomic.LoadInt32(&registrations); got != 1 {
		t.Fatalf("注册次数 = %d，期望 1", got)
	}
}

// TestHongguoAnonPlaybackExhaustedKeepsLastError 校验用满次数后返回最后一次的错误。
func TestHongguoAnonPlaybackExhaustedKeepsLastError(t *testing.T) {
	var registrations int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		index := atomic.AddInt32(&registrations, 1)
		io.WriteString(writer, `{"device_id_str":"`+strconv.Itoa(int(index))+`300","install_id_str":"`+strconv.Itoa(int(index))+`400"}`)
	}))
	defer server.Close()

	resetHongguoAnonPool()
	downloader := &Downloader{cfg: Config{dataDir: t.TempDir()}}
	var calls int
	_, _, err := hongguoAnonPlaybackAt(context.Background(), downloader, server.URL,
		func(ctx context.Context, candidate hongguoAnonDevice) (map[string]any, error) {
			calls++
			return nil, errHongguoEmptyBody
		})
	if !errors.Is(err, errHongguoEmptyBody) {
		t.Fatalf("应返回空 body 错误，实际 %v", err)
	}
	if calls != hongguoAnonPlaybackAttempts {
		t.Fatalf("应尝试 %d 次，实际 %d 次", hongguoAnonPlaybackAttempts, calls)
	}
	if got := atomic.LoadInt32(&registrations); got != int32(hongguoAnonPlaybackAttempts) {
		t.Fatalf("注册次数 = %d，期望 %d", got, hongguoAnonPlaybackAttempts)
	}
}
