package core

import (
	"context"
	"os"
	"testing"
)

// 真实端点验证：App 取流是否返回可用媒体地址（只取地址，不下载）。
//
// 存在意义：2026-10-16 定位到「红果接口拒绝了播放请求 / 未返回有效数据」的真因是
// 我方 X-Gorgon 被服务端判为签名校验失败（带 X-Gorgon → 200 空 body；不发 → 正常）。
// 本测试用于回归确认：一旦服务端策略变化（恢复校验、要求完整六头签名等）能第一时间发现。
//
// 运行：CHECK_LIVE_PROVIDERS=true go test ./core/ -run TestLiveHongguoAppPlayback -v
// 注意：内嵌 video id 属某部公开短剧首集，可能随剧集下架失效；失效时换成任意
// 可访问剧集的视频 ID 即可。
func TestLiveHongguoAppPlayback(t *testing.T) {
	if os.Getenv("CHECK_LIVE_PROVIDERS") != "true" {
		t.Skip("设置 CHECK_LIVE_PROVIDERS=true 才会请求真实端点")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	engine.downloader.cfg.Retries = 1
	engine.downloader.limiter = newRequestLimiter(20, 0)

	media, err := engine.downloader.resolveHongguoAppMedia(context.Background(), "7685285364566592536")
	if err != nil {
		t.Fatalf("App 取流失败（可能签名策略变化或该集已下架）: %v", err)
	}
	if media.URL == "" {
		t.Fatal("App 取流返回了空媒体地址")
	}
	t.Logf("取流成功 quality=%d variants=%d url=%s", media.Quality, len(media.Variants), media.URL)
}
