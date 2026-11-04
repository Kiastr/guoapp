import 'dart:async';

import 'package:flutter/material.dart';

import 'catalog_filters.dart';
import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';
import 'playback_launch_screen.dart';
import 'widgets.dart';

class RecommendationsScreen extends StatefulWidget {
  const RecommendationsScreen({
    super.key,
    required this.repository,
    required this.store,
    this.embedded = false,
  });
  final AppRepository repository;
  final LocalStore store;
  final bool embedded;

  @override
  State<RecommendationsScreen> createState() => _RecommendationsScreenState();
}

class _RecommendationsScreenState extends State<RecommendationsScreen> {
  // [+178] 推荐 tag 下的子项收敛为两路，都走 bookmall/tab/v1：
  //   - feed_personal「个性推荐」：请求带红果账号 Cookie，服务端有机会按
  //     账号画像出内容（Go 侧 hongguoPersonalFeedGenre）；**默认子项**。
  //   - feed「公开推荐」：保持 +149 起的匿名请求，不带任何身份。
  // 原「真人剧 / 漫剧 / AI 剧」三个子项走 landpage，对匿名设备返回空 body、
  // 只能靠官网兜底，且与分类页内容重复，故移除。
  // 注意：'feed_personal' 必须与 Go 侧 hongguoPersonalFeedGenre 逐字一致，
  // 它是跨 Dart / Go 的字符串协议，没有编译期检查。
  static const _genres = [
    CatalogCategory('feed_personal', '个性推荐'),
    CatalogCategory('feed', '公开推荐'),
  ];
  final _scroll = ScrollController();
  String _genre = 'feed_personal';
  List<Drama> _items = [];
  bool _loading = false;
  bool _more = false;
  bool _hasMore = true;
  bool _failedMore = false;
  String? _error;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    widget.repository.catalogUpdates.addListener(_metadataChanged);
    _load();
  }

  @override
  void dispose() {
    _generation++;
    widget.repository.catalogUpdates.removeListener(_metadataChanged);
    unawaited(widget.repository.cancelRecommendations());
    _scroll.dispose();
    super.dispose();
  }

  void _metadataChanged() {
    final drama = widget.repository.catalogUpdates.latest;
    if (!mounted || drama == null) return;
    setState(() {
      _items = [
        for (final item in _items)
          item.id == drama.id ? item.merge(drama) : item,
      ];
    });
  }

  Future<void> _load({bool more = false, bool force = false}) async {
    if (more && (_loading || _more || !_hasMore)) return;
    final generation = ++_generation;
    final genre = _genre;
    setState(() {
      _loading = !more;
      _more = more;
      _error = null;
      _failedMore = more;
    });
    try {
      await widget.repository.cancelRecommendations();
      if (!mounted || generation != _generation) return;
      final page = await widget.repository.recommendations(
        genre,
        more: more,
        force: force,
      );
      if (!mounted || generation != _generation) return;
      setState(() {
        _items = page.items;
        _hasMore = page.hasMore;
        _error = page.warning.isEmpty ? null : page.warning;
        _loading = _more = false;
      });
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _loading = _more = false;
        _error = error.toString();
      });
    }
  }

  String get _genreLabel {
    for (final genre in _genres) {
      if (genre.id == _genre) return genre.name;
    }
    return '推荐';
  }

  void _select(String genre) {
    if (_genre == genre) return;
    setState(() {
      _genre = genre;
      _items = [];
      _hasMore = true;
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    _load();
  }

  @override
  Widget build(BuildContext context) {
    final refresh = RefreshAction(
      loading: _loading || _more,
      tooltip: '刷新推荐',
      onPressed: () => _load(force: true),
    );
    final content = Column(
      children: [
        CatalogFilters(
          categories: _genres,
          category: _genre,
          onCategory: _select,
          onRetry: () => _load(force: true),
          trailing: widget.embedded ? refresh : null,
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
          child: Align(
            alignment: Alignment.centerLeft,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  '$_genreLabel · 已加载 ${_items.length} 部',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                // [+178] app 内引导：个性推荐依赖登录态，未登录会退化成匿名流，
                // 不说明的话用户会以为两栏内容没区别（Go 侧另有 warning 提示）。
                if (_genre == 'feed_personal')
                  Text(
                    '基于红果账号的推荐；未登录时与公开推荐相同',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                  ),
              ],
            ),
          ),
        ),
        if (_loading && _items.isNotEmpty)
          const LinearProgressIndicator(minHeight: 2),
        if (_error != null && _items.isNotEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    _error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
                TextButton(
                  onPressed: _loading || _more
                      ? null
                      : () => _load(more: _failedMore, force: !_failedMore),
                  child: const Text('重试'),
                ),
              ],
            ),
          ),
        Expanded(
          child: !widget.store.allowsSource('hongguo')
              ? const StatusPanel(title: '当前用户未开放红果', message: '可在用户管理中调整站源权限。')
              : _loading && _items.isEmpty
              ? const Center(child: CircularProgressIndicator())
              : _items.isEmpty
              ? StatusPanel(
                  title: _error == null ? '暂无推荐' : '推荐暂时无法加载',
                  message: _error ?? '稍后刷新可获取新的推荐。',
                  onRetry: () => _load(force: true),
                )
              : LayoutBuilder(
                  builder: (context, constraints) => RefreshIndicator(
                    onRefresh: () => _load(force: true),
                    child: CustomScrollView(
                      controller: _scroll,
                      physics: const AlwaysScrollableScrollPhysics(),
                      slivers: [
                        SliverPadding(
                          padding: const EdgeInsets.all(16),
                          sliver: SliverGrid(
                            gridDelegate: dramaGridDelegate(
                              context,
                              constraints.maxWidth - 32,
                            ),
                            delegate: SliverChildBuilderDelegate((
                              context,
                              index,
                            ) {
                              final drama = _items[index];
                              return DramaTile(
                                key: ValueKey(drama.id),
                                drama: drama,
                                repository: widget.repository,
                                onTap: () => unawaited(
                                  openPlaybackDirectly(
                                    context,
                                    drama: drama,
                                    repository: widget.repository,
                                    store: widget.store,
                                  ),
                                ),
                              );
                            }, childCount: _items.length),
                          ),
                        ),
                        SliverToBoxAdapter(
                          child: Padding(
                            padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
                            child: Center(
                              child: _more
                                  ? const CircularProgressIndicator()
                                  : _hasMore
                                  ? OutlinedButton.icon(
                                      onPressed: () => _load(more: true),
                                      icon: const Icon(
                                        Icons.auto_awesome_rounded,
                                      ),
                                      label: const Text('继续推荐'),
                                    )
                                  : TextButton(
                                      onPressed: () => _load(force: true),
                                      child: const Text('本轮推荐已看完，刷新获取新推荐'),
                                    ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
        ),
      ],
    );
    if (widget.embedded) return content;
    return Scaffold(
      appBar: AppBar(title: const Text('红果推荐'), actions: [refresh]),
      body: SafeArea(top: false, child: content),
    );
  }
}
