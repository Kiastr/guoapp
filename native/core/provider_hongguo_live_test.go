package core

// 真实端点回归测试：主页目录 + drpys / app 两条取流线路。
//
// 存在意义：2026-10-30 服务端同时改了两处策略，任何一处回归都会让用户看到「主页空白」
// 或「无法播放」，而单测（httptest 夹具）无法发现：
//  1. 内容接口按 hashF13 分桶，只有 branch 0 返回数据，1/2 一律 HTTP 200 空 body；
//  2. 取流要求 device_register 注册过的设备身份，且单集端点下线、只剩批量端点。
//
// 运行：CHECK_LIVE_PROVIDERS=true go test ./core/ -run TestLiveHongguoPublicRecovery -v
// 注意：内嵌 video id 属某部公开短剧的首集，可能随剧集下架失效；失效时换任意可访问
// 剧集的视频 ID 即可。

import (
	"context"
	"os"
	"testing"
)

func TestLiveHongguoPublicRecovery(t *testing.T) {
	if os.Getenv("CHECK_LIVE_PROVIDERS") != "true" {
		t.Skip("设置 CHECK_LIVE_PROVIDERS=true 才会请求真实端点")
	}
	ctx := context.Background()
	const videoID = "7685285364566592536"
	resetHongguoAnonPool()
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatalf("初始化引擎失败: %v", err)
	}
	downloader := engine.downloader

	catalog, err := engine.nativeCatalog(ctx, nativeInput{Source: sourceHongguo, Page: 1})
	if err != nil {
		t.Fatalf("主页目录失败（branch 抖动可能失效）: %v", err)
	}
	if len(catalog.Items) == 0 {
		t.Fatalf("主页目录返回 0 条（branch 未命中 0 或接口变更），warning=%q", catalog.Warning)
	}
	t.Logf("主页目录成功 items=%d 首条=%q", len(catalog.Items), catalog.Items[0].Title)

	drpy, err := downloader.resolveHongguoDrpyMedia(ctx, videoID, "auto")
	if err != nil {
		t.Fatalf("drpys 取流失败（身份或 branch 策略可能变化）: %v", err)
	}
	if drpy.URL == "" {
		t.Fatal("drpys 取流返回空地址")
	}
	t.Logf("drpys 取流成功 variants=%d", len(drpy.Variants))

	// 全程无登录态：app 线路走 hongguoAnonPlayback 匿名设备注册，
	// 这里成功即证明它不依赖登录（曾一度被误标为「必须登录」，见 routes.go 注释）。
	app, err := downloader.resolveHongguoAppMedia(ctx, videoID)
	if err != nil {
		t.Fatalf("app 线路取流失败（批量端点或注册身份策略可能变化）: %v", err)
	}
	if app.URL == "" {
		t.Fatal("app 线路返回空地址")
	}
	t.Logf("app 线路成功 variants=%d", len(app.Variants))
}
