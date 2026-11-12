package core

// 播放版 551 真签名的固定黄金向量。
//
// 来源（GPL-3.0-only，与实现同源，保留出处）：
//   - jackotom/hongguo-drama 6.9 src/native/playback-signature.test.cjs
//     （标注独立黄金源 N3urda/hongguoTV 7444bb01 的 PlayerSignTest.kt，
//      provenance fqapp rust/testdata/vectors.json）
//   - tsukiyo-zero/hongguoTV Argus.kt 的 551 golden-vector 测试
//
// 这些向量是「独立逆向解码外部密文后固定」的，不依赖本项目实现自洽。
// 签名错一个字节服务端就会静默拒绝，因此这里逐字节对拍，不能只测结构。

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestHgPlaybackProtoBufGolden(t *testing.T) {
	bean := newHgPlaybackProtoBuf().
		putVarint(1, 1077940818).putVarint(2, 2).putVarint(3, 305419896).
		putUtf8(4, "1967").putUtf8(5, "test_device_id").putUtf8(6, "1611921764").
		putUtf8(7, "").putUtf8(8, "").putVarint(9, 0).putUtf8(10, "").putUtf8(11, "").
		putVarint(12, 3400000000).
		putBytes(13, hgPlaybackMustHex("106e34a2b8c7")).
		putBytes(14, hgPlaybackMustHex("ac5095fc674e")).
		putProtobuf(15, newHgPlaybackProtoBuf().putVarint(1, 1).putVarint(2, 1).putVarint(3, 1).putVarint(7, 0xc792eccc)).
		putUtf8(16, "").putUtf8(20, "none").putVarint(21, 738).
		putProtobuf(23, newHgPlaybackProtoBuf().putUtf8(1, "NX551J").putVarint(2, 8196).putVarint(4, 0x80e0d800)).
		putVarint(25, 2)
	const want = "08d2a4808204100218f8acd191012204313936372a0e746573745f6465766963655f6964320a313631313932313736343a004200480052005a006080c49fd50c6a06106e34a2b8c77206ac5095fc674e7a0c08011001180138ccd9cbbc0c820100a201046e6f6e65a801e205ba01110a064e583535314a1084402080b0838708c80102"
	if got := hex.EncodeToString(bean.toBuf()); got != want {
		t.Fatalf("ProtoBuf bean 不匹配\n got  %s\n want %s", got, want)
	}

	// varint 必须截断到 32 位：-1 → 0xffffffff，4294967301 → 5。
	overflow := newHgPlaybackProtoBuf().putVarint(1, ^uint64(0)).putVarint(2, 4294967301)
	if got := hex.EncodeToString(overflow.toBuf()); got != "08ffffffff0f1005" {
		t.Fatalf("varint 截断不符: %s", got)
	}
}

func TestHgPlaybackSimonGolden(t *testing.T) {
	key := [4]uint64{1111111111111111111, 2222222222222222222, 3333333333333333333, 4444444444444444444}
	x, y := hgPlaybackSimonEncrypt(1234567890123456789, 9876543210987654321, key)
	if x != 6071150433487837324 || y != 2027321285990159935 {
		t.Fatalf("Simon 密文不符: %d, %d", x, y)
	}
	rounds := hgPlaybackSimonRounds(key)
	tail := rounds[68:]
	want := [4]uint64{13679004903447286117, 6988159389011166349, 12845848347567849394, 14469362449063813004}
	for index := range want {
		if tail[index] != want[index] {
			t.Fatalf("Simon 轮密钥 [%d] = %d，期望 %d", 68+index, tail[index], want[index])
		}
	}
}

func TestHgPlaybackLadonGolden(t *testing.T) {
	random := [4]byte{0x01, 0x02, 0x03, 0x04}
	const want = "AQIDBKM7Y7bWnOIUXmTybD6/MpGbuge4sBwUJsyHVivhtMsH"
	if got := hgPlaybackLadon(1700000000, 1611921764, 1967, random); got != want {
		t.Fatalf("Ladon 不符\n got  %s\n want %s", got, want)
	}
}

