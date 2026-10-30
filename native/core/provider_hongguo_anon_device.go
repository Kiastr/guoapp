package core

// 红果匿名取流设备身份池。
//
// 【为什么需要】2026-10-30 实测：取流端点（`multi_video_model`）对设备身份是硬门槛——
// 随机合成的 `iid` / `device_id` 一律 HTTP 200 + 空 body，只有走过 `device_register`
// 的设备身份才可能拿到数据。把公开库的 drpys 签名链抽成独立程序做对照：随机身份
// 5 轮 0 有效响应，同一套签名换成注册身份后 5 个身份各 1/1 成功。
//
// 【一个身份只能用一次】同一身份对同一端点重复请求，第 2 次起稳定返回空 body
// （实测连续 5 次只有第 1 次有数据）。因此本文件的策略不是「把身份用到底」，而是
// 「拿到空响应就作废换号」——重试同一个号没有任何意义。
//
// 【注册协议】请求体不是明文 JSON，而是：
//
//	magic(6) || random(32) || AES-128-CBC( sha512(gzip(json)) || gzip(json) )
//
// 其中 key / iv 由 `sha512(sha512(random) || salt)` 派生（前 16 字节做 key，后 16 字节
// 做 iv），salt 为协议固定常量。响应是明文 JSON，`device_id` / `install_id` 可能以
// 数字或 `*_str` 字符串两种形态出现，优先取后者。
//
// 该流程与 jackotom/hongguo-drama（GPL-3.0）的 `src/native/playback-device.js` 描述
// 一致；本文件按上述协议规格用 Go 独立实现，未复制其代码。
//
// 【与登录链设备的关系】本项目不含登录链，匿名身份只服务取流，生命周期短、可随时
// 作废重注册，因此单独落盘、单独内存槽。

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// hongguoAnonDeviceFilename 匿名取流身份的落盘文件名（位于 cfg.dataDir）。
	hongguoAnonDeviceFilename = "hongguo-anon-device.json"

	// hongguoAnonDeviceTTL 匿名身份的最长复用期。
	//
	// 固定 device_id 长期打同一接口，一旦被风控标记就是持久失效（实测废号 10 秒后
	// 仍空，说明标记不随时间自愈）。7 天轮换在「省注册」与「避免长期指纹」之间取
	// 平衡；真正的兜底是空 body 触发的即时作废。
	hongguoAnonDeviceTTL = 7 * 24 * time.Hour

	// hongguoAnonPlaybackAttempts 一次取流最多尝试几个匿名身份。
	//
	// 注册废号率实测约 1/3（部分身份注册成功但取流永久空 body），单次尝试有 1/3
	// 概率误判成风控；3 次把误判率压到 1/27，代价是真风控时多注册两个号（各约
	// 300ms）。取流是播放的前置阻塞步骤，宁可多花一秒也不要让用户看到「请登录」。
	hongguoAnonPlaybackAttempts = 3

	// hongguoAnonRegisterEndpoint 匿名设备注册入口。
	//
	// 注意是 applog 网关（`log5-applog-lf`），不是内容网关（`api5-normal-*`）——
	// `device_register` 只在前者上存在，打后者是 404。
	hongguoAnonRegisterEndpoint = "https://log5-applog-lf.fqnovel.com/service/2/device_register/?tt_data=a&cronet_version=8d40f833_2026-03-03&ttnet_version=4.2.243.31-douyin&use_store_region_cookie=1"

	// hongguoAnonRegisterUA 注册请求的 UA（与内容请求的 UA 不同：注册走的是
	// 「应用日志」通道，UA 里不含 Cronet/TTNet 段）。
	hongguoAnonRegisterUA = "com.phoenix.read/72232 (Linux; U; Android 16; zh_CN; 25053RT47C; Build/BP2A.250605.031.A3)"

	// hongguoAnonRegisterMaxBody 注册响应体上限，防止异常响应撑爆内存。
	hongguoAnonRegisterMaxBody = 1 << 20
)

