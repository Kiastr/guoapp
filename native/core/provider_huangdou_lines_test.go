package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// huangdouPlayResponse 构造一个符合黄豆 /api/drama/play 加密协议的夹具响应：
// 从请求的 requestId 派生 key（与 huangdouKey 一致），把 payload gzip 后 AES-CBC 加密，
// iv 前置。这样 resolveHuangdouPlayback 能像真实环境一样解密出 lines。
func huangdouPlayResponse(t *testing.T, request *http.Request, payload map[string]any) *http.Response {
	t.Helper()
	rid := request.Header.Get("requestId")
	key, err := huangdouKey(rid)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(payload)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write(plain)
	_ = zw.Close()
	iv := make([]byte, aes.BlockSize) // 固定 iv 便于测试（真实环境随机）
	cipherText, err := aesCBCEncrypt(gz.Bytes(), key, iv)
	if err != nil {
		t.Fatal(err)
	}
	body := append(append([]byte{}, iv...), cipherText...)
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request}
}

func TestResolveHuangdouPlayback_LinesToVariants(t *testing.T) {
	const host = "https://hd.example.test"
	keyHex := "8d10a11438ad91271f64824ded8fdd86"
	payload := map[string]any{
		"status":   "y",
		"m3u8":     host + "/api/drama/hls/abc/1/play.m3u8?line=free&sig=s1",
		"hls_key":  keyHex,
		"duration": 392,
		"lines": []any{
			map[string]any{"name": "free", "url": host + "/api/drama/hls/abc/1/play.m3u8?line=free&sig=s1"}, // 与主 URL 重复，应去重
			map[string]any{"name": "line2", "url": host + "/api/drama/hls/abc/1/play.m3u8?line=line2&sig=s2"},
			map[string]any{"name": "line3", "url": host + "/api/drama/hls/abc/1/play.m3u8?line=line3&sig=s3"},
			map[string]any{"name": "bad", "url": "ftp://not-http/play.m3u8"}, // 非 http，应过滤
		},
	}
	d := sourceFixtureDownloader(t, sourceFixtureTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/drama/play" {
			return huangdouPlayResponse(t, req, payload), nil
		}
		return sourceFixtureResponse(req, 404, ""), nil
	}))
	client := &huangdouAPIClient{d: d, host: host, sessionID: "sess", deviceID: "dev"}
	media, err := d.resolveHuangdouPlayback(context.Background(), client, "abc", 1)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(media.Variants) != 2 {
		t.Fatalf("期望 2 条备用线路（line2/line3，去重+过滤后），实际 %d: %+v", len(media.Variants), media.Variants)
	}
	if media.Referer != host+"/home" {
		t.Fatalf("主线路 referer 错误: %s", media.Referer)
	}
	if len(media.HLSKey) != 16 {
		t.Fatalf("主线路 key 应为 16 字节，实际 %d", len(media.HLSKey))
	}
	wantURLs := map[string]bool{
		host + "/api/drama/hls/abc/1/play.m3u8?line=line2&sig=s2": true,
		host + "/api/drama/hls/abc/1/play.m3u8?line=line3&sig=s3": true,
	}
	for _, v := range media.Variants {
		if !wantURLs[v.URL] {
			t.Fatalf("意外线路: %s", v.URL)
		}
		delete(wantURLs, v.URL)
		if len(v.HLSKey) != 16 || !bytes.Equal(v.HLSKey, media.HLSKey) {
			t.Fatalf("备用线路未共用 hls_key: %s", v.URL)
		}
		if v.Referer != media.Referer {
			t.Fatalf("备用线路 referer 不一致: %s", v.URL)
		}
		if v.Duration != media.Duration {
			t.Fatalf("备用线路 duration 不一致: %s", v.URL)
		}
	}
}

func TestResolveHuangdouPlayback_NoLines(t *testing.T) {
	const host = "https://hd.example.test"
	payload := map[string]any{
		"status": "y",
		"m3u8":   host + "/api/drama/hls/abc/1/play.m3u8?line=free&sig=s1",
	}
	d := sourceFixtureDownloader(t, sourceFixtureTransport(func(req *http.Request) (*http.Response, error) {
		return huangdouPlayResponse(t, req, payload), nil
	}))
	client := &huangdouAPIClient{d: d, host: host, sessionID: "s", deviceID: "d"}
	media, err := d.resolveHuangdouPlayback(context.Background(), client, "abc", 1)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(media.Variants) != 0 {
		t.Fatalf("无 lines 时不应有 Variants，实际 %d", len(media.Variants))
	}
}