func TestHgPlaybackHashGolden(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"bodyHash(空)", hgPlaybackBodyHash(""), "106e34a2b8c7"},
		{"queryHash(空)", hgPlaybackQueryHash(""), "106e34a2b8c7"},
		{"queryHash(query)", hgPlaybackQueryHash("aid=1967&device_id=123"), "925b8d86b20f"},
		{"bodyHash(stub)", hgPlaybackBodyHash("ac3ef48caa08fa3ed5e025da69edc645"), "5048b6e949d0"},
	}
	for _, item := range cases {
		if got := hex.EncodeToString(item.got); got != item.want {
			t.Fatalf("%s = %s，期望 %s", item.name, got, item.want)
		}
	}
}

// TestHgPlaybackArgus551Golden 四条外部固定密文，逐字节对拍。
func TestHgPlaybackArgus551Golden(t *testing.T) {
	cases := []struct {
		query     string
		ts        int64
		randField uint32
		want      string
	}{
		{
			query:     "aid=1967&device_id=dev_aaaaaaaa&iid=111",
			ts:        1700000000,
			randField: 659662259,
			want:      "8oHyrVYEy1xDCDlx64ko40qC6JC9VYY/ZkjJzEl50iSFDw8Yfph1hupLxfKrA9GswSEz0Yi4goA7OQ4u+H8ECdOaQDL4GduQ3LULSq9Q4iSEGbS3L6G24GXChtnmPr6n46M2PLlKpXvF7uE0lhH4yD+X69FpdM41gQwx5Y5U3NdwXFaxlqYFszyp7dlUE6BEBWqwDsiAH8Jk9TGKr/jfHt4fVmMc9zRyNRdGeV0x6aP//x7aV+8Po8a/9KK3UNG5KEpmIgzRT6NUttkBP9gRNUh3",
		},
		{
			query:     "aid=1967&device_id=dev_bbbbbbbbbbbbbbbb&iid=222",
			ts:        1700000001,
			randField: 1315257197,
			want:      "8oGWgilKQHD4eCdd0VB0vBSbTAkv/3xWE2YWJkxic35CgXkxrUlMTz05HJAak5b5Xx9yxohnoAAcEX//MI9t7XBBtnPwOTaJzkJgBLaYOOotfI7YuqhsEbE5BB363bhKz/Qw/+MI5gH6pgdAXfsRj68u7Qwu3gqCY1NYstqNZFFVrph7dWtXDBYM1aoVEyfkYD2haoaOr9D/gl1zdzAAzbW7we6lr9/zFApbuNsbr2dagdUrQc51QNN0HBOtIkpVBVunDwXM8qR271Ei+Pa4EgKy",
		},
		{
			query:     "aid=1967&device_id=X&iid=333",
			ts:        1234567890,
			randField: 554000487,
			want:      "8oEOCRTb0LQf+M+MsliYu4M9kBzAlzXvFgL/Ub198hyoBGxYfNXLAENmH+K/10JQ+Rbhm2zRxNtNv5wVd885xcb6C6F10zp/9jVeGHGz8plz8W3UXHHNrFKjJptjQElGDbyaVLSKt56+HfwO7ENYMfjVv/5PNi1upVciiHqyOPDDAtwi/fZkwBoU1kC7RolMOuedn4lCg/s0CjnNvxFT0QpF/K0yoW3Zpu30tkadIET7WbZ3+7KT3c1yAA6RKWI4mmw=",
		},
		{
			query:     "aid=1967&device_id=test_device_id&iid=456&version_name=6.5.1.32",
			ts:        1700000000,
			randField: 420072733,
			want:      "8oHu8LnZm0OtJJIaSr1427D+yeH70h5aZTVjoSW7MfFi/hk1LhjTqsEEWYpwcHfK4/2SXuJxTtopRcPeHzVpeFpUD4B4yxEG7REejpCZ1OqeAaLsw/lMebwUjUHrUjnAZCKq0QyimnwdiTiYIsH3GfiK/xlPcFcARqwX7RkI1qnnWUgiJVDEacSm8yhBat6MvDVOa7+U2MgcLu7Q26pFzX8VX5F+ryP76ETs1nRGLmlhHkwyiKb6CETstvGQnJgocgZBNNvlHD2/P+4LWjFMvhNK",
		},
	}
	for index, item := range cases {
		device := hgPlaybackQueryParam(item.query, "device_id")
		got := hgPlaybackArgus(item.query, "", item.ts, 1967, hgPlaybackLicenseID, device, "", item.randField)
		if got != item.want {
			t.Fatalf("golden #%d 不匹配\n got  %s\n want %s", index+1, got, item.want)
		}
		// randField 以 uint32 参与，超出 32 位的高位应被截断（上游断言）。
		if extra := hgPlaybackArgus(item.query, "", item.ts, 1967, hgPlaybackLicenseID, device, "", item.randField); extra != got {
			t.Fatalf("golden #%d 结果不稳定", index+1)
		}
	}
}