// hongguoAnonRegisterSalt 注册请求 KDF 的固定盐（协议常量，非随机）。
//
// 长度 64 字节；与 `sha512(random)` 拼接后再取 sha512，前 16 字节做 AES key、
// 后 16 字节做 CBC iv。
var hongguoAnonRegisterSalt = hongguoAnonSaltBytes()

// hongguoAnonSaltBytes 解码 KDF 盐。
//
// 用 hex 字面量而不是手写 []byte：64 字节的数组手抄极易错位，而盐只要错一个字节，
// 派生出的 AES key 就变，服务端会返回 `device_id: 0` 的「软拒绝」（HTTP 仍是 200），
// 排查成本极高。hex 串与协议常量一一对应，可直接比对。
func hongguoAnonSaltBytes() []byte {
	const encoded = "4dd4c2e6b83162090e52b3c7a6733ba41cb2462b829ab58a196b39db57177524f49baf7f08e8d68d26a72e37c1a95a2f1f05a51892aef2949732b62a38aadd58"
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) != 64 {
		panic("红果匿名设备注册盐常量损坏")
	}
	return decoded
}

// hongguoAnonDevice 一次匿名设备注册的产物。
type hongguoAnonDevice struct {
	DeviceID  string `json:"deviceId"`
	InstallID string `json:"installId"`
	// CDID / OpenUDID 与注册容器内的值同源：跨请求复用同一身份时必须一起沿用，
	// 否则 iid 与 cdid 对不上，等于同一「设备」出现两套指纹。
	CDID     string `json:"cdid,omitempty"`
	OpenUDID string `json:"openudid,omitempty"`
	// CreatedAt 注册时刻的 unix 秒，用于 TTL 判定。
	CreatedAt int64 `json:"createdAt"`
}

// usable 判断身份是否仍在复用期内。
func (device hongguoAnonDevice) usable(now time.Time) bool {
	if device.DeviceID == "" || device.InstallID == "" || device.CreatedAt <= 0 {
		return false
	}
	return now.Unix()-device.CreatedAt < int64(hongguoAnonDeviceTTL/time.Second)
}

// hongguoAnonPool 进程内的身份缓存。
//
// 与 Downloader 分开存：匿名身份是全局单例语义（所有取流共用一套「当前身份」），
// 挂在 Downloader 上会让多实例测试互相污染；用包级状态 + 互斥锁即可。
var hongguoAnonPool struct {
	mu     sync.Mutex
	loaded bool
	device hongguoAnonDevice
}

// hongguoAnonDeviceFilePath 落盘路径；dataDir 为空时返回空串（测试/无持久化环境）。
func (downloader *Downloader) hongguoAnonDeviceFilePath() string {
	if downloader.cfg.dataDir == "" {
		return ""
	}
	return filepath.Join(downloader.cfg.dataDir, hongguoAnonDeviceFilename)
}

// hongguoAnonDevice 取一个可用的匿名取流身份：内存 → 落盘 → 现场注册。
//
// 返回的身份不保证服务端认可（废号率约 1/3，见文件头注释）；调用方在真实请求拿到
// 空 body 时必须调 hongguoAnonDeviceInvalidate 并重试。
func (downloader *Downloader) hongguoAnonDevice(ctx context.Context) (hongguoAnonDevice, error) {
	return downloader.hongguoAnonDeviceAt(ctx, hongguoAnonRegisterEndpoint)
}

// hongguoAnonDeviceAt 与 hongguoAnonDevice 相同，但允许指定注册入口（测试用）。
func (downloader *Downloader) hongguoAnonDeviceAt(ctx context.Context, endpoint string) (hongguoAnonDevice, error) {
	// 串行化整个取号流程：并发取流同时发现身份失效时，只应该注册一次。
	hongguoAnonPool.mu.Lock()
	defer hongguoAnonPool.mu.Unlock()

	now := time.Now()
	if hongguoAnonPool.loaded && hongguoAnonPool.device.usable(now) {
		return hongguoAnonPool.device, nil
	}

	if cached := downloader.loadHongguoAnonDevice(); cached.usable(now) {
		hongguoAnonPool.device, hongguoAnonPool.loaded = cached, true
		return cached, nil
	}

	registered, err := downloader.hongguoAnonRegisterDevice(ctx, endpoint)
	if err != nil {
		return hongguoAnonDevice{}, err
	}
	hongguoAnonPool.device, hongguoAnonPool.loaded = registered, true
	downloader.saveHongguoAnonDevice(registered)
	return registered, nil
}

