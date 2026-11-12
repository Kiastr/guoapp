package core

// 红果播放模型专用「真 551 Argus/Ladon」签名。
//
// 【为什么要有这个文件】2026-11-11 起服务端对播放模型接口开始校验真签名：
// 旧的 x-argus/x-ladon 只是 base64(4 字节时间戳) 占位，且与伪造的
// x-gorgon/x-helios/x-medusa 同时出现，被判定为不可信请求后静默返回
// HTTP 200 + 0 字节（注册、目录、详情接口都正常，只有播放模型被丢弃）。
// 本项目实网复现：主页目录 72 条正常、drpys 取流「红果接口返回空内容」。
//
// 【上游依据（GPL-3.0-only，保留出处）】
//   - tsukiyo-zero/hongguoTV 提交 269c6d887「播放模型改用真签名，恢复取流」
//     kotlin-tv/core/src/main/kotlin/com/hongguotv/core/Argus.kt
//   - jackotom/hongguo-drama 6.9 提交 b364419c2
//     src/native/playback-signature.js（只调整模型签名族，其余协议不动）
//   - 二者共同标注源自 ch6vip/fqapp rust/src/sign/{argus,simon,ladon,protobuf}.rs
//
// 【作用范围：只签播放模型请求】
// 目录 / 详情 / 搜索 / 账号 / 弹幕仍走各自原有签名（signHongguoRequest、
// signHongguoCommentRequest、hgDrpySignedVideoRequest 家族），本文件不改动它们。
// 播放模型的两条链路在此接入：
//   1) drpys 两段式第一段  hgDrpyVideoModelWith   → hongguoPlaybackHeadersForURL
//   2) App video_model 单段 hongguoAppRequest     → hongguoPlaybackApplyHeaders
//
// 【为什么常量集中在此】服务端画像字段会变（版本串、licenseId、bean 布局）。
// 全部集中在下面 const/var 区，将来再改版时只需改这一处；签名算法本身
// （ProtoBuf → Simon → 551 framing → AES-CBC）是稳定的，不需要跟着动。
//
// 【回退开关】hongguoPlaybackRealSignature 置 false 即退回旧占位签名，
// 用于线上出问题时快速对照排查，不需要回滚整个提交。

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math/bits"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// hongguoPlaybackRealSignature 播放模型是否改用真 551 签名。
// 置 false 时两条播放链路都退回原有占位签名（便于 A/B 排查）。
const hongguoPlaybackRealSignature = true

const (
	// hgPlaybackLicenseID 与 fqapp 协议常量一致，进 Argus bean field6 与 Ladon 输入。
	hgPlaybackLicenseID = int64(1611921764)
	// hgPlaybackSDKVersion 是 Argus bean field8 的固定画像串，与 query 的
	// version_name 不是一回事：上游固定写死，改成 App 版本号反而会被拒。
	hgPlaybackSDKVersion = "v04.04.05-ov-android"
	// hgPlaybackAidFallback query 里没有合法 aid 时的兜底值（上游行为）。
	hgPlaybackAidFallback = int64(1967)
)

var (
	// hgPlaybackSignKey Simon-128 的四字主密钥（32 字节，按小端读 4 个 u64）。
	hgPlaybackSignKey = hgPlaybackMustHex("fc78e0a9657a0c748ce51559903ccf03510e51d3cff232d71343e88a321c5304")
	// hgPlaybackAESKey / hgPlaybackAESIV 551 外层的 AES-128-CBC 参数（无自动 padding）。
	hgPlaybackAESKey = hgPlaybackMustHex("d31e3718288a1027baab59f146a09a9c")
	hgPlaybackAESIV  = hgPlaybackMustHex("ea180a0336ed352fcd24e4d50018ae54")
	// hgPlaybackMagic8 / hgPlaybackTail9 是 551 framing 的头尾标记。
	hgPlaybackMagic8 = hgPlaybackMustHex("f2f7fcfff2f7fcff")
	hgPlaybackTail9  = hgPlaybackMustHex("eafb2cfe8568519154")
)

func hgPlaybackMustHex(value string) []byte {
	raw, err := hex.DecodeString(value)
	if err != nil {
		panic("红果播放签名常量不是合法 hex: " + err.Error())
	}
	return raw
}