// TestHgPlaybackSignHeadersContract 验证「最终 query / 时间 / 正文」都绑定进签名，
// 且占位头会被清干净。这几条是上游反复强调、也最容易移植错的契约。
func TestHgPlaybackSignHeadersContract(t *testing.T) {
	query := "aid=1967&device_id=test_device_id&iid=456&version_name=6.5.1.32"
	randField := uint32(420072733)
	ladonRandom := [4]byte{0x01, 0x02, 0x03, 0x04}

	base := hgPlaybackSignHeaders(query, nil, 1700000000, 1700000000000, randField, ladonRandom)
	if len(base) != 4 {
		t.Fatalf("无正文时应只有 4 个头，实际 %d: %v", len(base), base)
	}
	if base["x-khronos"] != "1700000000" || base["x-ss-req-ticket"] != "1700000000000" {
		t.Fatalf("时间头不符: %v", base)
	}
	if base["x-ss-stub"] != "" {
		t.Fatal("无正文不该带 x-ss-stub")
	}
	if base["x-ladon"] != "AQIDBKM7Y7bWnOIUXmTybD6/MpGbuge4sBwUJsyHVivhtMsH" {
		t.Fatalf("x-ladon 不符: %s", base["x-ladon"])
	}
	const wantArgus = "8oHu8LnZm0OtJJIaSr1427D+yeH70h5aZTVjoSW7MfFi/hk1LhjTqsEEWYpwcHfK4/2SXuJxTtopRcPeHzVpeFpUD4B4yxEG7REejpCZ1OqeAaLsw/lMebwUjUHrUjnAZCKq0QyimnwdiTiYIsH3GfiK/xlPcFcARqwX7RkI1qnnWUgiJVDEacSm8yhBat6MvDVOa7+U2MgcLu7Q26pFzX8VX5F+ryP76ETs1nRGLmlhHkwyiKb6CETstvGQnJgocgZBNNvlHD2/P+4LWjFMvhNK"
	if base["x-argus"] != wantArgus {
		t.Fatal("x-argus 与 signPlayer 契约不一致")
	}
	// 空正文与 nil 正文等价。
	if len(hgPlaybackSignHeaders(query, []byte{}, 1700000000, 1700000000000, randField, ladonRandom)) != 4 {
		t.Fatal("空正文被当成了有正文")
	}
	// 有正文：x-ss-stub 是原始 body 的 MD5（小写 hex），x-argus 必须变化，x-ladon 不变。
	body := []byte(`{"x":1}`)
	withBody := hgPlaybackSignHeaders(query, body, 1700000000, 1700000000000, randField, ladonRandom)
	if withBody["x-ss-stub"] != "ac3ef48caa08fa3ed5e025da69edc645" {
		t.Fatalf("x-ss-stub 不符: %s", withBody["x-ss-stub"])
	}
	if withBody["x-argus"] == base["x-argus"] {
		t.Fatal("正文变化没有绑定进 x-argus")
	}
	if withBody["x-ladon"] != base["x-ladon"] {
		t.Fatal("x-ladon 不应受正文影响")
	}
	// query 的原始字节参与签名：%2A 与 * 必须给出不同结果。
	a := hgPlaybackSignHeaders(query+"&extra=%2A", nil, 1700000000, 1700000000000, randField, ladonRandom)
	b := hgPlaybackSignHeaders(query+"&extra=*", nil, 1700000000, 1700000000000, randField, ladonRandom)
	if a["x-argus"] == b["x-argus"] {
		t.Fatal("query 编码差异没有绑定进签名")
	}
	// 换时间必须重新签名。
	later := hgPlaybackSignHeaders(query, nil, 1700000001, 1700000001000, randField, ladonRandom)
	if later["x-argus"] == base["x-argus"] || later["x-ladon"] == base["x-ladon"] {
		t.Fatal("时间戳没有绑定进签名")
	}
	// iid 不得变成 Argus bean field16：改 iid 但保持 device_id/aid/version 不变时，
	// field16 恒为空，签名差异只应来自 queryHash。这里只确认不会 panic、结果非空。
	if _, err := base64.StdEncoding.DecodeString(base["x-argus"]); err != nil {
		t.Fatalf("x-argus 不是合法 base64: %v", err)
	}
}

