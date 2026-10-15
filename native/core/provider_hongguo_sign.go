package core

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
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
// 【服务端规则与实测依据】2026-10-16 用参考实现（sunnysky123/hongguo-web 的
// PureSign.java，六头纯代码实现）编译成预言机后逐组对照实测（同一 URL / 同一 body）：
//
//	完全不发签名头                              → 放行（正常返回）
//	gorgon + khronos（+ ss-stub / ss-req-ticket）→ HTTP 200 **空 body**（比不签名更糟）
//	gorgon + khronos + argus + ladon            → 正常返回（2.3 万字节完整数据）
//	完整六头（再加 helios / medusa）             → 同样正常，但 **helios/medusa 并非必需**
//
// ⇒ 一旦携带签名，**必须带齐 gorgon / khronos / argus / ladon 这一最小集**；
// 不完整的签名会被服务端当作伪造成客户端处理（返空）。本端点不校验 helios/medusa。
//
// 因此 2026-10-15 15:30 前后本项目取流全挂的真因是：Phase 1/2 只发了
// gorgon + khronos（+ ss-stub / ss-req-ticket），**缺 argus / ladon**，
// 服务端上线该校验后即被拒。**Gorgon 算法本身没有错**——实测本函数的 Gorgon
// 与参考实现的 RC4 变体虽算法不同，两者都被服务端接受。
//
// X-Argus / X-Ladon 构造极简，就是同一时间戳的 base64 两种字节序：
//
//	x-argus = base64(le32(khronos))
//	x-ladon = base64(be32(khronos))
func signHongguoRequest(request *http.Request, body []byte, now time.Time) {
	timestamp := uint32(now.Unix())
	queryHash := md5.Sum([]byte(request.URL.RawQuery))
	var payload [20]byte
	copy(payload[:4], queryHash[:4])
	if body != nil {
		bodyHash := md5.Sum(body)
		copy(payload[4:8], bodyHash[:4])
		request.Header.Set("X-SS-STUB", fmt.Sprintf("%X", bodyHash))
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
	request.Header.Set("X-Khronos", strconv.FormatUint(uint64(timestamp), 10))

	// 最小签名集的另两头：同一时间戳的两种字节序 base64（缺任一头即被服务端判空）。
	var stamp [4]byte
	binary.LittleEndian.PutUint32(stamp[:], timestamp)
	request.Header.Set("X-Argus", base64.StdEncoding.EncodeToString(stamp[:]))
	binary.BigEndian.PutUint32(stamp[:], timestamp)
	request.Header.Set("X-Ladon", base64.StdEncoding.EncodeToString(stamp[:]))

	request.Header.Set("X-SS-Req-Ticket", strconv.FormatInt(now.UnixMilli(), 10))
}