// hgPlaybackPlaceholderHeaders 是必须清掉的占位/伪造签名头。
// 上游 ContentRepository.playbackRequest 的做法：把这些头全部剔除，只保留
// UA/Accept/Content-Type 一类普通头，再叠加 PlayerSigner.headers() 的真头。
// 真头与占位头同时出现会被当作伪造痕迹，必然被拒。
var hgPlaybackPlaceholderHeaders = []string{
	"x-gorgon", "x-helios", "x-medusa", "x-argus", "x-ladon", "x-khronos",
	"x-ss-stub", "x-ss-req-ticket", "x-medias", "x-tt-dt",
}

// hgPlaybackQueryParam 按 Go url.Values.Get 语义取 query 参数（首个匹配值）。
// 上游用 URLDecoder.decode / decodeURIComponent，会把 '+' 当空格、按 %XX 解码，
// 与 url.QueryUnescape 一致；解码失败时保留原串（与上游 try/catch 语义一致）。
func hgPlaybackQueryParam(query, name string) string {
	for _, part := range strings.Split(query, "&") {
		at := strings.IndexByte(part, '=')
		key := part
		if at >= 0 {
			key = part[:at]
		}
		decodedKey, err := url.QueryUnescape(key)
		if err != nil {
			decodedKey = key
		}
		if decodedKey != name {
			continue
		}
		if at < 0 {
			return ""
		}
		value, err := url.QueryUnescape(part[at+1:])
		if err != nil {
			return part[at+1:]
		}
		return value
	}
	return ""
}

// hgPlaybackPad16 PKCS#7 对齐到 16 字节整数倍（已是整数倍时补满一整块）。
func hgPlaybackPad16(data []byte) []byte {
	pad := 16 - len(data)%16
	out := make([]byte, len(data)+pad)
	copy(out, data)
	for index := len(data); index < len(out); index++ {
		out[index] = byte(pad)
	}
	return out
}

// hgPlaybackProtoBuf 只写不读的 protobuf 编码器；varint 截断到 32 位（上游语义）。
type hgPlaybackProtoBuf struct {
	parts [][]byte
}

func hgPlaybackVarint(value uint64) []byte {
	number := uint32(value)
	var out []byte
	for number >= 128 {
		out = append(out, byte(number&127)|128)
		number >>= 7
	}
	return append(out, byte(number))
}

func newHgPlaybackProtoBuf() *hgPlaybackProtoBuf {
	return &hgPlaybackProtoBuf{}
}

func (b *hgPlaybackProtoBuf) putVarint(tag int, value uint64) *hgPlaybackProtoBuf {
	b.parts = append(b.parts, hgPlaybackVarint(uint64(tag)<<3), hgPlaybackVarint(value))
	return b
}

func (b *hgPlaybackProtoBuf) putBytes(tag int, value []byte) *hgPlaybackProtoBuf {
	b.parts = append(b.parts, hgPlaybackVarint(uint64(tag)<<3|2), hgPlaybackVarint(uint64(len(value))), value)
	return b
}

func (b *hgPlaybackProtoBuf) putUtf8(tag int, value string) *hgPlaybackProtoBuf {
	return b.putBytes(tag, []byte(value))
}

func (b *hgPlaybackProtoBuf) putProtobuf(tag int, nested *hgPlaybackProtoBuf) *hgPlaybackProtoBuf {
	return b.putBytes(tag, nested.toBuf())
}

func (b *hgPlaybackProtoBuf) toBuf() []byte {
	var out []byte
	for _, part := range b.parts {
		out = append(out, part...)
	}
	return out
}

// hgPlaybackSimonRounds 展开 Simon-128/128 的 72 个轮密钥。
func hgPlaybackSimonRounds(key [4]uint64) [72]uint64 {
	var rounds [72]uint64
	copy(rounds[:4], key[:])
	const z = uint64(0x3dc94c3a046d678b)
	for index := 4; index < len(rounds); index++ {
		mixed := bits.RotateLeft64(rounds[index-1], -3) ^ rounds[index-3]
		mixed ^= bits.RotateLeft64(mixed, -1)
		bit := (z >> uint((index-4)%62)) & 1
		rounds[index] = ^rounds[index-4] ^ mixed ^ bit ^ 3
	}
	return rounds
}

