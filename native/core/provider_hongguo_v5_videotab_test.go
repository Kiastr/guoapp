package core

// bodyless v5 GET video_tab 回退的离线单测。
//
// 背景：2026-11-20 服务端加固，匿名 POST 播放门返回 200 空体；上游 jackotom@92df757ed /
// tsukiyo@dbe677e1c 实测带 v5 签名的 GET video_tab 仍可出模型。这里按项目传统做纯函数
// 字节级对拍：query 参数顺序、bodyless 头集合（无 x-ss-stub/Content-*）、响应归一两种
// data 形状。网络出口 hgDrpyRequestJSON 强制 https + fqnovel.com 白名单，无法 httptest，
// 故不打真实请求；请求体的正确性收敛到可单测的 query/headers 两个纯函数上。

import (
	"crypto/md5"
	"strconv"
	"strings"
	"testing"
)

// 固定身份，仅为可复现的字节断言，非真实凭据。
func hgV5TestIdentity() hongguoAnonDevice {
	return hongguoAnonDevice{
		DeviceID:  "1234567890",
		InstallID: "9876543210",
		CDID:      "test-cdid-v5-tab",
	}
}

// TestHgV5VideoTabQueryOrder 校验 query 参数集与顺序：身份/画像在前，业务字段居中，
// _rticket 收尾（与 jackotom 的 {...values, _rticket} 逐字节一致）。签名绑定 query
// 原文，顺序错一位就会导致 x-medusa 不匹配、服务端静默空体。
func TestHgV5VideoTabQueryOrder(t *testing.T) {
	got := hgV5VideoTabQuery("DROP2024", hgV5TestIdentity(), 1700000000123)
	want := [][2]string{
		{"aid", "8662"},
		{"app_name", "novelread"},
		{"version_code", "74332"},
		{"version_name", "7.4.3.32"},
		{"channel", "vivo_8662_64"},
		{"device_platform", "android"},
		{"device_type", "PGT-AN10"},
		{"os_version", "12"},
		{"update_version_code", "74332"},
		{"iid", "9876543210"},
		{"cdid", "test-cdid-v5-tab"},
		{"device_id", "1234567890"},
		{"video_id", "DROP2024"},
		{"video_id_type", "0"},
		{"device_level", "1"},
		{"need_all_video_definition", "1"},
		{"need_mp4_align", "0"},
		{"use_os_player", "0"},
		{"video_platform", "1024"},
		{"source", "0"},
		{"from_video_id", ""},
		{"_rticket", "1700000000123"},
	}
	if len(got) != len(want) {
		t.Fatalf("query 字段数 = %d，期望 %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项 = %v，期望 %v", i, got[i], want[i])
		}
	}
	// _rticket 必须最后一项：x-khronos 与它同源，顺序变动会破坏签名。
	if last := got[len(got)-1]; last[0] != "_rticket" {
		t.Fatalf("_rticket 不在末尾: %v", last)
	}
}

// TestHgV5VideoTabQueryEncoded 校验编码后的 query 原文：video_id / from_video_id 等
// 按 pythonUrlEncode（保留 * ~，空格等按 %XX 大写）输出，确保签名输入与发送一致。
func TestHgV5VideoTabQueryEncoded(t *testing.T) {
	encoded := hgDrpyPythonURLEncode(hgV5VideoTabQuery("123456", hgV5TestIdentity(), 1700000000456))
	if !strings.HasPrefix(encoded, "aid=8662&app_name=novelread&version_code=74332") {
		t.Fatalf("query 前缀不对: %s", encoded)
	}
	if !strings.HasSuffix(encoded, "&source=0&from_video_id=&_rticket=1700000000456") {
		t.Fatalf("query 后缀不对: %s", encoded)
	}
	if strings.Contains(encoded, " ") || strings.Contains(encoded, "+") {
		t.Errorf("query 出现未编码空格或加号: %s", encoded)
	}
}

// hgV5TestValidTicket 抖动出满足 branch==12 的合法 ticket（签名头生成的隐含契约）。
// 生产路径由 hgV5VideoTabFetchAt 的循环保证；单测直调纯函数时必须自行满足，
// 否则 hashF13 返回空导致 hgV5Medusa 访问 f13[0] panic。
func hgV5TestValidTicket(t *testing.T, videoID string, identity hongguoAnonDevice, now int64) int64 {
	t.Helper()
	ts := uint32(now / 1000)
	emptyMD5 := md5.Sum(nil)
	tsLE := hgDrpyLe32(ts)
	for offset := int64(0); offset < 1024; offset++ {
		ticket := now + offset
		query := hgDrpyPythonURLEncode(hgV5VideoTabQuery(videoID, identity, ticket))
		qsm3 := hgDrpySM3([]byte(query))
		iv := hgDrpyGetIv(hgDrpyGetIv(hgDrpyGetIv(0x20230928, qsm3[:]), emptyMD5[:]), tsLE[:])
		if iv&15 == 12 {
			return ticket
		}
	}
	t.Fatal("1024 次抖动内未找到合法 ticket")
	return 0
}

