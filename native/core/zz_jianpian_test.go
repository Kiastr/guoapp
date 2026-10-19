package core

import (
	"context"
	"os"
	"testing"
)

// 用真实 荐片 脚本（rule 驱动型）跑一遍分类 / 目录 / 详情。
func TestZZJianpianLive(t *testing.T) {
	script, err := os.ReadFile("/tmp/jp2.js")
	if err != nil {
		t.Skip("no script")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	result, err := engine.importDrpyScript(context.Background(), string(script))
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}
	t.Logf("导入: %v", result)
	key := result.(map[string]any)["key"].(string)
	site, _ := engine.downloader.drpy.get("drpy:" + key)
	categories, err := engine.downloader.fetchDrpyCategories(context.Background(), site)
	if err != nil {
		t.Fatalf("分类失败: %v", err)
	}
	t.Logf("分类 %d: %v", len(categories), categories)
	if len(categories) == 0 {
		t.Fatal("分类为空")
	}
	page, more, err := engine.downloader.fetchDrpyCatalogPage(context.Background(), site, 1, categories[0].ID, "")
	if err != nil {
		t.Fatalf("目录失败: %v", err)
	}
	t.Logf("目录 %d 部 hasMore=%v", len(page), more)
	if len(page) == 0 {
		t.Fatal("目录为空")
	}
	t.Logf("第一部: %s / %s / %s", page[0].Title, page[0].Cover, page[0].SourceID)
	detail, chapters, err := engine.downloader.fetchDrpyDetail(context.Background(), site, page[0].SourceID)
	if err != nil {
		t.Fatalf("详情失败: %v", err)
	}
	t.Logf("详情: %s 分集 %d 部", detail.Title, len(chapters))
	if len(chapters) > 0 {
		t.Logf("首集: %s -> %q", chapters[0].Title, chapters[0].VideoURL)
	}
}
