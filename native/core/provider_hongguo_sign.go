package core

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/bits"
	"net/http"
	"strconv"
	"time"
)

func newHongguoDeviceID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return strconv.FormatUint(1_000_000_000_000_000_000+binary.BigEndian.Uint64(random[:])%8_000_000_000_000_000_000, 10)
}

// signHongguoRequest 红果 App 请求头签名。
//
// ⚠ X-Gorgon 自 2026-10-16 起**不再发送**（下方 keepGorgon=false）。
// 实测（curl 逐项对照 + 本项目真实代码路径 `resolveHongguoAppMedia` 对拍）：
//   - 带本函数的 X-Gorgon + X-Khronos → 服务端判为签名校验失败 → HTTP 200 **空 body**
//     （取流全挂，正是「红果接口拒绝了播放请求」「未返回有效数据」的真因）
//   - **不发 X-Gorgon**（X-Khronos / X-SS-STUB / X-SS-Req-Ticket 保留）→ 正常取到
//     1080P 播放地址（实测通过）
//
// 对照实验：伪造 X-Gorgon 同样触发空 body；仅 X-Argus/X-Ladon、或完全不签名都正常；
// 带伪造 Cookie 也无影响 ⇒ 服务端对「携带 X-Gorgon」的请求会做校验，本实现的算法
// 当前不被接受（2026-10-15 15:30 前后服务端开始校验，当天所有第三方客户端同时挂）。
//
// 待办：移植服务端认可的签名（参考实现为 RC4 变体六头集：gorgon/khronos/ladon/argus/
// helios/medusa，本项目弹幕链路 signHongguoCommentRequest 已是同族 RC4 变体，可作起点）。
// 修好后把 keepGorgon 改回 true 即可恢复。
func signHongguoRequest(request *http.Request, body []byte, now time.Time) {
	const keepGorgon = false
	timestamp := uint32(now.Unix())
	if keepGorgon {
		queryHash := md5.Sum([]byte(request.URL.RawQuery))
		var payload [20]byte
		copy(payload[:4], queryHash[:4])
		if body != nil {
			bodyHash := md5.Sum(body)
			copy(payload[4:8], bodyHash[:4])
		}
		copy(payload[12:16], []byte{0, 6, 11, 28})
		binary.BigEndian.PutUint32(payload[16:], timestamp)
		key := [...]byte{0x44, 0xb9, 0xb9, 0xd9, 0xa4, 0xae, 0xf9, 0xfc, 0xa4, 0x93, 0xaa, 0x75, 0x7c, 0xa3, 0xc2, 0xc4, 0xa4, 0x96, 0x93, 0x8f}
		for index := range payload {
			payload[index] ^= key[index]
		}
		for index := range payload {
			mixed := bits.RotateLeft8(payload[index], 4) ^ payload[(index+1)%len(payload)]
			payload[index] = bits.Reverse8(mixed) ^ 0xff ^ byte(len(payload))
		}
		signature := append([]byte{0x84, 0x04, 0x40, 0x1c, 0, 0}, payload[:]...)
		request.Header.Set("X-Gorgon", hex.EncodeToString(signature))
	}
	if body != nil {
		request.Header.Set("X-SS-STUB", fmt.Sprintf("%X", md5.Sum(body)))
	}
	request.Header.Set("X-Khronos", strconv.FormatUint(uint64(timestamp), 10))
	request.Header.Set("X-SS-Req-Ticket", strconv.FormatInt(now.UnixMilli(), 10))
}