// hongguoAnonDeviceInvalidate 作废一个被服务端拒绝的身份。
//
// 只在 deviceID 与当前身份一致时动手：并发场景下别的协程可能已经换过号，
// 此时删除会把新号一并抹掉。内存未加载（loaded=false）同样直接返回——
// 说明文件已被别的协程清掉，无需重复处理。
func (downloader *Downloader) hongguoAnonDeviceInvalidate(deviceID string) {
	hongguoAnonPool.mu.Lock()
	defer hongguoAnonPool.mu.Unlock()
	if !hongguoAnonPool.loaded || hongguoAnonPool.device.DeviceID != deviceID {
		return
	}
	hongguoAnonPool.device = hongguoAnonDevice{}
	hongguoAnonPool.loaded = false
	if path := downloader.hongguoAnonDeviceFilePath(); path != "" {
		_ = os.Remove(path)
	}
}

// hongguoAnonPlayback 用匿名身份执行一次取流请求；空 body 时判定该身份被拒，
// 作废后换号重试，最多 hongguoAnonPlaybackAttempts 次。
//
// 【为什么抽成共享函数】drpys 与 app 两条线路的请求构造完全不同，但「取号 → 打请求 →
// 空就换号」的编排一模一样。放在一处，重试次数与失效判定（errors.Is(errHongguoEmptyBody)）
// 只有一份，避免两条线路对「什么算身份失效」产生分歧。
func hongguoAnonPlayback(
	ctx context.Context,
	downloader *Downloader,
	run func(context.Context, hongguoAnonDevice) (map[string]any, error),
) (map[string]any, hongguoAnonDevice, error) {
	return hongguoAnonPlaybackAt(ctx, downloader, hongguoAnonRegisterEndpoint, run)
}

// hongguoAnonPlaybackAt 与 hongguoAnonPlayback 相同，但允许指定注册入口（测试用）。
func hongguoAnonPlaybackAt(
	ctx context.Context,
	downloader *Downloader,
	endpoint string,
	run func(context.Context, hongguoAnonDevice) (map[string]any, error),
) (map[string]any, hongguoAnonDevice, error) {
	identity, err := downloader.hongguoAnonDeviceAt(ctx, endpoint)
	if err != nil {
		return nil, hongguoAnonDevice{}, err
	}
	var body map[string]any
	for attempt := 0; attempt < hongguoAnonPlaybackAttempts; attempt++ {
		body, err = run(ctx, identity)
		// 非空 body 错误（网络/端点/解析）与身份无关，直接交回调用方；
		// 已是最后一次尝试也不再换号，免得白注册一个用不上的身份。
		if !errors.Is(err, errHongguoEmptyBody) || attempt == hongguoAnonPlaybackAttempts-1 {
			break
		}
		downloader.hongguoAnonDeviceInvalidate(identity.DeviceID)
		next, registerErr := downloader.hongguoAnonDeviceAt(ctx, endpoint)
		if registerErr != nil {
			break
		}
		identity = next
	}
	return body, identity, err
}

