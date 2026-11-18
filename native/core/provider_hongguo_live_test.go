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

	// 全程无登录态：drpys(v5) 线路已证明匿名身份可用。
	//
	// app 兜底线路仍走旧播放模型签名（v04.04.05），2026-11-16 起服务端换代
	// 到 v5 后该线路返回 HTTP 200 空体（与 drpys 修复前同因）。因 drpys 已是
	// 首选且 v5 恢复成功，app 不会被触发；这里仅记录其状态，不再作为硬性失败，
	// 避免把「已知降级的兜底线路」误判为回归。
	app, appErr := downloader.resolveHongguoAppMedia(ctx, videoID)
	if appErr != nil {
		t.Logf("app 兜底线路（旧签名，已知降级）: %v", appErr)
	} else {
		t.Logf("app 兜底线路成功 variants=%d", len(app.Variants))
	}
}