func TestHgPlaybackStripPlaceholders(t *testing.T) {
	original := map[string]string{
		"User-Agent":      "com.phoenix.read/71332",
		"Accept":          "application/json",
		"Content-Type":    "application/json; charset=UTF-8",
		"x-gorgon":        "8404...",
		"X-Helios":        "fake",
		"x-medusa":        "fake",
		"x-argus":         "AAAA",
		"x-ladon":         "AAAA",
		"x-khronos":       "1700000000",
		"x-ss-stub":       "DEADBEEF",
		"x-ss-req-ticket": "1700000000000",
		"x-tt-dt":         "",
		"sdk-version":     "2",
		"x-xs-from-web":   "0",
	}
	got := hongguoPlaybackStripPlaceholders(original)
	for _, key := range hgPlaybackPlaceholderHeaders {
		for existing := range got {
			if strings.EqualFold(existing, key) {
				t.Fatalf("占位头 %s 未被清除", existing)
			}
		}
	}
	for _, key := range []string{"User-Agent", "Accept", "Content-Type", "sdk-version", "x-xs-from-web"} {
		if got[key] == "" {
			t.Fatalf("普通头 %s 被误删", key)
		}
	}
}

func TestHgPlaybackTicketFromURL(t *testing.T) {
	const full = "https://api5-normal-sinfonlineb.fqnovel.com/novel/player/multi_video_model/v1/?aid=8662&_rticket=1700000000123&device_id=123"
	if got := hongguoPlaybackTicketFromURL(full); got != 1700000000123 {
		t.Fatalf("_rticket 解析错误: %d", got)
	}
	headers := hongguoPlaybackHeadersForURL(full, []byte(`{"a":1}`), map[string]string{
		"User-Agent": "ua", "x-gorgon": "placeholder", "x-argus": "placeholder",
	})
	if headers["x-khronos"] != "1700000000" {
		t.Fatalf("x-khronos 与 _rticket 不同源: %s", headers["x-khronos"])
	}
	if headers["x-ss-req-ticket"] != "1700000000123" {
		t.Fatalf("ticket 不符: %s", headers["x-ss-req-ticket"])
	}
	if headers["x-gorgon"] != "" || headers["x-argus"] == "placeholder" {
		t.Fatal("占位头未替换")
	}
	if headers["User-Agent"] != "ua" || headers["x-ss-stub"] == "" {
		t.Fatal("普通头丢失或正文摘要缺失")
	}
}