// hgPlaybackSimonEncrypt 加密一个 16 字节块（两个小端 u64）。
func hgPlaybackSimonEncrypt(left, right uint64, key [4]uint64) (uint64, uint64) {
	rounds := hgPlaybackSimonRounds(key)
	x, y := left, right
	for _, round := range rounds {
		x, y = y, x^(bits.RotateLeft64(y, 1)&bits.RotateLeft64(y, 8))^bits.RotateLeft64(y, 2)^round
	}
	return x, y
}

// hgPlaybackBodyHash Argus bean field13：无正文时 SM3(16 个 0 字节)[:6]，
// 有正文时 SM3(md5(body) 的原始字节)[:6]。注意是对 hex 解码后的 16 字节做
// SM3，不是对 hex 字符串本身——大小写不同的 hex 结果相同。
func hgPlaybackBodyHash(stubHex string) []byte {
	if stubHex == "" {
		sum := hgDrpySM3(make([]byte, 16))
		return sum[:6]
	}
	raw, err := hex.DecodeString(stubHex)
	if err != nil {
		raw = []byte{}
	}
	sum := hgDrpySM3(raw)
	return sum[:6]
}

// hgPlaybackQueryHash Argus bean field14：SM3(最终 query 的原始字节)[:6]。
// 必须对**发出去的 RawQuery 原文**做，不能重新编码——上游 golden 特意断言
// `extra=%2A` 与 `extra=*` 产生不同签名。
func hgPlaybackQueryHash(query string) []byte {
	if query == "" {
		sum := hgDrpySM3(make([]byte, 16))
		return sum[:6]
	}
	sum := hgDrpySM3([]byte(query))
	return sum[:6]
}

// hgPlaybackArgus 生成 x-argus：有序 protobuf bean → PKCS#7 → Simon →
// 551 framing（MAGIC8 + 密文 + TAIL9，异或、反转）→ AES-128-CBC → base64。
//
// installID 固定传空串：上游 golden 明确断言「iid 存在时 field16 仍为空」，
// 不要拿 query 的 iid 去填它。
func hgPlaybackArgus(query, stubHex string, ts, aid, licenseID int64, deviceID, installID string, randField uint32) string {
	aidText := strconv.FormatInt(aid, 10)
	if aid == 0 {
		aidText = strconv.FormatInt(hgPlaybackAidFallback, 10)
	}
	licenseText := strconv.FormatInt(licenseID, 10)
	if licenseID == 0 {
		licenseText = strconv.FormatInt(hgPlaybackLicenseID, 10)
	}

	sub15 := newHgPlaybackProtoBuf().
		putVarint(1, 1).putVarint(2, 1).putVarint(3, 1).putVarint(7, 0xc792eccc)
	sub23 := newHgPlaybackProtoBuf().
		putUtf8(1, "NX551J").putVarint(2, 8196).putVarint(4, 0x80e0d800)

	bean := newHgPlaybackProtoBuf().
		putVarint(1, 1077940818). // 0x20200929 * 2
		putVarint(2, 2).
		putVarint(3, uint64(randField)).
		putUtf8(4, aidText).
		putUtf8(5, deviceID).
		putUtf8(6, licenseText).
		putUtf8(7, hgPlaybackQueryParam(query, "version_name")).
		putUtf8(8, hgPlaybackSDKVersion).
		putVarint(9, 134744640).
		putBytes(10, make([]byte, 8)).
		putVarint(11, 0).
		putVarint(12, uint64(ts)<<1).
		putBytes(13, hgPlaybackBodyHash(stubHex)).
		putBytes(14, hgPlaybackQueryHash(query)).
		putProtobuf(15, sub15).
		putUtf8(16, installID).
		putUtf8(20, "none").
		putVarint(21, 738).
		putProtobuf(23, sub23).
		putVarint(25, 2)

	protobuf := hgPlaybackPad16(bean.toBuf())
	var key [4]uint64
	for index := range key {
		key[index] = binary.LittleEndian.Uint64(hgPlaybackSignKey[index*8:])
	}
	encrypted := make([]byte, len(protobuf))
	for offset := 0; offset < len(protobuf); offset += 16 {
		x, y := hgPlaybackSimonEncrypt(
			binary.LittleEndian.Uint64(protobuf[offset:]),
			binary.LittleEndian.Uint64(protobuf[offset+8:]),
			key,
		)
		binary.LittleEndian.PutUint64(encrypted[offset:], x)
		binary.LittleEndian.PutUint64(encrypted[offset+8:], y)
	}

	framed := make([]byte, 0, len(hgPlaybackMagic8)+len(encrypted)+len(hgPlaybackTail9))
	framed = append(framed, hgPlaybackMagic8...)
	framed = append(framed, encrypted...)
	framed = append(framed, hgPlaybackTail9...)
	for index := len(hgPlaybackMagic8); index < len(framed); index++ {
		framed[index] ^= hgPlaybackMagic8[index%len(hgPlaybackMagic8)]
	}
	for left, right := 0, len(framed)-1; left < right; left, right = left+1, right-1 {
		framed[left], framed[right] = framed[right], framed[left]
	}
	padded := hgPlaybackPad16(append(framed, 'a', 'o'))

	block, err := aes.NewCipher(hgPlaybackAESKey)
	if err != nil {
		return ""
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, hgPlaybackAESIV).CryptBlocks(ciphertext, padded)
	result := make([]byte, 0, 2+len(ciphertext))
	result = append(result, 0xf2, 0x81)
	result = append(result, ciphertext...)
	return base64.StdEncoding.EncodeToString(result)
}

