import 'dart:async';

import 'package:flutter/material.dart';

import 'cinema_sections.dart';
import 'cinema_tokens.dart';
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
  // 推荐 tag 下的三档子项（+178 收敛为两路，+179 新增第三路，+180 调整命名与默认）：
  //   - feed_behavior「个性推荐」：+179 的两段式 tab/v?cr=1028 + cell/change/v，
  //     翻页回传已见剧集与最近曝光，无需登录。**默认子项**。
  //   - feed「公开推荐」：+149 起的匿名 tab/v1，不带任何身份。
  //   - feed_personal「账号推荐（实验）」：tab/v1 + 红果账号 Cookie。+178 实测
  //     服务端未按账号画像调序，与公开推荐内容一致，故降级为实验档并置于末位。
  // 【+180 改名沿革】feed_behavior 原显示「行为推荐」，现改叫「个性推荐」并设为
  // 默认；「个性推荐」这个旧名原属 feed_personal，现改为「账号推荐（实验）」。
  // 两个 id 与 Go 侧常量一律不变——它们是跨 Dart / Go 的字符串协议，没有编译期
  // 检查，改名只动显示名与默认项，不动 id、不动取流逻辑。
  // 原「真人剧 / 漫剧 / AI 剧」三个子项走 landpage，对匿名设备返回空 body、
  // 只能靠官网兜底，且与分类页内容重复，故移除（+178）。
  // 注意：这几个 id 必须与 Go 侧 hongguoPersonalFeedGenre /
  // hongguoFeedGenre / hongguoCellFeedGenre 逐字一致。
  static const _genres = [
    CatalogCategory('feed_behavior', '个性推荐'),
    CatalogCategory('feed', '公开推荐'),
    CatalogCategory('feed_personal', '账号推荐（实验）'),
  ];
  final _scroll = ScrollController();
  String _genre = 'feed_behavior';
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

  /// [+190] 三档推荐模式：原本是第二行 ChoiceChip，现收编为标题行里的
  /// 文字超链接按钮，点击弹菜单，当前档打勾。passthrough 让外层
  /// PopupMenuButton 接管点击（见 cinema_sections.dart 注释）；tooltip 传空串
  /// 避免两层 Tooltip 抢手势。
  Widget _genreMenu() => PopupMenuButton<String>(
    key: const ValueKey('recommend-genre-menu'),
    tooltip: '切换推荐模式',
    onSelected: _select,
    itemBuilder: (_) => [
      for (final genre in _genres)
        PopupMenuItem<String>(
          value: genre.id,
          child: Row(
            children: [
              Expanded(child: Text(genre.name)),
              if (genre.id == _genre)
                const Icon(Icons.check_rounded, size: 18),
            ],
          ),
        ),
    ],
    child: CinemaTextLink(
      label: _genreLabel,
      icon: Icons.expand_more_rounded,
      tooltip: '',
      emphasis: true,
      passthrough: true,
    ),
  );

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
        // [+190] 首页 IA：原本的第二行三档 chips 与两行说明文字收编为
        // 「为你推荐」标题行里的「档位文字按钮 + 换一批文字链接」，首页不再
        // 出现双行筛选。独立推荐页没有这个标题行语境，档位与刷新都放到
        // AppBar（见下面的 Scaffold 分支）。
        if (widget.embedded)
          CinemaSectionHeader(
            title: '为你推荐',
            actions: [
              _genreMenu(),
              CinemaTextLink(
                label: '换一批',
                icon: Icons.refresh_rounded,
                tooltip: '换一批推荐',
                onTap: _loading || _more ? null : () => _load(force: true),
              ),
            ],
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
                          padding: const EdgeInsets.all(CinemaTokens.pageGutter),
                          sliver: SliverGrid(
                            gridDelegate: dramaGridDelegate(
                              context,
                              constraints.maxWidth -
                                  2 * CinemaTokens.pageGutter,
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
      appBar: AppBar(
        title: const Text('红果推荐'),
        actions: [_genreMenu(), refresh],
      ),
      body: SafeArea(top: false, child: content),
    );
  }
}
