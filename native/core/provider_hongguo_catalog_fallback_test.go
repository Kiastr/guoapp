package core

import "testing"

// TestHongguoWebFallbackRoutes 锁住「分类键 → 官网分类页」的映射。
//
// 这条映射的来由：App 的 landpage 接口对匿名设备返回空 body 后，剧库要靠官网
// 分类页兜底；此前只写了 real-drama，导致漫剧 / AI 剧页直接报错。任何人
// 日后调整分类键都必须同步这里，否则又会出现「某一类永远空白」。
//
// 空分类（「全部」）必须是三路：App 接口正常时「全部」本来就是真人剧 + 漫剧 +
// AI 剧三路聚合，兜底若只给真人剧一页 24 部，列表会骤减，看起来像「刷新后
// 内容反而变少」。
func TestHongguoWebFallbackRoutes(t *testing.T) {
	cases := []struct {
		category string
		want     []string // route 序列
	}{
		{"", []string{"real-drama", "comic-drama", "ai-drama"}},
		{"short_play", []string{"real-drama"}},
		{"comic_series", []string{"comic-drama"}},
		{"ai_series", []string{"ai-drama"}},
		{"unknown", nil},
	}
	for _, item := range cases {
		routes := hongguoWebFallbackRoutes(item.category)
		if len(routes) != len(item.want) {
			t.Fatalf("category=%q 得到 %d 路（%v），期望 %d 路", item.category, len(routes), routes, len(item.want))
		}
		for index, want := range item.want {
			if routes[index].route != want {
				t.Fatalf("category=%q 第 %d 路 = %q，期望 %q", item.category, index, routes[index].route, want)
			}
			if routes[index].name == "" {
				t.Fatalf("category=%q 第 %d 路缺少中文名", item.category, index)
			}
		}
	}
}