// hgPlaybackLadon 生成 x-ladon：34 轮 ARX 分组密码，输出 4 字节随机前缀 + 密文。
// 输入串是 `ts-licenseID-aid`，aid 必须取 query 里的真实值，不可用兜底常量。
func hgPlaybackLadon(ts, licenseID, aid int64, random [4]byte) string {
	aidText := strconv.FormatInt(aid, 10)
	material := make([]byte, 0, len(random)+len(aidText))
	material = append(material, random[:]...)
	material = append(material, aidText...)
	sum := md5.Sum(material)
	digest := []byte(hex.EncodeToString(sum[:])) // 32 个 ASCII 字符
	words := [4]uint64{
		binary.LittleEndian.Uint64(digest[0:8]),
		binary.LittleEndian.Uint64(digest[8:16]),
		binary.LittleEndian.Uint64(digest[16:24]),
		binary.LittleEndian.Uint64(digest[24:32]),
	}

	table := make([]uint64, 0, 35)
	table = append(table, words[0])
	queue := []uint64{words[2], words[3]}
	b0, b8 := words[0], words[1]
	for index := 0; index < 34; index++ {
		mixed := bits.RotateLeft64(b8, -8) + b0
		mixed ^= uint64(index)
		queue = append(queue, mixed)
		final := mixed ^ bits.RotateLeft64(b0, 3)
		table = append(table, final)
		b0 = final
		b8 = queue[0]
		queue = queue[1:]
	}

	text := strconv.FormatInt(ts, 10) + "-" + strconv.FormatInt(licenseID, 10) + "-" + aidText
	input := hgPlaybackPad16([]byte(text))
	for offset := 0; offset < len(input); offset += 16 {
		a := binary.LittleEndian.Uint64(input[offset:])
		b := binary.LittleEndian.Uint64(input[offset+8:])
		for index := 0; index < 34; index++ {
			b = table[index] ^ (a + bits.RotateLeft64(b, -8))
			a = b ^ bits.RotateLeft64(a, 3)
		}
		binary.LittleEndian.PutUint64(input[offset:], a)
		binary.LittleEndian.PutUint64(input[offset+8:], b)
	}
	out := make([]byte, 0, len(random)+len(input))
	out = append(out, random[:]...)
	out = append(out, input...)
	return base64.StdEncoding.EncodeToString(out)
}

