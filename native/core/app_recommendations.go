package core

import (
	"context"
	"errors"
	"time"
)

type nativeRecommendationState struct {
	Query   hongguoRecommendationQuery `json:"query"`
	Page    int                        `json:"page"`
	HasMore bool                       `json:"hasMore"`
}

func nativeRecommendationKey(genre string) string { return sourceHongguo + "|recommendation:" + genre }

func (engine *nativeEngine) cachedRecommendations(genre string) (nativeCatalogResult, error) {
	if err := (hongguoRecommendationQuery{Genre: genre}).validate(); err != nil {
		return nativeCatalogResult{}, err
	}
	result := engine.nativeCached(nativeRecommendationKey(genre))
	engine.mu.Lock()
	_, found := engine.recommendations[genre]
	engine.mu.Unlock()
	if !found && len(result.Items) > 0 {
		result.Fresh, result.HasMore = false, false
		result.Warning = joinNativeWarnings(result.Warning, "推荐位置已失效，请刷新推荐")
	}
	return result, nil
}

func (engine *nativeEngine) nativeRecommendations(ctx context.Context, input nativeInput) (nativeCatalogResult, error) {
	genre := input.Category
	if err := (hongguoRecommendationQuery{Genre: genre}).validate(); err != nil {
		return nativeCatalogResult{}, err
	}
	unlock, err := engine.lockSourceCatalog(ctx, sourceHongguo)
	if err != nil {
		return nativeCatalogResult{}, err
	}
	defer unlock()
	if retried, err := engine.retryCatalogSave(); retried {
		cached, _ := engine.cachedRecommendations(genre)
		if err != nil || len(cached.Items) > 0 {
			return cached, nil
		}
	}
	cached, _ := engine.cachedRecommendations(genre)
	engine.mu.Lock()
	state, found := engine.recommendations[genre]
	engine.mu.Unlock()
	if !input.Force && found && len(cached.Items) > 0 && (input.Command != "more" || !state.HasMore) {
		return cached, nil
	}
	if !input.Force && !found && len(cached.Items) > 0 {
		return cached, nil
	}
	// [feed] 推荐流的「刷新」= 换一批，不是回到头部。若照搬分类页的
	// 「force 即重置游标」，用户下拉刷新拿到的永远是首批那 3 部——这正是
	// 「刷新后还是原来的剧」的复现。这里保留 offset/session/seen 继续往后取。
	feed := isHongguoFeedGenre(genre)
	if (input.Force || !found) && !(feed && found) {
		state = nativeRecommendationState{Query: hongguoRecommendationQuery{Genre: genre}, HasMore: true}
	}
	// [+178] 个性推荐把账号 Cookie 送进 tab/v1。这里实时读登录态：Cookie
	// 只作为函数参数在内存里传递，绝不进入 state.Query（那个结构会落盘，
	// 见 provider_hongguo_feed.go 里的说明）。未登录时 cookie 为空，请求
	// 退化成匿名公开流，并在结果里给一条提示，不让默认子项开天窗。
	cookie := ""
	if genre == hongguoPersonalFeedGenre {
		session := engine.hongguoLoginLoad()
		if hongguoLoginHasSession(session.Cookies) {
			cookie = hongguoAccountCookieHeader(session.Cookies)
		}
	}
	page, err := engine.downloader.fetchHongguoRecommendations(ctx, state.Query, cookie)
	if err != nil {
		// 推荐兜底到官网首页四个板块（热播短剧 / 真人剧 / 漫剧 / AI 剧，
		// 共 36 部，字段与分类页同构）。
		//
		// 【为什么不写游标】推荐是带 offset + session + seen 的流，官网首页是
		// 固定的 36 部、没有下一页。把兜底结果写进 recommendations 状态，下一轮
		// 「加载更多」就会拿着一个对不上的游标去请求 App 接口。这里只返回结果、
		// 不落游标，下次刷新重新走「先试 App 接口，空了再兜底」。
		//
		// 【为什么清掉旧警告】不清的话，源状态面板会一直挂着上一条「红果 App
		// 接口返回空内容」——而用户对匿名设备门禁无能为力（换注册身份也空），
		// 兜底既然给了真实在架内容，就该让状态回到干净态。
		if fallback, fallbackErr := engine.downloader.fetchHongguoWebHomeSections(ctx); fallbackErr == nil && len(fallback) > 0 {
			engine.mu.Lock()
			known := make(map[string]nativeDrama, len(engine.catalogs[sourceHongguo]))
			for _, drama := range engine.catalogs[sourceHongguo] {
				known[drama.ID] = drama
			}
			state := engine.catalogStates[nativeRecommendationKey(genre)]
			state.Warning = ""
			state.UpdatedAt = time.Now()
			engine.catalogStates[nativeRecommendationKey(genre)] = state
			engine.mu.Unlock()
			items := make([]nativeDrama, 0, len(fallback))
			for _, drama := range fallback {
				item := nativeNormalize(drama)
				if old, exists := known[item.ID]; exists {
					item = mergeNativeDrama(old, item)
				}
				items = append(items, item)
			}
			return nativeCatalogResult{Items: items, Page: 1, Fresh: true}, nil
		}
		if len(cached.Items) == 0 || errors.Is(err, context.Canceled) {
			return nativeCatalogResult{}, err
		}
		cached.Fresh = false
		cached.Warning = joinNativeWarnings(cached.Warning, publicError(err).Error())
		return cached, nil
	}
	if err := ctx.Err(); err != nil {
		return nativeCatalogResult{}, err
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.recommendations == nil {
		engine.recommendations = map[string]nativeRecommendationState{}
	}
	key := nativeRecommendationKey(genre)
	previous := engine.catalogs[key]
	// 分类页刷新是「重看这一屏」，清空旧列表；推荐流刷新是「再来一批」，
	// 新剧追加在后面，不能把已看到的先清掉。
	if input.Force && !feed {
		previous = nil
	}
	seen := make(map[string]bool, len(previous))
	for _, drama := range previous {
		seen[drama.ID] = true
	}
	fresh := make([]nativeDrama, 0, len(page.Dramas))
	known := make(map[string]nativeDrama, len(engine.catalogs[sourceHongguo]))
	for _, drama := range engine.catalogs[sourceHongguo] {
		known[drama.ID] = drama
	}
	for _, drama := range page.Dramas {
		item := nativeNormalize(drama)
		if old, exists := known[item.ID]; exists {
			item = mergeNativeDrama(old, item)
		}
		if !seen[item.ID] {
			seen[item.ID] = true
			fresh = append(fresh, item)
		}
	}
	if len(fresh) == 0 && page.HasMore {
		return nativeCatalogResult{}, errors.New("暂时没有新的推荐，已保留上次位置；可重试或刷新推荐")
	}
	items := mergeNativeCatalog(previous, fresh)
	identifiers := make([]string, 0, min(540, len(items)))
	for _, drama := range items[max(0, len(items)-540):] {
		identifiers = append(identifiers, drama.ID)
	}
	session := page.SessionID
	if session == "" {
		session = state.Query.SessionID
	}
	state.Query = hongguoRecommendationQuery{Genre: genre, Offset: page.NextOffset, SessionID: session, Seen: identifiers}
	state.Page++
	state.HasMore = page.HasMore
	engine.recommendations[genre] = state
	engine.catalogs[key] = items
	engine.catalogs[sourceHongguo] = mergeNativeCatalog(engine.catalogs[sourceHongguo], fresh)
	engine.hongguoCatalog = engine.downloader.hongguoCatalogSnapshot()
	engine.catalogStates[key] = nativeCatalogState{UpdatedAt: time.Now(), Page: state.Page, HasMore: state.HasMore}
	result := nativeCatalogResult{Items: append([]nativeDrama{}, items...), Page: state.Page, HasMore: state.HasMore, Fresh: true}
	if genre == hongguoPersonalFeedGenre && cookie == "" {
		result.Warning = joinNativeWarnings(result.Warning, "未登录红果账号，当前是公开推荐；在设置里扫码登录后可启用个性化")
	}
	if err := engine.writeCatalogDiskLocked(); err != nil {
		result.Fresh, result.Warning, result.saveError = false, err.Error(), err
	}
	return result, nil
}