// hongguoAnonRegisterDevice 现场注册一个匿名设备身份。
func (downloader *Downloader) hongguoAnonRegisterDevice(ctx context.Context, endpoint string) (hongguoAnonDevice, error) {
	now := time.Now()
	nowMs := now.UnixMilli()
	cdid := hongguoAnonUUID()
	openUDID := hongguoAnonHex(8)

	// 设备画像：与红果 App 7.3.9.32 的注册容器同构。字段值都是协议约定，
	// 不采集本机信息（`ipv6_list` / `sim_serial_number` 给空数组而非 null，
	// 与客户端序列化结果一致）。
	header := map[string]any{
		"display_name":          "红果免费短剧",
		"update_version_code":   73932,
		"manifest_version_code": 73932,
		"app_version_minor":     "7.3.9.32",
		"aid":                   8662,
		"channel":               "update_64",
		"package":               "com.phoenix.read",
		"app_version":           "7.3.9.32",
		"version_code":          73932,
		"sdk_version":           "3.7.0-rc.34-fanqie-xiaoshuo",
		"sdk_target_version":    29,
		"git_hash":              "3a88f4e",
		"os":                    "Android",
		"os_version":            "16",
		"os_api":                36,
		"device_model":          "25053RT47C",
		"device_brand":          "Redmi",
		"device_manufacturer":   "Redmi",
		"cpu_abi":               "arm64-v8a",
		"release_build":         "b5e187f",
		"density_dpi":           520,
		"display_density":       "mdpi",
		"resolution":            "1280x2772",
		"language":              "zh",
		"timezone":              8,
		"access":                "wifi",
		"not_request_sender":    0,
		"carrier":               "CHINA MOBILE",
		"mcc_mnc":               "46000",
		"rom":                   "eng.root.20231116.160340",
		"rom_version":           "W528JS release-keys",
		"cdid":                  cdid,
		"sig_hash":              "56a962410c494bbaf0b58dba20cae56f",
		"openudid":              openUDID,
		"clientudid":            hongguoAnonUUID(),
		"ipv6_list":             []string{},
		"region":                "CN",
		"tz_name":               "Asia/Shanghai",
		"tz_offset":             28800,
		"sim_region":            "cn",
		"sim_serial_number":     []string{},
		"oaid_may_support":      false,
		"req_id":                hongguoAnonUUID(),
		"device_platform":       "android",
		"custom": map[string]any{
			"account_region_src": "fallback",
			"account_region":     "cn",
			"host_bit":           64,
			"dragon_device_type": "phone",
		},
		"pre_installed_channel": "",
		"apk_first_install_time": nowMs,
		"is_system_app":          0,
		"sdk_flavor":             "china",
		"guest_mode":             0,
	}
	container := map[string]any{"magic_tag": "ss_app_log", "header": header, "_gen_time": nowMs}
	plain, err := json.Marshal(container)
	if err != nil {
		return hongguoAnonDevice{}, errors.New("红果匿名设备注册参数无效")
	}
	body, err := hongguoAnonSealRegisterBody(plain)
	if err != nil {
		return hongguoAnonDevice{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return hongguoAnonDevice{}, errors.New("红果匿名设备注册请求构造失败")
	}
	request.Header.Set("User-Agent", hongguoAnonRegisterUA)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/octet-stream;tt-data=a")
	request.Header.Set("log-encode-type", "gzip")
	request.Header.Set("x-ss-dp", "8662")
	request.Header.Set("x-ss-req-ticket", strconv.FormatInt(nowMs, 10))
	request.Header.Set("x-vc-bdturing-sdk-version", "4.0.3.cn")
	request.Header.Set("lc", "101")
	request.Header.Set("sdk-version", "2")
	request.Header.Set("passport-sdk-version", "5051452")
	request.Header.Set("x-neptune", "-8|50:51:59:30:40:47:49:39")
	stub := md5.Sum(body)
	request.Header.Set("x-ss-stub", strings.ToUpper(hex.EncodeToString(stub[:])))

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return hongguoAnonDevice{}, ctx.Err()
		}
		return hongguoAnonDevice{}, errors.New("红果匿名设备注册失败，请检查网络")
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, hongguoAnonRegisterMaxBody))
	if readErr != nil {
		return hongguoAnonDevice{}, errors.New("红果匿名设备注册响应读取失败")
	}
	if response.StatusCode != http.StatusOK {
		return hongguoAnonDevice{}, fmt.Errorf("红果匿名设备注册失败（HTTP %d）", response.StatusCode)
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return hongguoAnonDevice{}, errors.New("红果匿名设备注册返回无效数据")
	}
	deviceID := hongguoAnonResponseID(result, "device_id")
	installID := hongguoAnonResponseID(result, "install_id")
	if deviceID == "" || installID == "" {
		return hongguoAnonDevice{}, errors.New("红果匿名设备注册未返回有效设备号")
	}
	return hongguoAnonDevice{
		DeviceID:  deviceID,
		InstallID: installID,
		CDID:      cdid,
		OpenUDID:  openUDID,
		CreatedAt: now.Unix(),
	}, nil
}

