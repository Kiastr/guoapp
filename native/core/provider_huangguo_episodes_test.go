package core

import "testing"

// 视频页的真实结构（摘自线上页面）：
// 选集区在 `data-web-play-ep-grid` 里，只有当前这部剧的每一集；
// 页面上其余 `/video/<id>/` 链接来自「相关推荐 / 猜你喜欢 / 排行榜」。
const huangguoVideoPageFixture = `<html><body>
<div class="hg-web-play__side">
  <div class="hg-web-play__ep-grid" data-web-play-ep-grid>
    <a class="hg-web-play__ep is-active" href="/video/10586/" data-ep-id="1">01</a>
    <a class="hg-web-play__ep" href="/video/10586/ep-2/" data-ep-id="2">02</a>
  </div>
</div>
<section class="hg-recommend">
  <a href="/video/11130/" title="别的剧">别的剧</a>
  <a href="/video/6147/" title="另一部剧">另一部剧</a>
  <a href="/detail/758/" title="详情链接">详情链接</a>
</section>
<footer><a href="/video/6905/">页脚推荐</a></footer>
</body></html>`

func TestHuangguoEpisodesIgnoreRecommendedDramas(t *testing.T) {
	episodes := parseHuangguoAIEpisodes(huangguoVideoPageFixture, "https://huangguoai.com/detail/10586/", "10586")
	if len(episodes) != 2 {
		t.Fatalf("期望 2 集（推荐剧必须被过滤），实际 %d 集：%+v", len(episodes), episodes)
	}
	if episodes[0].Index != 1 || episodes[1].Index != 2 {
		t.Fatalf("集数应来自 data-ep-id（1、2），实际 %d、%d", episodes[0].Index, episodes[1].Index)
	}
	if episodes[0].URL != "https://huangguoai.com/video/10586/" {
		t.Fatalf("第 1 集 URL 不对：%s", episodes[0].URL)
	}
	if episodes[1].URL != "https://huangguoai.com/video/10586/ep-2/" {
		t.Fatalf("第 2 集 URL 不对：%s", episodes[1].URL)
	}
}

// 没有选集容器（模板改版）时，仍要靠视频 ID 过滤掉推荐剧。
func TestHuangguoEpisodesFilterByVideoIDWithoutGrid(t *testing.T) {
	page := `<html><body>
<a href="/video/10586/" title="第1集">第1集</a>
<a href="/video/10586/ep-2/" title="第2集">第2集</a>
<a href="/video/10586/ep-3/" title="第3集">第3集</a>
<a href="/video/11130/" title="推荐A">推荐A</a>
<a href="/video/6147/" title="推荐B">推荐B</a>
</body></html>`
	episodes := parseHuangguoAIEpisodes(page, "https://huangguoai.com/detail/10586/", "10586")
	if len(episodes) != 3 {
		t.Fatalf("期望 3 集，实际 %d 集：%+v", len(episodes), episodes)
	}
	for _, episode := range episodes {
		if want := "https://huangguoai.com/video/10586/"; len(episode.URL) < len(want) || episode.URL[:len(want)] != want {
			t.Fatalf("混入了非本剧的链接：%s", episode.URL)
		}
	}
}

// detail ID 与 video ID 不是同一套时，过滤会把结果清空；
// 此时应退回旧的全页行为，而不是返回空选集。
func TestHuangguoEpisodesFallBackWhenIDsDiffer(t *testing.T) {
	page := `<html><body>
<a href="/video/10586/" title="第1集">第1集</a>
<a href="/video/10586/ep-2/" title="第2集">第2集</a>
</body></html>`
	episodes := parseHuangguoAIEpisodes(page, "https://huangguoai.com/detail/99999/", "99999")
	if len(episodes) == 0 {
		t.Fatal("ID 体系不一致时不应返回空选集")
	}
	if len(episodes) != 2 {
		t.Fatalf("兜底应拿到 2 集，实际 %d 集", len(episodes))
	}
}

// 选集按钮不带 data-ep-id 时，仍按标题里的数字定集数。
func TestHuangguoEpisodesIndexFromTitle(t *testing.T) {
	page := `<html><body>
<div class="hg-web-play__ep-grid" data-web-play-ep-grid>
  <a href="/video/10586/ep-11/">第11集</a>
  <a href="/video/10586/ep-12/">第12集</a>
</div>
</body></html>`
	episodes := parseHuangguoAIEpisodes(page, "https://huangguoai.com/detail/10586/", "10586")
	if len(episodes) != 2 {
		t.Fatalf("期望 2 集，实际 %d 集", len(episodes))
	}
	if episodes[0].Index != 11 || episodes[1].Index != 12 {
		t.Fatalf("集数解析应为 11、12，实际 %d、%d", episodes[0].Index, episodes[1].Index)
	}
}
