package core

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveHanimeCatalogRankingDetailPlayback 是本机联网冒烟测试，默认跳过。
//
// 沙箱 / CI 的出口 IP 会被 hanime1.me 按地区挡掉（Cloudflare 403），所以
// 只能由能打开该站点的机器手动验证：
//
//	cd native && CHECK_LIVE_PROVIDERS=true go test ./core -run TestLiveHanime -v
//
// 覆盖「列表 → 榜单 → 详情/系列 → 取流」四条链路，任一环节解析失效都会在
// 这里直接暴露，而不是等到装包后才发现站点改版。
func TestLiveHanimeCatalogRankingDetailPlayback(t *testing.T) {
	if os.Getenv("CHECK_LIVE_PROVIDERS") != "true" {
		t.Skip("set CHECK_LIVE_PROVIDERS=true to touch hanime1.me")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	d := engine.downloader

	items, more, err := d.fetchHanimeCatalogPage(ctx, 1, "", "")
	if err != nil || len(items) == 0 {
		t.Fatalf("列表失败：count=%d more=%t err=%v", len(items), more, err)
	}
	t.Logf("列表 ok：%d 部，hasMore=%t，首条=%s", len(items), more, items[0].Title)

	board, found := findRankingBoard("hanime-daily")
	if !found {
		t.Fatal("本日排行榜单未注册")
	}
	ranking, err := d.loadRankingPage(ctx, board, 1, true)
	if err != nil || len(ranking.Items) == 0 {
		t.Fatalf("榜单失败：count=%d err=%v", len(ranking.Items), err)
	}
	t.Logf("榜单 ok：%d 条", len(ranking.Items))

	var lastErr error
	for index, drama := range items {
		if index >= 5 {
			break
		}
		_, chapters, err := d.fetchHanimeDetailChapters(ctx, drama.SourceID)
		if err != nil || len(chapters) == 0 {
			lastErr = err
			continue
		}
		media, err := d.resolveProviderMedia(ctx, Task{DramaID: drama.ID, DramaTitle: drama.DisplayTitle(), Chapter: chapters[0], Index: 1})
		if err != nil {
			lastErr = err
			continue
		}
		choice := nativePlaybackChoices(media, 0)
		if len(choice.media) == 0 {
			lastErr = err
			continue
		}
		t.Logf("详情/取流 ok：%s 分集=%d 线路=%d 最高清晰度=%d 地址=%s",
			drama.ID, len(chapters), len(choice.media), media.Quality, media.URL)
		return
	}
	t.Fatalf("没有可取流的样本，最后错误：%v", lastErr)
}