// hongguoAnonSealRegisterBody 把注册容器加密成协议要求的二进制形态。
//
// 结构：magic(6) || random(32) || AES-128-CBC( sha512(gzip) || gzip )，
// 其中 key/iv = sha512( sha512(random) || salt ) 的前 / 后 16 字节。
func hongguoAnonSealRegisterBody(plain []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(plain); err != nil {
		return nil, errors.New("红果匿名设备注册压缩失败")
	}
	if err := writer.Close(); err != nil {
		return nil, errors.New("红果匿名设备注册压缩失败")
	}
	payload := compressed.Bytes()

	random := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return nil, errors.New("红果匿名设备注册随机数生成失败")
	}
	seeded := sha512.Sum512(random)
	keyed := sha512.New()
	keyed.Write(seeded[:])
	keyed.Write(hongguoAnonRegisterSalt)
	derived := keyed.Sum(nil)

	block, err := aes.NewCipher(derived[:16])
	if err != nil {
		return nil, errors.New("红果匿名设备注册密钥派生失败")
	}
	digest := sha512.Sum512(payload)
	buffer := make([]byte, 0, len(digest)+len(payload)+aes.BlockSize)
	buffer = append(buffer, digest[:]...)
	buffer = append(buffer, payload...)
	// PKCS#7 填充。
	padding := aes.BlockSize - len(buffer)%aes.BlockSize
	buffer = append(buffer, bytes.Repeat([]byte{byte(padding)}, padding)...)
	encrypted := make([]byte, len(buffer))
	cipher.NewCBCEncrypter(block, derived[16:32]).CryptBlocks(encrypted, buffer)

	sealed := make([]byte, 0, 6+len(random)+len(encrypted))
	sealed = append(sealed, 0x74, 0x63, 0x05, 0x10, 0x00, 0x00)
	sealed = append(sealed, random...)
	sealed = append(sealed, encrypted...)
	return sealed, nil
}

// hongguoAnonResponseID 从注册响应里取设备号。
//
// 优先 `*_str`：服务端对小整数会给 JSON number，超过 2^53 会丢精度（Go 侧用
// json.Number 读，但历史上偶见只给 number 且被 JS 端截断的响应），字符串形态最稳。
func hongguoAnonResponseID(result map[string]any, name string) string {
	if value, ok := result[name+"_str"]; ok {
		if text, ok := value.(string); ok && hongguoNumericID.MatchString(text) && strings.Trim(text, "0") != "" {
			return text
		}
	}
	switch value := result[name].(type) {
	case json.Number:
		text := value.String()
		if hongguoNumericID.MatchString(text) && strings.Trim(text, "0") != "" {
			return text
		}
	case string:
		if hongguoNumericID.MatchString(value) && strings.Trim(value, "0") != "" {
			return value
		}
	}
	return ""
}

// loadHongguoAnonDevice 读落盘身份；任何异常都当作「没有」。
func (downloader *Downloader) loadHongguoAnonDevice() hongguoAnonDevice {
	path := downloader.hongguoAnonDeviceFilePath()
	if path == "" {
		return hongguoAnonDevice{}
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) == 0 || len(content) > 1<<16 {
		return hongguoAnonDevice{}
	}
	var device hongguoAnonDevice
	if err := json.Unmarshal(content, &device); err != nil {
		return hongguoAnonDevice{}
	}
	return device
}

// saveHongguoAnonDevice 写落盘身份；失败不影响主流程（下次重新注册即可）。
func (downloader *Downloader) saveHongguoAnonDevice(device hongguoAnonDevice) {
	path := downloader.hongguoAnonDeviceFilePath()
	if path == "" {
		return
	}
	content, err := json.Marshal(device)
	if err != nil {
		return
	}
	_ = writeNativeCacheFile(path, content)
}

// hongguoAnonUUID 生成 v4 UUID（注册容器里的 cdid / clientudid / req_id 用它）。
func hongguoAnonUUID() string {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return ""
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// hongguoAnonHex 生成 n 字节随机数的小写 hex（openudid 用它）。
func hongguoAnonHex(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