// hgPlaybackSignHeaders 按最终 query 与请求体生成播放模型的真签名头。
// ts 必须取 _rticket/1000（与 URL 同源），ticket 必须是最终 _rticket 值：
// 上游明确「x-khronos 必须与 URL 的 _rticket 同源，否则签名与 query 对不上」。
// 换路/重试必须重新调用本函数，不能复用旧头（时间戳参与签名）。
func hgPlaybackSignHeaders(query string, body []byte, ts, ticket int64, randField uint32, ladonRandom [4]byte) map[string]string {
	aid := hgPlaybackAidFallback
	if raw := hgPlaybackQueryParam(query, "aid"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			aid = parsed
		}
	}
	stub := ""
	if len(body) > 0 {
		sum := md5.Sum(body)
		stub = hex.EncodeToString(sum[:]) // 小写 hex，与上游一致
	}
	headers := map[string]string{
		"x-ss-req-ticket": strconv.FormatInt(ticket, 10),
		"x-khronos":       strconv.FormatInt(ts, 10),
		"x-ladon":         hgPlaybackLadon(ts, hgPlaybackLicenseID, aid, ladonRandom),
		"x-argus": hgPlaybackArgus(query, stub, ts, aid, hgPlaybackLicenseID,
			hgPlaybackQueryParam(query, "device_id"), "", randField),
	}
	if stub != "" {
		headers["x-ss-stub"] = stub
	}
	return headers
}

// hongguoPlaybackSignForRequest 对外入口：自动生成随机量。
func hongguoPlaybackSignForRequest(query string, body []byte, ts, ticket int64) map[string]string {
	var ladonRandom [4]byte
	var randField [4]byte
	if _, err := crand.Read(ladonRandom[:]); err != nil {
		binary.LittleEndian.PutUint32(ladonRandom[:], uint32(time.Now().UnixNano()))
	}
	if _, err := crand.Read(randField[:]); err != nil {
		binary.LittleEndian.PutUint32(randField[:], uint32(time.Now().Unix()))
	}
	return hgPlaybackSignHeaders(query, body, ts, ticket,
		binary.LittleEndian.Uint32(randField[:]), ladonRandom)
}

// hongguoPlaybackStripPlaceholders 从请求头里剔除占位/伪造签名头（大小写不敏感）。
func hongguoPlaybackStripPlaceholders(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers)+5)
	for key, value := range headers {
		blocked := false
		for _, placeholder := range hgPlaybackPlaceholderHeaders {
			if strings.EqualFold(key, placeholder) {
				blocked = true
				break
			}
		}
		if !blocked {
			out[key] = value
		}
	}
	return out
}

// hongguoPlaybackTicketFromURL 从最终 URL 里取 _rticket；解析失败时退回当前毫秒。
// branch 抖动会改 _rticket，所以必须在 URL 定稿之后调用。
func hongguoPlaybackTicketFromURL(fullURL string) int64 {
	query := fullURL
	if at := strings.IndexByte(fullURL, '?'); at >= 0 {
		query = fullURL[at+1:]
	} else {
		return time.Now().UnixMilli()
	}
	if ticket, err := strconv.ParseInt(hgPlaybackQueryParam(query, "_rticket"), 10, 64); err == nil && ticket > 0 {
		return ticket
	}
	return time.Now().UnixMilli()
}

// hongguoPlaybackHeadersForURL 给 drpys 第一段用：拿最终 URL 与原始头，
// 剔除占位头后叠加真签名。query 直接取 RawQuery 原文，不重新编码。
func hongguoPlaybackHeadersForURL(fullURL string, payload []byte, original map[string]string) map[string]string {
	headers := hongguoPlaybackStripPlaceholders(original)
	query := fullURL
	if at := strings.IndexByte(fullURL, '?'); at >= 0 {
		query = fullURL[at+1:]
	}
	ticket := hongguoPlaybackTicketFromURL(fullURL)
	for key, value := range hongguoPlaybackSignForRequest(query, payload, ticket/1000, ticket) {
		headers[key] = value
	}
	return headers
}

// hongguoPlaybackApplyHeaders 给 App video_model 线路用：就地改 http.Request。
func hongguoPlaybackApplyHeaders(request *http.Request, body []byte, ts, ticket int64) {
	for _, placeholder := range hgPlaybackPlaceholderHeaders {
		request.Header.Del(placeholder)
	}
	for key, value := range hongguoPlaybackSignForRequest(request.URL.RawQuery, body, ts, ticket) {
		request.Header.Set(key, value)
	}
}