// TestHgV5VideoTabSignedHeadersBodyless 校验 bodyless 头集合：
// 必须带 x-khronos/x-helios/x-medusa/x-ss-req-ticket；必须**不带** x-ss-stub
// 与 Content-Type/Content-Encoding——后两者是 gzip POST 专用，GET 带上会被判伪造。
func TestHgV5VideoTabSignedHeadersBodyless(t *testing.T) {
	identity := hgV5TestIdentity()
	ticket := hgV5TestValidTicket(t, "DROP2024", identity, 1700000000000)
	ts := uint32(ticket / 1000)
	fullURL := hgV5VideoTabEndpoint + "?" + hgDrpyPythonURLEncode(hgV5VideoTabQuery("DROP2024", identity, ticket))
	who := hgV5IdentityMap(identity)

	headers := hgV5VideoTabSignedHeaders(fullURL, ticket, ts, who)

	for _, key := range []string{"x-khronos", "x-helios", "x-medusa", "x-ss-req-ticket", "User-Agent", "Accept"} {
		if value, ok := headers[key]; !ok || value == "" {
			t.Errorf("缺签名/必需头 %s", key)
		}
	}
	if got := headers["x-khronos"]; got != strconv.FormatUint(uint64(ts), 10) {
		t.Errorf("x-khronos = %s，应与 ticket/1000 = %d 同源", got, ts)
	}
	if got := headers["x-ss-req-ticket"]; got != strconv.FormatInt(ticket, 10) {
		t.Errorf("x-ss-req-ticket = %s，期望 %d", got, ticket)
	}
	for _, key := range []string{"x-ss-stub", "Content-Type", "Content-Encoding", "content-type", "content-encoding"} {
		if _, ok := headers[key]; ok {
			t.Errorf("bodyless GET 不该带 %s", key)
		}
	}
}

// TestHgV5NormalizeVideoTabShapes 校验响应归一的两种 data 形状：
// 新返回 data.video_model_data.<id> 与旧/单集 data.<id>，都要归一成 player 的
// data[videoID].video_model，交回原解析链零改动复用。
func TestHgV5NormalizeVideoTabShapes(t *testing.T) {
	const videoID = "DROP2024"
	model := map[string]any{"video_list": []any{map[string]any{"main_url": "https://cdn.example/x.mp4"}}}
	cases := []struct {
		name     string
		response map[string]any
	}{
		{
			name: "video_model_data 包裹",
			response: map[string]any{"code": 0, "data": map[string]any{
				"video_model_data": map[string]any{videoID: map[string]any{"video_model": model}},
			}},
		},
		{
			name:     "data 直挂 id",
			response: map[string]any{"Code": 0, "data": map[string]any{videoID: map[string]any{"video_model": model}}},
		},
		{
			// video_model_data 里还夹了别的集：必须按 id 精确取，不误取首个。
			name: "多集按 id 精确取",
			response: map[string]any{"code": 0, "data": map[string]any{
				"video_model_data": map[string]any{
					"OTHER": map[string]any{"video_model": map[string]any{"tag": "wrong"}},
					videoID: map[string]any{"video_model": model},
					"THIRD": map[string]any{"video_model": map[string]any{"tag": "wrong2"}},
				},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := hgV5NormalizeVideoTab(tc.response, videoID)
			if err != nil {
				t.Fatalf("归一失败: %v", err)
			}
			if hgDrpyResponseCode(got) != 0 {
				t.Fatalf("归一后业务码非 0: %v", got["code"])
			}
			data, _ := got["data"].(map[string]any)
			entry, _ := data[videoID].(map[string]any)
			if entry == nil {
				t.Fatalf("归一后缺 data[%s]: %v", videoID, got)
			}
			gotModel, _ := entry["video_model"].(map[string]any)
			if gotModel == nil {
				t.Fatalf("归一后缺 video_model: %v", entry)
			}
			if _, ok := gotModel["video_list"]; !ok {
				t.Fatalf("取错集（video_list 丢失）: %v", gotModel)
			}
		})
	}
}

// TestHgV5NormalizeVideoTabRejects 校验拒绝路径：业务码非 0、空 data、id 不在其中
// 且有多于一个候选（多集预告误取防护），都要返回错误让换号编排接手。
func TestHgV5NormalizeVideoTabRejects(t *testing.T) {
	const videoID = "DROP2024"
	cases := []struct {
		name     string
		response map[string]any
	}{
		{"业务码非 0", map[string]any{"code": 110001, "data": map[string]any{}}},
		{"空 data", map[string]any{"code": 0}},
		{"data 无该 id 且多候选", map[string]any{"code": 0, "data": map[string]any{
			"A": map[string]any{"video_model": map[string]any{}},
			"B": map[string]any{"video_model": map[string]any{}},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := hgV5NormalizeVideoTab(tc.response, videoID); err == nil {
				t.Fatal("期望返回错误，实际成功")
			}
		})
	}
}

// TestHgV5NormalizeVideoTabSingleFallback 单集时 data 里只有一个候选且 id 不匹配，
// 允许回退取该唯一候选（兼容服务端只回一个别名 id 的情形）。
func TestHgV5NormalizeVideoTabSingleFallback(t *testing.T) {
	const videoID = "DROP2024"
	response := map[string]any{"code": 0, "data": map[string]any{
		"ALIAS": map[string]any{"video_model": map[string]any{"video_list": []any{}}},
	}}
	got, err := hgV5NormalizeVideoTab(response, videoID)
	if err != nil {
		t.Fatalf("单集回退失败: %v", err)
	}
	data, _ := got["data"].(map[string]any)
	if _, ok := data[videoID].(map[string]any); !ok {
		t.Fatalf("单集回退后缺 data[%s]: %v", videoID, got)
	}
}
