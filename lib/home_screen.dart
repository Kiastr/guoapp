import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'app_layout.dart';
import 'app_bottom_navigation.dart';
import 'app_build.dart';
import 'core_bridge.dart';
import 'catalog_filters.dart';
import 'catalog_browser.dart';
import 'catalog_sort.dart';
import 'catalog_sort_sheet.dart';
import 'cinema_icons.dart';
import 'cinema_sections.dart';
import 'cinema_tokens.dart';
import 'recommendations_screen.dart';
import 'rankings_screen.dart';
import 'detail_screen.dart';
import 'playback_launch_screen.dart';
import 'downloads_screen.dart';
import 'local_store.dart';
import 'webdav_screen.dart';
import 'models.dart';
import 'remote_widgets.dart';
import 'widgets.dart';
import 'vip_icon.dart';
import 'settings_screen.dart';
import 'profiles_screen.dart';
import 'search_input.dart';
import 'source_gate_dialog.dart';
import 'source_gate_taps.dart';
import 'site_verification.dart';
import 'site_verification_screen.dart';
import 'sources_screen.dart';
import 'batch_download_screen.dart';
import 'batch_downloads.dart';
import 'drama_actions.dart';
import 'follow_state.dart';
import 'library_updater.dart';
import 'saved_library.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.repository, required this.store});
  final AppRepository repository;
  final LocalStore store;
  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  static const _recommendationCategory = 'app:recommendations';

  /// [+159] 「重载分组」的判定阈值。
  ///
  /// 用户加了十几个站源之后，「所有站源」「扩展站源集合」这类分组一次要打
  /// 几十个并发请求，首次进入时列表可能好几秒是空的——只靠顶部那个 2px 的
  /// 细进度条完全看不出来在干活。这里把源数达到阈值的分组当成重载分组，
  /// 在内容区盖一层转圈 + 进度提示（详见 [_loadOverlay]）。
  static const _heavySourceThreshold = 10;

  /// [+159] 当前这轮目录加载的进度（已结束源数 / 待请求源数），由
  /// [CatalogBrowser.load] 的 onProgress 回传。仅用于重载分组的加载提示。
  int _loadDone = 0;
  int _loadTotal = 0;

  final _search = TextEditingController();
  final _scroll = ScrollController();
  Timer? _debounce;
  late SourceSite _source;
  bool _allSources = false;

  /// [+158] 用户在组选择器里选中的分组 id（多源分组聚合展示）。
  ///
  /// 必须是内存态而不是直接读 [LocalStore.catalogView]：切组时 setCatalogSource
  /// 是异步落盘的，setState 之后的第一帧还读不到新值，目录就会按「旧分组」
  /// 去加载（表现为点多源分组却只请求了组内首个源）。与 [_allSources] 同一
  /// 套路：先 setState 生效，再异步持久化供重建 / 重启恢复。
  String _selectedGroupId = '';

  List<Drama> _items = [];

  /// _items 的版本号：每次内容变化自增，_visible 的缓存据此失效。
  /// 只用 length 判断不够——同长度的不同内容（比如换页回来）会吃到旧缓存。
  int _itemsRevision = 0;

  /// _visible 的缓存。构建目录时这个 getter 每帧都会被访问，
  /// 上千条做一遍「过滤 + 排序」会明显卡顿。
  List<Drama>? _visibleCache;
  String _visibleSignature = '';
  bool _loading = true;
  bool _loadingMore = false;
  bool _hasMore = true;
  String? _error;
  /// 当前源的站点验证入口。仅在错误提示需要「去验证」时用到：让用户在
  /// 403 现场直接跳过去，不用自己翻到设置里找。
  SiteVerificationTarget? _verificationTarget;
  int _generation = 0;

  int _tab = 0;
  String _submittedQuery = '';
  final _categorySelections = <String, String>{};
  late final CatalogBrowser _browser;

  /// [+190] 首页搜索框常驻，不再有「展开 / 收起」状态；这里只记输入框是否
  /// 获得焦点，用来决定「最近搜索」要不要出现，避免常驻搜索框之外再常显
  /// 一行历史把首屏挤高。电视端不走页面输入框（用对话框），此值恒 false。
  bool _searchFocused = false;
  bool _categoriesLoading = false;
  String? _categoriesError;
  int _categoryGeneration = 0;
  late final LibraryUpdater _updater;
  final _changedSources = <String>{};
  final _selectedDramas = <String, Drama>{};
  Timer? _cacheRefreshTimer;
  bool _refreshingUpdatedCache = false;
  bool _selectionMode = false;
  bool _showRecommendations = false;
  final _recentTaps = RepeatTapGate();
  String _sourceSignature = '';
  bool _catalogLoadScheduled = false;

  /// _sourceGroups 的缓存：build 里 AppBar 组选择器、tag 栏 key 等处会
  /// 多次读取，每次重算 store.sources 过滤 + fromSources 分组。缓存按
  /// 「可见源 + 收藏集」签名失效，行为与重算完全一致。
  String _groupsSignature = '';
  List<SourceGroup> _groupsCache = const [];

  List<SourceGroup> get _sourceGroups {
    final signature =
        '${widget.store.favoriteSourceIds.join(',')}|'
        '${widget.store.sources.map((site) => site.id).join(',')}|'
        '${widget.store.sourceOrder.join(',')}';
    if (signature != _groupsSignature) {
      _groupsSignature = signature;
      // store.sources 已排除被隐藏的运行时片源（与黄果同逻辑）；
      // favoriteSourceIds 让收藏的片源独占一组，与红果等同级。
      // [+157] groupOrder 让用户排的顺序生效（站源管理页上移 / 下移）。
      final groups = SourceGroup.fromSources(
        widget.store.sources,
        favoriteIds: widget.store.favoriteSourceIds,
        groupOrder: widget.store.sourceOrder,
      );
      _groupsCache = [
        if (groups.length > 1) SourceGroup('all', '全部站源', widget.store.sources),
        ...groups,
      ];
    }
    return _groupsCache;
  }

  /// 当前选中源对应的分组 id：收藏的运行时片源独占一组（组 id 即源 id，
  /// 与 SourceGroup.fromSources 的 favoriteIds 一致）；未收藏的按大组 /
  /// 内置源自身。「全部站源」为特殊组。
  ///
  /// [+158] 例外：用户在组选择器里点过多源分组时，分组 id 被显式记在
  /// [_selectedGroupId]（并持久化到 [CatalogView.groupId]），直接用它——
  /// 否则组内首个源若是收藏源（青空 / 韩小圈），分组会被反推成「源 id
  /// 独占组」，点「动漫源集合」等于点「青空」。
  /// 当前源不在那个组里时（用户后来切到了组外的源）自动失效。
  String get _groupIdOfSource {
    if (_allSources) return 'all';
    final selected = _selectedGroupId;
    if (selected.isNotEmpty) {
      final group = _sourceGroups.where((g) => g.id == selected).firstOrNull;
      if (group != null && group.sources.any((s) => s.id == _source.id)) {
        return selected;
      }
    }
    return widget.store.isFavoriteSource(_source.id)
        ? _source.id
        : _source.groupId;
  }

  SourceGroup get _group {
    final id = _groupIdOfSource;
    return _sourceGroups.where((group) => group.id == id).firstOrNull ??
        SourceGroup(
          id,
          widget.store.isFavoriteSource(_source.id)
              ? _source.name
              : _source.groupName,
          [_source],
        );
  }

  bool get _onlineSearch => _group.sources.any((source) => source.onlineSearch);
  bool get _searchSuggestions =>
      _group.sources.any((source) => source.searchSuggestions);
  String get _searchHint {
    final online = _group.sources
        .where((source) => source.onlineSearch)
        .toList();
    if (online.isEmpty) {
      return '筛选本机已更新剧库';
    }
    final names = online.map((source) => source.name).join('、');
    return online.length == _group.sources.length
        ? '搜索$names'
        : '搜索$names及本机剧库';
  }

  String get _category => _categorySelections[_group.id] ?? '';
  List<CatalogCategory> get _categories => _browser.categories(_group);
  String get _displayCategory =>
      _showRecommendations ? _recommendationCategory : _category;
  List<CatalogCategory> get _displayCategories => [
    CatalogCategory.all,
    if (_group.id == 'hongguo')
      const CatalogCategory(_recommendationCategory, '推荐'),
    ..._categories.where((entry) => entry.id.isNotEmpty),
  ];

  Future<void> _loadCategories({
    bool force = false,
    bool cacheOnly = false,
  }) async {
    final generation = ++_categoryGeneration;
    final group = _group;
    setState(() {
      _categoriesLoading = true;
      _categoriesError = null;
    });
    final error = await _browser.loadCategories(
      group,
      force: force,
      cacheOnly: cacheOnly,
    );
    if (!mounted ||
        generation != _categoryGeneration ||
        group.id != _group.id) {
      return;
    }
    setState(() {
      _categoriesLoading = false;
      _categoriesError = error;
    });
    if (!_showRecommendations &&
        _category.isNotEmpty &&
        !_categories.any((entry) => entry.id == _category)) {
      _changeCategory('');
    }
  }

  Future<void> _refreshCatalog() async {
    if (widget.repository.supportsSourceManagement) {
      if (_group.sources.any((source) => _updater.busy(source.id))) return;
      _pauseCatalog();
      await _updater.update(_group.sources);
      return;
    }
    // 分类变化极少（站点自己加分类才会有），刷新时按缓存走，不当成
    // 必须重拉的网络任务——否则一次刷新要跑两轮「全部站源」的请求，
    // 几十个源时非常慢。真正想重拉分类可以切分组或重启。
    await _loadCategories();
    if (mounted) await _load(force: true);
  }

  void _updateChanged() {
    if (mounted) setState(() {});
  }

  /// 密码锁切换后可见站源会变，这里把当前站源归一化到仍然可见的站源。
  void _sourcesChanged() {
    if (!mounted) return;
    final visible = widget.store.sources;
    final signature = visible.map((site) => site.id).join(',');
    if (signature == _sourceSignature) return;
    final wasEmpty = _sourceSignature.isEmpty;
    _sourceSignature = signature;
    if (visible.isEmpty) {
      setState(() {
        _items = [];
        _itemsRevision++;
        _hasMore = false;
        _loading = false;
        _loadingMore = false;
        _stopHeavyNotice();
      });
      return;
    }
    final allowed = visible.map((site) => site.id).toSet();
    if (!wasEmpty &&
        allowed.contains(_source.id) &&
        _source.id == widget.store.source) {
      // [+160] 当前分组的源集合变大了（订阅 / 导入的源正好命中当前组）：
      // 列表还停在旧内容上，新源要等手动下拉或重新选组才会出现——而用户
      // 刚订阅完，最直觉的预期就是"新源已经在了"。这里自动补一轮，并当
      // 冷启动处理：新源没有任何缓存，这是最慢的一轮，不能走 300ms 延迟档。
      final current = {for (final site in _group.sources) site.id};
      if (current.difference(_loadedGroupIds).isEmpty) return;
      unawaited(_load(useCache: true, cold: true));
      return;
    }
    _changeSource(SourceSite.byId(widget.store.source));
  }

  void _catalogUpdated(String source) {
    if (!mounted) return;
    _changedSources.add(source);
    _cacheRefreshTimer?.cancel();
    _cacheRefreshTimer = Timer(const Duration(milliseconds: 100), () {
      unawaited(_reloadUpdatedCache());
    });
  }

  Future<void> _reloadUpdatedCache() async {
    if (_refreshingUpdatedCache) return;
    _refreshingUpdatedCache = true;
    final epoch = widget.store.profileEpoch;
    try {
      while (mounted &&
          _changedSources.isNotEmpty &&
          epoch == widget.store.profileEpoch) {
        final sources = Set.of(_changedSources);
        _changedSources.clear();
        final updates = <String, Drama>{};
        for (final source in sources) {
          if (!widget.store.allowsSource(source)) continue;
          try {
            final cached = await widget.repository.cached(source);
            for (final drama in cached.items) {
              if (widget.store.allowsSource(drama.source)) {
                updates[drama.id] = drama;
              }
            }
          } catch (error) {
            if (mounted && epoch == widget.store.profileEpoch) {
              setState(() => _error = '更新后读取缓存失败：$error');
            }
          }
        }
        if (!mounted || epoch != widget.store.profileEpoch) return;
        _browser.updateDramas(updates.values);
        // 站源任务已经改写了磁盘缓存，内存里的旧条目必须作废，否则下面
        // 的 cacheOnly 重载会跳过回读、继续展示更新前的列表。
        _browser.invalidateCached(sources);
        setState(() {
          _items = [
            for (final item in _items)
              updates[item.id] == null ? item : item.merge(updates[item.id]!),
          ];
          _itemsRevision++;
          for (final id in _selectedDramas.keys.toList()) {
            if (updates[id] != null) {
              _selectedDramas[id] = _selectedDramas[id]!.merge(updates[id]!);
            }
          }
        });
        await saveUserChange(
          context,
          () => widget.store.refreshDramas(updates.values),
        );
        if (!mounted || epoch != widget.store.profileEpoch) return;
        if (_group.sources.any((source) => sources.contains(source.id))) {
          await _loadCategories(cacheOnly: true);
          if (mounted &&
              !_loading &&
              !_loadingMore &&
              (!_onlineSearch || _search.text.trim().isEmpty)) {
            await _load(cacheOnly: true);
          }
        }
      }
    } finally {
      _refreshingUpdatedCache = false;
    }
  }

  void _changeGroup(SourceGroup group) {
    if (_group.id == group.id) return;
    // [+158] 多源分组按整组聚合展示：目录同时请求组内每个源，标题保持组名
    // （与「全部站源」同机制，先回来的源先渲染）。必须把组 id 记下来交给
    // [CatalogView.groupId]，否则分组会被「当前源」反推掉——点「动漫源集合」
    // 会落到组内首个源青空，而青空是收藏源，于是整页变成「青空」。
    //
    // 黄果组除外：它的三个源是同一站点的不同后端（视频 / AI / 旧版），
    // 聚合会让同一部剧重复出现，保持原行为（落到组内首个源）。
    // [+161] 不能拿「源数 > 1」当聚合条件：分组可能只剩一个源（Kazumi 规则
    // 被删、启动时运行时注册表还没同步完，动漫源集合就只剩青空）。这时若因为
    // length == 1 不记 groupId，集合会被「当前源」反推成青空的独占组——点
    // 「动漫源集合」实际选中青空，标题和勾选都跑掉。只要不是「全部站源」和
    // 「黄果」这两个特例，就记住分组 id；单源集合照常按集合展示。
    final aggregate = group.id != 'all' && group.id != 'huangguo';
    _changeSource(
      group.sources.first,
      allSources: group.id == 'all',
      groupId: aggregate ? group.id : '',
    );
  }

  void _changeCategory(String category) {
    if (category == _recommendationCategory && _group.id == 'hongguo') {
      if (_showRecommendations) return;
      _pauseCatalog();
      setState(() {
        _showRecommendations = true;
        _selectionMode = false;
        _selectedDramas.clear();
        _search.clear();
        _submittedQuery = '';
      });
      return;
    }
    if (_category == category && !_showRecommendations) return;
    _debounce?.cancel();
    setState(() {
      _showRecommendations = false;
      _selectionMode = false;
      _selectedDramas.clear();
      _categorySelections[_group.id] = category;
      if (_onlineSearch) {
        _search.clear();
        _submittedQuery = '';
      }
      _items = [];
      _itemsRevision++;
      _hasMore = true;
      _error = null;
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    _load(useCache: true);
  }

  void _swipeCategory(DragEndDetails details) {
    final velocity = details.primaryVelocity ?? 0;
    if (velocity.abs() < 240) return;
    final categories = _displayCategories;
    final index = categories.indexWhere(
      (entry) => entry.id == _displayCategory,
    );
    final next = index + (velocity < 0 ? 1 : -1);
    if (next >= 0 && next < categories.length) {
      _changeCategory(categories[next].id);
    }
  }

  /// [+190] 手机 / 桌面搜索框已常驻，AppBar 不再放放大镜；这个入口只在电视端
  /// 保留——遥控器无法在页面输入框里打字，仍走对话框搜索（与 +190 前一致）。
  void _toggleSearch() => unawaited(_televisionSearch());

  void _openRankings() {
    _pauseCatalog();
    Navigator.push<void>(
      context,
      MaterialPageRoute(
        builder: (_) => RankingsScreen(
          repository: widget.repository,
          store: widget.store,
          initialGroup: _group.id,
        ),
      ),
    );
  }

  Future<void> _manageSources() async {
    _pauseCatalog();
    await Navigator.push<void>(
      context,
      MaterialPageRoute(
        builder: (_) => SourcesScreen(
          repository: widget.repository,
          store: widget.store,
          initialSource: _source.id,
        ),
      ),
    );
    if (!mounted) return;
    await _loadCategories();
    if (mounted && (!_onlineSearch || _submittedQuery.isEmpty)) {
      await _load(useCache: true);
    }
  }

  Future<void> _chooseDisplayMode() async {
    final selection = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('界面模式'),
        children: [
          RadioGroup<String>(
            groupValue: widget.store.displayMode,
            onChanged: (value) => Navigator.pop(context, value),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final mode in const {
                  'auto': '自动识别设备',
                  'television': '电视 / 遥控器',
                  'standard': '手机 / 电脑',
                }.entries)
                  RadioListTile<String>(
                    value: mode.key,
                    autofocus: mode.key == widget.store.displayMode,
                    title: Text(mode.value),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
    if (selection != null && mounted) {
      await saveUserChange(
        context,
        () => widget.store.setDisplayMode(selection),
      );
    }
  }

  Future<void> _televisionSearch() async {
    final query = await showDialog<String>(
      context: context,
      builder: (_) => TelevisionSearchDialog(
        initialValue: _search.text,
        title: _group.id == 'all'
            ? '搜索已开放站源'
            : _onlineSearch
            ? '搜索${_group.name}短剧'
            : '筛选当前已加载短剧',
        recentSearches: widget.store.recentSearches,
        onCancel: () => unawaited(widget.repository.cancelSuggestions()),
        suggestions: _searchSuggestions ? widget.repository.suggestions : null,
      ),
    );
    if (query != null && mounted) {
      _submitSearch(query);
    }
  }

  void _televisionBack() {
    if (_selectionMode) {
      _cancelSelection();
    } else if (_tab != 0) {
      setState(() => _tab = 0);
    } else if (_search.text.isNotEmpty) {
      _search.clear();
      _searchChanged('');
    }
  }

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onCatalogScroll);
    _source = SourceSite.byId(widget.store.source);
    _allSources = widget.store.catalogView.allSources;
    // [+158] 上次选中的分组（多源分组聚合展示）从持久化偏好里恢复。
    _selectedGroupId = widget.store.catalogView.groupId;
    _sourceSignature = widget.store.sources.map((site) => site.id).join(',');
    // [+160] 冷启动重载的基线：首轮加载还没跑，先按当前分组要请求的源记，
    // 避免初始加载前收到一次注册表广播就被误判成"分组多了源"。
    _loadedGroupIds = {for (final site in _group.sources) site.id};
    // [+178] 红果源默认打开「推荐」tag（原来默认「全部」）。[+180] 推荐页
    // 默认子项仍叫「个性推荐」，但已换到两段式取流那一路（feed_behavior），
    // 见 recommendations_screen.dart。切到别的分组时
    // _changeSource 会把它复位为 false，回到该分组的「全部」，不影响其他源。
    // 同时把目录态的加载标记收干净：下面 _load() 在 _showRecommendations
    // 下会直接返回，不会重置 _loading / _hasMore。
    if (_group.id == 'hongguo') {
      _showRecommendations = true;
      _loading = false;
      _hasMore = false;
    }
    _browser = CatalogBrowser(
      widget.repository,
      health: widget.store.sourceHealth,
      onHealth: (source, ok, elapsedMs) =>
          widget.store.noteSourceHealth(source, ok: ok, elapsedMs: elapsedMs),
    );
    _updater = LibraryUpdater(
      widget.repository,
      widget.store,
      onCatalogChanged: _catalogUpdated,
    )..addListener(_updateChanged);
    _updater.startWatching();
    widget.store.addListener(_sourcesChanged);
    widget.repository.catalogUpdates.addListener(_metadataChanged);
    if (widget.store.sources.isNotEmpty) {
      _load(useCache: true);
      _loadCategories();
    } else {
      _loading = false;
    }
  }

  @override
  void dispose() {
    _updater.removeListener(_updateChanged);
    _updater.dispose();
    _cacheRefreshTimer?.cancel();
    widget.store.removeListener(_sourcesChanged);
    widget.repository.catalogUpdates.removeListener(_metadataChanged);
    _generation++;
    _categoryGeneration++;
    unawaited(_browser.cancel());
    unawaited(widget.repository.cancelSuggestions());
    _debounce?.cancel();
    _heavyNoticeTimer?.cancel();
    _heavyNoticeHideTimer?.cancel();
    _search.dispose();
    _scroll.removeListener(_onCatalogScroll);
    _scroll.dispose();
    super.dispose();
  }

  void _onCatalogScroll() {
    if (!mounted ||
        _showRecommendations ||
        !_hasMore ||
        _loading ||
        _loadingMore ||
        !_scroll.hasClients) {
      return;
    }
    final position = _scroll.position;
    final threshold = (position.viewportDimension * 1.5).clamp(320.0, 900.0);
    if (position.extentAfter > threshold || _catalogLoadScheduled) return;
    _catalogLoadScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _catalogLoadScheduled = false;
      if (!mounted ||
          _showRecommendations ||
          !_hasMore ||
          _loading ||
          _loadingMore ||
          !_scroll.hasClients) {
        return;
      }
      unawaited(_load(more: true));
    });
  }

  void _metadataChanged() {
    final drama = widget.repository.catalogUpdates.latest;
    if (!mounted || drama == null || !widget.store.allowsSource(drama.source)) {
      return;
    }
    _browser.updateDrama(drama);
    setState(() {
      _items = [
        for (final item in _items)
          item.id == drama.id ? item.merge(drama) : item,
      ];
      _itemsRevision++;
    });
    unawaited(saveUserChange(context, () => widget.store.refreshDrama(drama)));
  }

  Future<void> _load({
    bool more = false,
    bool useCache = false,
    bool force = false,
    bool cacheOnly = false,
    // [+160] 冷启动重载：当前分组刚多了源（订阅 / 导入），这批源没有任何
    // 缓存，必然会走满网络请求。即便列表里还有旧内容，也要像首次进组那样
    // **立刻**给出加载提示——300ms 延迟档正好错过最卡的开头。
    bool cold = false,
  }) async {
    if (_showRecommendations) return;
    if (more && (_loading || _loadingMore || !_hasMore)) return;
    final generation = ++_generation;
    final group = _group;
    // 记录本轮实际请求的源集合，供 _sourcesChanged 判断"当前分组是否多了源"。
    _loadedGroupIds = {for (final site in group.sources) site.id};
    final query = _onlineSearch ? _search.text.trim() : '';
    setState(() {
      _error = null;
      if (query.isNotEmpty) _categorySelections[group.id] = '';
      if (more) {
        _loadingMore = true;
      } else {
        _loading = true;
        _loadingMore = false;
        if (query != _submittedQuery) _items = [];
        // 「已响应 x / N」按本轮实际要请求的源数起步；加载器随后会用真实
        // 的请求集（扣掉健康度跳过的死源）覆盖它。
        _loadDone = 0;
        _loadTotal = group.sources.length;
        if (_heavyGroup) {
          // 列表还空着（首次进组 / 刚换组）就立刻给反馈；已经有内容时延迟
          // 300ms 再出现——秒开的刷新不该闪一下遮罩。冷启动重载例外：新订阅
          // 的源没有缓存，这一轮一定慢，直接立刻出现。
          _startHeavyNotice(immediate: _items.isEmpty || cold);
        } else {
          _stopHeavyNotice();
        }
      }
    });
    void accept(CatalogPage result, {bool cached = false}) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _items = result.items;
        _itemsRevision++;
        _hasMore = result.hasMore;
        _submittedQuery = query;
        _loading = cached && !result.fresh;
        _loadingMore = false;
        _error = result.warning.isEmpty ? null : result.warning;
        // 只在这一轮真的结束（不是"先读缓存、网络还在跑"）时才收起提示：
        // 缓存阶段通常几十毫秒，卡顿是后面几十个网络请求造成的。
        if (!_loading) _stopHeavyNotice();
      });
      if (result.warning.contains('浏览器验证')) {
        unawaited(_refreshVerificationTarget());
      }
      unawaited(
        saveUserChange(context, () => widget.store.refreshDramas(result.items)),
      );
    }

    try {
      final result = await _browser.load(
        group,
        category: _category,
        query: query,
        more: more,
        useCache: useCache,
        force: force,
        cacheOnly: cacheOnly,
        onCached: (result) => accept(result, cached: true),
        onProgress: (done, total) {
          if (!mounted || generation != _generation) return;
          // 绝大多数源已经响应 → 立刻撤掉提示，别让一两个不响应的源把界面
          // 继续挡到 12 秒上限。延迟档的 300ms 计时器也一并取消：那种情况
          // 下遮罩还没出现，不取消就会在撤掉之后再闪一下。
          final releasing =
              _heavyNoticeReached(done, total) &&
              (_heavyNotice ||
                  (_heavyNoticeTimer?.isActive ?? false) ||
                  (_heavyNoticeHideTimer?.isActive ?? false));
          if (done == _loadDone && total == _loadTotal && !releasing) return;
          if (releasing) {
            _heavyNoticeTimer?.cancel();
            _heavyNoticeHideTimer?.cancel();
          }
          setState(() {
            _loadDone = done;
            _loadTotal = total;
            if (releasing) _heavyNotice = false;
          });
        },
        onPartial: (result) {
          // 流式刷新：某个源先回来了就先把它渲染出来，其余源陆续补齐。
          //
          // 这里**只更新列表、不写库**——accept() 里的 refreshDramas 会做
          // 一次全量序列化（实测百毫秒级），每批数据都调一次会让 UI 再次卡住。
          // 最终结果仍由下面的 accept() 统一写库。
          if (!mounted || generation != _generation) return;
          if (result.items.isEmpty) return;
          setState(() {
            _items = result.items;
            _itemsRevision++;
            _hasMore = result.hasMore;
            _loading = true;
            _loadingMore = false;
          });
        },
      );
      accept(result);
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _loading = false;
        _loadingMore = false;
        _error = error.toString();
        _stopHeavyNotice();
      });
      // 403 现场给「去站点验证」入口，省得用户自己翻到设置里找。
      if (_error!.contains('浏览器验证')) {
        unawaited(_refreshVerificationTarget());
      }
    }
  }

  /// 拉取当前源是否有可用的站点验证入口。
  ///
  /// 只在报错时按需调用：这个接口要走一次 native 桥，首页正常加载不该为
  /// 它多付一次往返。
  Future<void> _refreshVerificationTarget() async {
    final source = _source.id;
    if (!widget.repository.supportsSourceManagement) {
      if (_verificationTarget != null) {
        setState(() => _verificationTarget = null);
      }
      return;
    }
    try {
      final view = await widget.repository.siteVerifications();
      if (!mounted) return;
      SiteVerificationTarget? target;
      for (final item in view.targets) {
        if (item.source == source) {
          target = item;
          break;
        }
      }
      setState(() => _verificationTarget = target);
    } catch (_) {
      // 取不到入口不影响主流程：错误面板退化成只有「站源诊断」。
    }
  }

  /// 从错误面板直接打开站点验证页，验证成功回来自动重载这一组。
  Future<void> _openSiteVerification(SiteVerificationTarget target) async {
    final changed = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (_) =>
            SiteVerificationScreen(repository: widget.repository, target: target),
      ),
    );
    if (changed == true && mounted) _load(force: true);
  }

  /// 当前错误是否属于「去验证一下就能好」的那类。
  ///
  /// 只认挑战页（文案带「浏览器验证」）。IP 封禁（1020）验证救不了，给了
  /// 按钮只会把用户引到一条走不通的路上——那种情况要换节点。
  bool get _errorNeedsVerification =>
      _verificationTarget != null &&
      (_error?.contains('浏览器验证') ?? false);

  Widget? get _errorSecondaryActions {
    final Widget? diagnose = widget.repository.supportsSourceManagement
        ? TextButton(onPressed: _manageSources, child: const Text('站源诊断'))
        : null;
    if (!_errorNeedsVerification) return diagnose;
    return Wrap(
      alignment: WrapAlignment.center,
      spacing: 8,
      runSpacing: 4,
      children: [
        FilledButton.tonalIcon(
          onPressed: () => _openSiteVerification(_verificationTarget!),
          icon: const Icon(Icons.verified_user_outlined),
          label: const Text('去站点验证'),
        ),
        ?diagnose,
      ],
    );
  }

  /// [groupId] 见 [_changeGroup]：多源分组传组 id 让首页按整组聚合，
  /// 单源分组 / 全部站源传空串回到「按当前源反推」；不传表示保持现状。
  void _changeSource(
    SourceSite source, {
    bool allSources = false,
    String? groupId,
  }) {
    final nextGroupId = groupId ?? _selectedGroupId;
    if (_source.id == source.id &&
        _allSources == allSources &&
        nextGroupId == _selectedGroupId) {
      return;
    }
    _debounce?.cancel();
    _search.clear();
    setState(() {
      _showRecommendations = false;
      _selectionMode = false;
      _selectedDramas.clear();
      _source = source;
      _allSources = allSources;
      _selectedGroupId = nextGroupId;
      _items = [];
      _itemsRevision++;
      _hasMore = true;
      _submittedQuery = '';
      _error = null;
      // 换源后旧源的验证入口必须作废：否则错误面板会拿 A 站的凭据入口
      // 去验证 B 站。
      _verificationTarget = null;
    });
    unawaited(
      saveUserChange(
        context,
        () => widget.store.setCatalogSource(
          source.id,
          allSources: allSources,
          groupId: nextGroupId,
        ),
      ),
    );
    if (_scroll.hasClients) {
      _scroll.jumpTo(0);
    }
    _load(useCache: true);
    _loadCategories();
  }

  void _searchChanged(String query) {
    _debounce?.cancel();
    _selectionMode = false;
    _selectedDramas.clear();
    if (_onlineSearch && (_loading || _loadingMore)) {
      _generation++;
      unawaited(_browser.cancel());
      _loading = _loadingMore = false;
    }
    setState(() {});
    if (_onlineSearch && query.trim().isEmpty) {
      _debounce = Timer(const Duration(milliseconds: 300), () => _load());
    }
  }

  void _submitSearch(String query) {
    if (_showRecommendations) {
      _showRecommendations = false;
      _categorySelections[_group.id] = '';
    }
    _selectionMode = false;
    _selectedDramas.clear();
    _search.text = query.trim();
    _debounce?.cancel();
    if (_search.text.isNotEmpty) {
      unawaited(
        saveUserChange(
          context,
          () => widget.store.rememberSearch(_search.text),
        ),
      );
    }
    if (_onlineSearch) {
      _load();
    } else {
      setState(() {});
    }
  }

  Future<void> _chooseCatalogView() async {
    final selected = await chooseCatalogView(context, widget.store.catalogView);
    if (selected != null && mounted) {
      await saveUserChange(
        context,
        () => widget.store.setCatalogView(selected),
      );
      if (mounted && _scroll.hasClients) _scroll.jumpTo(0);
    }
  }

  void _openDrama(Drama drama, {bool resume = false, bool download = false}) {
    _pauseCatalog();
    if (download) {
      Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => DetailScreen(
            drama: drama,
            repository: widget.repository,
            store: widget.store,
            downloadOnOpen: true,
          ),
        ),
      );
      return;
    }
    unawaited(
      openPlaybackDirectly(
        context,
        drama: drama,
        repository: widget.repository,
        store: widget.store,
      ),
    );
  }

  void _changeTab(int tab) => setState(() {
    _tab = tab;
    _selectionMode = false;
    _selectedDramas.clear();
  });

  /// 连点「最近观看」6 次弹出站源密码锁（用于启用 / 关闭密码功能）。
  /// 仅多源包（真果鉴）生效；红果鉴单源包无密码功能。
  void _onNavSelected(int tab) {
    if (allSourcesEnabled && tab == 2) {
      if (_recentTaps.register(tab)) {
        _openSourceGate();
        return;
      }
    } else {
      _recentTaps.reset();
    }
    _changeTab(tab);
  }

  void _openSourceGate() {
    _pauseCatalog();
    unawaited(showSourceGateDialog(context, widget.store));
  }

  void _cancelSelection() => setState(() {
    _selectionMode = false;
    _selectedDramas.clear();
  });

  void _selectDrama(Drama drama) {
    if (!widget.store.allowsSource(drama.source)) return;
    if (!_selectedDramas.containsKey(drama.id) &&
        _selectedDramas.length >= BatchDownloads.maxDramas) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('一次最多选择 50 部短剧，请分批处理')));
      return;
    }
    setState(() {
      _selectionMode = true;
      if (_selectedDramas.remove(drama.id) == null) {
        _selectedDramas[drama.id] = drama;
      }
    });
  }

  void _downloadSelected() {
    if (!widget.store.canDownload || _selectedDramas.isEmpty) return;
    _pauseCatalog();
    Navigator.push<void>(
      context,
      MaterialPageRoute(
        builder: (_) => BatchDownloadScreen(
          repository: widget.repository,
          store: widget.store,
          dramas: _selectedDramas.values.toList(),
        ),
      ),
    );
  }

  Future<void> _batchAction() async {
    if (_selectedDramas.isEmpty) return;
    final choice = await showBatchDramaActions(
      context,
      count: _selectedDramas.length,
    );
    if (choice == null || !mounted) return;
    final store = widget.store;
    final epoch = store.profileEpoch;
    final dramas = _selectedDramas.values
        .where((drama) => store.allowsSource(drama.source))
        .toList();
    final label = switch (choice) {
      'favoriteAdd' => '批量加入追剧',
      'favoriteRemove' => '批量取消追剧',
      'read' => '标记更新已读',
      'favoriteSourceAdd' => '收藏站源',
      'favoriteSourceRemove' => '取消收藏站源',
      'removeHistory' => '删除观看记录',
      _ when choice.startsWith('status:') =>
        '标记${FollowStatus.values.firstWhere((value) => choice == 'status:${value.name}').label}',
      _ => '批量操作',
    };
    var applied = 0;
    try {
      if (choice.startsWith('status:')) {
        final status = FollowStatus.values.firstWhere(
          (value) => choice == 'status:${value.name}',
        );
        for (final drama in dramas) {
          await store.setFollowStatus(drama, status);
          applied++;
        }
      } else {
        switch (choice) {
          case 'favoriteAdd':
            for (final drama in dramas) {
              if (store.following(drama.id) != null) continue;
              await store.toggleFavorite(drama);
              applied++;
            }
          case 'favoriteRemove':
            for (final drama in dramas) {
              if (store.following(drama.id) == null) continue;
              await store.toggleFavorite(drama);
              applied++;
            }
          case 'read':
            for (final drama in dramas) {
              if (store.following(drama.id)?.hasUpdates != true) continue;
              await store.markUpdatesRead(drama.id);
              applied++;
            }
          case 'favoriteSourceAdd':
          case 'favoriteSourceRemove':
            final add = choice == 'favoriteSourceAdd';
            final ids = <String>{for (final drama in dramas) drama.source};
            for (final id in ids) {
              if (store.isFavoriteSource(id) == add) continue;
              await store.toggleFavoriteSource(id);
              applied++;
            }
          case 'removeHistory':
            for (final drama in dramas) {
              if (store.watched(drama.id) == null) continue;
              await store.removeHistory(drama.id);
              applied++;
            }
        }
      }
    } catch (error) {
      if (mounted) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text('批量操作失败：$error')));
      }
      return;
    }
    if (!mounted || epoch != store.profileEpoch) return;
    _cancelSelection();
    final unit = choice.startsWith('favoriteSource') ? '个站源' : '部短剧';
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text('$label：已处理 $applied $unit')));
  }

  void _dramaActions(Drama drama) => showDramaActions(
    context,
    drama: drama,
    store: widget.store,
    onContinue: () => _openDrama(drama, resume: true),
    onDownload: widget.repository.supportsDownloads && widget.store.canDownload
        ? () => _openDrama(drama, download: true)
        : null,
    onSelect: widget.repository.supportsDownloads && widget.store.canDownload
        ? () => _selectDrama(drama)
        : null,
  );

  Widget _catalogTile(
    Drama drama, {
    FocusNode? focusNode,
    VoidCallback? onFocus,
  }) {
    final following = widget.store.following(drama.id);
    final canSelect = widget.store.allowsSource(drama.source);
    return DramaTile(
      key: ValueKey(drama.id),
      drama: drama,
      repository: widget.repository,
      focusNode: focusNode,
      onFocus: onFocus,
      onTap: () => _selectionMode ? _selectDrama(drama) : _openDrama(drama),
      onLongPress: canSelect ? () => _selectDrama(drama) : null,
      onMore: () => _dramaActions(drama),
      actions: DramaActionButton(
        drama: drama,
        onPressed: () => _dramaActions(drama),
      ),
      selected: _selectionMode ? _selectedDramas.containsKey(drama.id) : null,
      badge: following == null
          ? null
          : '${following.status.label}${following.hasUpdates ? ' · ${following.updateLabel}' : ''}',
    );
  }

  void _pauseCatalog() {
    _debounce?.cancel();
    _generation++;
    unawaited(_browser.cancel());
    unawaited(widget.repository.cancelSuggestions());
    setState(() {
      _loading = false;
      _loadingMore = false;
      _stopHeavyNotice();
    });
  }

  bool get _supportsVipFilter =>
      _group.sources.any((source) => source.id == 'huangdou');
  bool get _hideVip => _supportsVipFilter && widget.store.hideVip;

  /// 当前要展示的条目（过滤 + 排序）。
  ///
  /// 带缓存：这个 getter 在每次 rebuild 时都会被访问，而「全部站源」可能有
  /// 上千条——每帧做一遍「allowsSource 逐条判断 + 搜索匹配 + 排序」会明显
  /// 卡顿（点按钮后半天才出画面）。缓存签名覆盖所有影响结果的输入：
  /// 条目版本、搜索词、分类、排序视图、VIP 过滤、以及站源可见性相关的
  /// store 状态。
  List<Drama> get _visible {
    final view = widget.store.catalogView;
    final query = _search.text.trim().toLowerCase();
    // 可见站源列表本身就编码了「隐藏 / 解锁 / 用户权限」——直接拿它做
    // 签名的一部分，比单独猜某个计数更可靠（隐藏操作不会改变长度）。
    final sourceIds = widget.store.sources.map((site) => site.id).join(',');
    final signature =
        '$_itemsRevision|$query|$_category|${view.sort.name}|${view.release}|'
        '$_hideVip|$_onlineSearch|$sourceIds';
    final cached = _visibleCache;
    if (cached != null && signature == _visibleSignature) return cached;
    final result = sortCatalog(
      _items.where((drama) {
        if (!widget.store.allowsSource(drama.source)) return false;
        if (_category.startsWith('local:') &&
            categoryName(drama.category) != _category.substring(6)) {
          return false;
        }
        if (_hideVip && drama.source == 'huangdou' && drama.vip) {
          return false;
        }
        return _onlineSearch ||
            query.isEmpty ||
            matchesDramaQuery(drama, query);
      }),
      view,
    );
    _visibleCache = result;
    _visibleSignature = signature;
    return result;
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (context, _) => LayoutBuilder(
      builder: (context, constraints) {
        final television = AppLayout.isTelevision(context);
        final desktop = constraints.maxWidth >= 840;
        final scaffold = Scaffold(
          appBar: AppBar(
            toolbarHeight: television ? 64 : null,
            titleSpacing: 12,
            title: _selectionMode
                ? const Text(
                    '选择短剧',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  )
                : _tab == 0
                ? PopupMenuButton<SourceGroup>(
                    key: const ValueKey('source-switch'),
                    tooltip: '切换站源',
                    enabled: _sourceGroups.length > 1,
                    onSelected: _changeGroup,
                    itemBuilder: (_) => [
                      for (final group in _sourceGroups)
                        PopupMenuItem(
                          value: group,
                          child: Row(
                            children: [
                              Expanded(child: Text(group.name)),
                              if (group.id == _group.id)
                                const Icon(Icons.check_rounded, size: 20),
                            ],
                          ),
                        ),
                    ],
                    child: SizedBox(
                      height: 48,
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Flexible(
                            // [+190] 品牌头按定稿保留「站源名 + 点开切源」，
                            // 不用设计稿的「发现好剧」大标题；只把字重 / 字距
                            // 往定稿 h2 的粗体质感靠。PopupMenuButton 与
                            // ValueKey('source-switch') 一律不动（多个 widget
                            // test 依赖它）。
                            child: Text(
                              _group.name,
                              overflow: TextOverflow.ellipsis,
                              style: const TextStyle(
                                fontSize: 21,
                                fontWeight: FontWeight.w800,
                                letterSpacing: -.5,
                              ),
                            ),
                          ),
                          if (_sourceGroups.length > 1)
                            const Icon(Icons.expand_more_rounded),
                        ],
                      ),
                    ),
                  )
                : const Text(appName),
            actions: [
              if (_selectionMode) ...[
                TextButton(
                  key: const ValueKey('clear-catalog-selection'),
                  onPressed: _selectedDramas.isEmpty
                      ? null
                      : () => setState(_selectedDramas.clear),
                  child: const Text('清空'),
                ),
                TextButton(
                  key: const ValueKey('cancel-catalog-selection'),
                  onPressed: _cancelSelection,
                  child: const Text('取消'),
                ),
              ] else ...[
                if (_tab == 1)
                  IconButton(
                    key: const ValueKey('follow-webdav-sync'),
                    tooltip: 'WebDAV 同步',
                    onPressed: () => Navigator.push(
                      context,
                      MaterialPageRoute<void>(
                        builder: (_) => WebDavSyncScreen(
                          store: widget.store,
                          repository: widget.repository,
                          syncOnOpen: true,
                        ),
                      ),
                    ),
                    icon: const Icon(Icons.cloud_sync_outlined),
                  ),
                if (_tab == 0) ...[
                  if (!_showRecommendations)
                    IconButton(
                      tooltip: '排序与筛选 · ${widget.store.catalogView.sort.label}',
                      onPressed: _chooseCatalogView,
                      color:
                          widget.store.catalogView.sort != CatalogSort.source ||
                              widget.store.catalogView.release.isNotEmpty
                          ? Theme.of(context).colorScheme.primary
                          : null,
                      icon: const Icon(Icons.sort_rounded),
                    ),
                  IconButton(
                    key: const ValueKey('open-rankings'),
                    tooltip: '榜单',
                    onPressed: widget.store.sources.isEmpty
                        ? null
                        : _openRankings,
                    icon: const Icon(Icons.leaderboard_outlined),
                  ),
                  if (!_showRecommendations)
                    IconButton(
                      key: const ValueKey('select-catalog-dramas'),
                      tooltip: '多选',
                      onPressed: () => setState(() => _selectionMode = true),
                      icon: const Icon(Icons.checklist_rounded),
                    ),
                  // [+190] 手机 / 桌面搜索框常驻，放大镜入口只在电视端保留
                  // （遥控器仍走对话框搜索）。
                  if (television)
                    IconButton(
                      key: const ValueKey('toggle-search'),
                      tooltip: '搜索',
                      icon: const Icon(Icons.search_rounded),
                      onPressed: _toggleSearch,
                    ),
                ],
                if (_tab == 0 &&
                    !_showRecommendations &&
                    constraints.maxWidth >= 400)
                  RefreshAction(
                    key: const ValueKey('catalog-refresh'),
                    loading:
                        _loading ||
                        _loadingMore ||
                        _categoriesLoading ||
                        _group.sources.any(
                          (source) => _updater.busy(source.id),
                        ),
                    tooltip: '更新剧库',
                    onPressed: widget.store.sources.isEmpty
                        ? null
                        : _refreshCatalog,
                  ),
                PopupMenuButton<String>(
                  tooltip: '更多',
                  onSelected: (value) {
                    if (value == 'update') {
                      _refreshCatalog();
                    } else if (value == 'sources') {
                      _manageSources();
                    } else if (value == 'settings') {
                      Navigator.push(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => SettingsScreen(
                            repository: widget.repository,
                            store: widget.store,
                          ),
                        ),
                      );
                    } else if (value == 'users') {
                      Navigator.push(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => ProfilesScreen(store: widget.store),
                        ),
                      );
                    } else if (value == 'display') {
                      _chooseDisplayMode();
                    } else if (value == 'about') {
                      showAboutDialog(
                        context: context,
                        applicationName: appName,
                        applicationVersion: AppLayout.versionOf(context),
                        applicationIcon: const Icon(
                          Icons.play_circle_filled_rounded,
                          size: 48,
                          color: Color(0xFFFF765F),
                        ),
                        children: [
                          const Text('独立运行，打开即可浏览和播放。观看记录与追剧收藏保存在当前设备。'),
                        ],
                      );
                    }
                  },
                  itemBuilder: (_) => [
                    if (_tab == 0 &&
                        !_showRecommendations &&
                        constraints.maxWidth < 400)
                      PopupMenuItem(
                        value: 'update',
                        enabled:
                            widget.store.sources.isNotEmpty &&
                            !_group.sources.any(
                              (source) => _updater.busy(source.id),
                            ),
                        child: const Text('更新剧库'),
                      ),
                    if (widget.repository.supportsSourceManagement)
                      const PopupMenuItem(
                        value: 'sources',
                        child: Text('站源管理'),
                      ),
                    const PopupMenuItem(value: 'users', child: Text('用户管理')),
                    const PopupMenuItem(
                      value: 'settings',
                      child: Text('设置与备份'),
                    ),
                    const PopupMenuItem(value: 'display', child: Text('界面模式')),
                    const PopupMenuItem(
                      value: 'about',
                      child: Text('关于$appName'),
                    ),
                  ],
                ),
              ],
              const SizedBox(width: 8),
            ],
          ),
          body: SafeArea(
            top: false,
            child: Row(
              children: [
                if (television) ...[
                  SizedBox(
                    width: 164,
                    child: Padding(
                      padding: const EdgeInsets.fromLTRB(8, 24, 8, 12),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          for (final entry in [
                            (Icons.explore_rounded, '发现'),
                            (Icons.bookmark_rounded, '追剧'),
                            (Icons.history_rounded, '最近观看'),
                            if (widget.store.canDownload)
                              (Icons.download_rounded, '下载'),
                          ].indexed)
                            Padding(
                              padding: const EdgeInsets.only(bottom: 14),
                              child: RemoteButton(
                                key: ValueKey('tv-nav-${entry.$1}'),
                                label: entry.$2.$2,
                                icon: entry.$2.$1,
                                selected: _tab == entry.$1,
                                autofocus: entry.$1 == 0,
                                onPressed: () => _onNavSelected(entry.$1),
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
                  const VerticalDivider(width: 1),
                ] else if (desktop) ...[
                  NavigationRail(
                    selectedIndex: _tab,
                    onDestinationSelected: _onNavSelected,
                    labelType: NavigationRailLabelType.all,
                    groupAlignment: -.8,
                    destinations: [
                      NavigationRailDestination(
                        icon: CinemaIcon(CinemaSymbol.discover),
                        selectedIcon: CinemaIcon(CinemaSymbol.discover),
                        label: Text('发现'),
                      ),
                      NavigationRailDestination(
                        icon: CinemaIcon(CinemaSymbol.follow),
                        selectedIcon: CinemaIcon(CinemaSymbol.follow),
                        label: Text('追剧'),
                      ),
                      NavigationRailDestination(
                        icon: CinemaIcon(CinemaSymbol.history),
                        selectedIcon: CinemaIcon(CinemaSymbol.history),
                        label: Text('最近观看'),
                      ),
                      if (widget.store.canDownload)
                        NavigationRailDestination(
                          icon: CinemaIcon(CinemaSymbol.download),
                          selectedIcon: CinemaIcon(CinemaSymbol.download),
                          label: Text('下载'),
                        ),
                    ],
                  ),
                  const VerticalDivider(width: 1, thickness: 1),
                ],
                Expanded(
                  child: _tab == 0
                      ? widget.store.sources.isEmpty
                            ? const StatusPanel(
                                title: '暂无可用站源',
                                message: '请联系管理员为当前用户开放站源。',
                              )
                            : _catalogWithLoadFeedback(
                                selectionInBody: desktop || television,
                              )
                      : _tab == 3
                      ? DownloadsScreen(
                          repository: widget.repository,
                          store: widget.store,
                          embedded: true,
                        )
                      : SavedLibrary(
                          key: ValueKey('saved-tab-$_tab'),
                          repository: widget.repository,
                          store: widget.store,
                          history: _tab == 2,
                          onOpen: _openDrama,
                          onContinue: (drama) =>
                              _openDrama(drama, resume: true),
                          onDownload:
                              widget.repository.supportsDownloads &&
                                  widget.store.canDownload
                              ? (drama) => _openDrama(drama, download: true)
                              : null,
                        ),
                ),
              ],
            ),
          ),
          bottomNavigationBar: desktop || television
              ? null
              : _selectionMode
              ? _selectionBar()
              : AppBottomNavigation(
                  selectedIndex: _tab,
                  onDestinationSelected: _onNavSelected,
                  destinations: [
                    NavigationDestination(
                      icon: CinemaIcon(CinemaSymbol.discover),
                      label: '发现',
                    ),
                    NavigationDestination(
                      icon: CinemaIcon(CinemaSymbol.follow),
                      label: '追剧',
                    ),
                    NavigationDestination(
                      icon: CinemaIcon(CinemaSymbol.history),
                      label: '最近观看',
                    ),
                    if (widget.store.canDownload)
                      NavigationDestination(
                        icon: CinemaIcon(CinemaSymbol.download),
                        label: '下载',
                      ),
                  ],
                ),
        );
        if (!television && !_selectionMode) return scaffold;
        return PopScope(
          canPop:
              !_selectionMode &&
              (!television || _tab == 0 && _search.text.isEmpty),
          onPopInvokedWithResult: (didPop, result) {
            if (!didPop) _televisionBack();
          },
          child: CallbackShortcuts(
            bindings: {
              const SingleActivator(LogicalKeyboardKey.escape): () =>
                  Navigator.of(context).maybePop(),
              const SingleActivator(LogicalKeyboardKey.goBack): () =>
                  Navigator.of(context).maybePop(),
            },
            child: scaffold,
          ),
        );
      },
    ),
  );

  /// [+159] 重载分组的加载提示——有时间上限，绝不一直挡到加载结束。
  ///
  /// 「所有站源」和源数 ≥ [_heavySourceThreshold] 的集合一次要打几十个请求。
  /// 实测难熬的只有**开头十几秒**：第一批内容很快就能出来，但列表在持续
  /// 流式追加，这段掉帧最明显；之后只是慢源陆续回来，界面已经能正常用。
  /// 所以提示带 [_heavyNoticeLimit] 上限，到点自动收起——几十个源的组可能
  /// 拖到一分钟才收尾，一直挡着等于不让用户看剧。
  ///
  /// 两档形态：
  /// - 列表还空着 → 立刻出现大半屏遮罩，明确告诉用户在等什么；
  /// - 列表已经有内容（通常进组一两秒后就出剧了）→ 300ms 后才出现，
  ///   遮罩更透，既说明"现在卡是正常的"，也能看见底下已经加载的内容。
  ///
  /// 非重载分组保持原样：一两个源的组本来就是秒开，盖提示反而像卡住了。
  static const _heavyNoticeDelay = Duration(milliseconds: 300);

  /// 提示最长存活时间。用户反馈卡顿集中在前十几秒，取 12 秒；到点无论
  /// 是否还有慢源在返回，都把界面还给列表。
  static const _heavyNoticeLimit = Duration(seconds: 12);

  /// 已响应的源占比到这里就把提示撤掉（不等 [_heavyNoticeLimit]）。
  ///
  /// 几十个源的组里总有一两个不响应的，连接会一直挂到超时（最长 60 秒），
  /// 光靠 12 秒上限等于让这两个源白占十秒遮罩。实测 90% 之后该出的内容
  /// 基本都在列表里了，剩下的源回来只是往尾部追加，没必要继续挡着。
  /// 用向上取整判定：12 个源要 11 个、30 个源要 27 个；小组不会被
  /// 「回来一个就撤」误伤。
  static const _heavyNoticeReleaseRatio = 0.9;

  Timer? _heavyNoticeTimer;
  Timer? _heavyNoticeHideTimer;
  bool _heavyNotice = false;

  /// 上一轮加载时当前分组实际请求的源集合。
  ///
  /// 订阅 / 导入新源后，当前分组本身也会变大（新增的源往往就落在当前组里）。
  /// 用这份快照判断「是不是多了源」：多了就说明列表缺内容，需要自动补一轮。
  Set<String> _loadedGroupIds = const {};

  bool get _heavyGroup =>
      _allSources || _group.sources.length >= _heavySourceThreshold;

  void _startHeavyNotice({required bool immediate}) {
    _heavyNoticeTimer?.cancel();
    _heavyNoticeHideTimer?.cancel();
    if (immediate) {
      _heavyNotice = true;
      _scheduleHeavyNoticeHide();
      return;
    }
    _heavyNoticeTimer = Timer(_heavyNoticeDelay, () {
      if (!mounted) return;
      setState(() => _heavyNotice = true);
      _scheduleHeavyNoticeHide();
    });
  }

  void _scheduleHeavyNoticeHide() {
    _heavyNoticeHideTimer?.cancel();
    _heavyNoticeHideTimer = Timer(_heavyNoticeLimit, () {
      if (!mounted) return;
      setState(() => _heavyNotice = false);
    });
  }

  void _stopHeavyNotice() {
    _heavyNoticeTimer?.cancel();
    _heavyNoticeHideTimer?.cancel();
    _heavyNotice = false;
  }

  /// 本轮进度是否已经到 [_heavyNoticeReleaseRatio]。
  bool _heavyNoticeReached(int done, int total) =>
      total > 0 && done >= (total * _heavyNoticeReleaseRatio).ceil();

  Widget _catalogWithLoadFeedback({required bool selectionInBody}) {
    final overlay = _heavyNotice && _loading;
    final catalog = _catalog(
      selectionInBody: selectionInBody,
      hideEmptyLoading: overlay,
    );
    if (!overlay) return catalog;
    return Stack(fit: StackFit.expand, children: [catalog, _loadOverlay()]);
  }

  Widget _loadOverlay() {
    final scheme = Theme.of(context).colorScheme;
    final theme = Theme.of(context);
    final total = _loadTotal;
    final done = total > 0 ? _loadDone.clamp(0, total) : _loadDone;
    final hasItems = _items.isNotEmpty;
    return AbsorbPointer(
      child: ColoredBox(
        key: const ValueKey('heavy-load-overlay'),
        // 用 surface 而不是纯黑：Material 3 的浅色/深色主题都能自然融合。
        // 已有内容时更透（0.72），让用户看得见底下已经加载出来的剧。
        color: scheme.surface.withValues(alpha: hasItems ? 0.72 : 0.94),
        child: Center(
          child: Material(
            color: hasItems ? scheme.surfaceContainerHigh : Colors.transparent,
            elevation: hasItems ? 3 : 0,
            borderRadius: BorderRadius.circular(24),
            child: Padding(
              padding: EdgeInsets.symmetric(
                horizontal: hasItems ? 28 : 0,
                vertical: hasItems ? 24 : 0,
              ),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  SizedBox(
                    width: 52,
                    height: 52,
                    child: CircularProgressIndicator(
                      // 第一个源回来之前先转不定长圈；有进度后换成确定进度，
                      // 让「在动」这件事有具体数字可看。
                      value: total > 0 && done > 0 ? done / total : null,
                      strokeWidth: 4,
                    ),
                  ),
                  const SizedBox(height: 20),
                  Text(
                    _allSources ? '正在加载全部站源' : '正在加载${_group.name}',
                    textAlign: TextAlign.center,
                    style: theme.textTheme.titleMedium?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    total > 0 ? '已响应 $done / $total 个站源' : '正在并发请求站源…',
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: scheme.onSurfaceVariant,
                    ),
                  ),
                  if (hasItems) ...[
                    const SizedBox(height: 10),
                    Text(
                      '加载高峰期可能略卡，稍后自动收起',
                      textAlign: TextAlign.center,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: scheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _catalog({
    required bool selectionInBody,
    bool hideEmptyLoading = false,
  }) {
    final items = _visible;
    final television = AppLayout.isTelevision(context);
    return Column(
      children: [
        // [+190] 搜索框常驻（电视端仍走对话框，不在页面上放输入框）。
        if (!television)
          Padding(
            padding: const EdgeInsets.fromLTRB(
              CinemaTokens.pageGutter,
              10,
              CinemaTokens.pageGutter,
              4,
            ),
            child: SearchInput(
              key: ValueKey('search-${_group.id}'),
              controller: _search,
              // 常驻输入框不能一进页面就弹键盘。
              autofocus: false,
              hint: _searchHint,
              suggestions: _searchSuggestions
                  ? widget.repository.suggestions
                  : null,
              onChanged: _searchChanged,
              onCancel: () => unawaited(widget.repository.cancelSuggestions()),
              onSearch: _submitSearch,
              onFocusChange: (focused) {
                if (mounted && _searchFocused != focused) {
                  setState(() => _searchFocused = focused);
                }
              },
            ),
          ),
        // [+190] 最近搜索只在输入框获得焦点时出现，避免常驻搜索框之外
        // 再常显一行历史把首屏挤高。
        if (_searchFocused &&
            _search.text.trim().isEmpty &&
            widget.store.recentSearches.isNotEmpty)
          Padding(
            padding: const EdgeInsets.fromLTRB(
              CinemaTokens.pageGutter,
              0,
              CinemaTokens.pageGutter,
              8,
            ),
            child: Row(
              children: [
                Expanded(
                  child: SingleChildScrollView(
                    scrollDirection: Axis.horizontal,
                    child: Row(
                      children: [
                        for (final query in widget.store.recentSearches)
                          Padding(
                            padding: const EdgeInsets.only(right: 8),
                            child: ActionChip(
                              avatar: const Icon(
                                Icons.history_rounded,
                                size: 16,
                              ),
                              label: Text(query),
                              onPressed: () => _submitSearch(query),
                            ),
                          ),
                      ],
                    ),
                  ),
                ),
                IconButton(
                  tooltip: '清空最近搜索',
                  onPressed: () =>
                      saveUserChange(context, widget.store.clearRecentSearches),
                  icon: const Icon(Icons.delete_outline_rounded, size: 20),
                ),
              ],
            ),
          ),
        CatalogFilters(
          key: ValueKey('filters-${_group.id}'),
          categories: _displayCategories,
          category: _displayCategory,
          error: _categoriesError,
          onCategory: _changeCategory,
          // 标签栏轻拉 = 刷新当前分类目录；重拉 = 打开分类面板
          // （面板内可排序选择分类）。
          onRefresh: () => unawaited(_refreshCatalog()),
          onRetry: () => _loadCategories(force: true),
          trailing: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (_supportsVipFilter)
                IconButton(
                  tooltip: widget.store.hideVip ? 'VIP：隐藏' : 'VIP：显示',
                  onPressed: () => saveUserChange(
                    context,
                    () => widget.store.setHideVip(!widget.store.hideVip),
                  ),
                  icon: VipIcon(hidden: widget.store.hideVip),
                ),
            ],
          ),
        ),
        // [+190] 「继续观看」横卡：最近未看完的两条，纯本地绘制、不发网络
        // 请求，也不使用 DramaCover / DramaTile（见 cinema_sections.dart
        // 文件头注释：interface_test 会跨 tab 数封面）。无记录时组件返回
        // 零尺寸，不占首屏一行空白。
        ContinueWatchingStrip(
          key: const ValueKey('continue-watching-strip'),
          entries: widget.store.history
              .where((entry) => !entry.finished)
              .take(2)
              .toList(),
          onOpen: (drama) => _openDrama(drama, resume: true),
          onViewAll: () => _changeTab(2),
        ),
        // [+190] 目录态的「为你推荐 / 换一批」标题行。推荐态的标题行由
        // RecommendationsScreen 自己在内嵌模式渲染（档位按钮要读它的
        // _genre / _load），这里只负责目录态，避免两侧各维护一套状态。
        if (!_showRecommendations)
          CinemaSectionHeader(
            title: '为你推荐',
            actions: [
              CinemaTextLink(
                label: '换一批',
                icon: Icons.refresh_rounded,
                tooltip: '换一批剧集',
                onTap: (_loading || _loadingMore)
                    ? null
                    : () => _load(force: true),
              ),
            ],
          ),
        if (_showRecommendations)
          Expanded(
            child: GestureDetector(
              onHorizontalDragEnd: television ? null : _swipeCategory,
              child: RecommendationsScreen(
                repository: widget.repository,
                store: widget.store,
                embedded: true,
              ),
            ),
          )
        else ...[
          if (_loading && _items.isNotEmpty)
            const LinearProgressIndicator(minHeight: 2),
          Expanded(
            child: GestureDetector(
              onHorizontalDragEnd: television ? null : _swipeCategory,
              child: _loading && _items.isEmpty
                  ? hideEmptyLoading
                        // 重载分组有自己的遮罩，这里再渲染一份「正在加载剧集」
                        // 会从半透明遮罩底下透出来重影。
                        ? const SizedBox.expand()
                        : const Center(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                CircularProgressIndicator(),
                                SizedBox(height: 18),
                                Text('正在加载剧集'),
                              ],
                            ),
                          )
                  : _items.isEmpty && _error != null
                  ? StatusPanel(
                      title: '暂时无法加载',
                      message: _error!,
                      onRetry: () => _load(force: true),
                      secondaryAction: _errorSecondaryActions,
                      icon: _errorNeedsVerification
                          ? Icons.verified_user_outlined
                          : Icons.wifi_off_rounded,
                    )
                  : items.isEmpty
                  ? StatusPanel(
                      title: '没有找到匹配的短剧',
                      message: _hideVip
                          ? '可以换个搜索词，或显示 VIP 内容。'
                          : widget.store.sources.length > 1
                          ? '可以换个搜索词或切换站源。'
                          : '可以换个搜索词，或刷新后重试。',
                      onRetry:
                          _hasMore &&
                              !_loadingMore &&
                              (!_onlineSearch ||
                                  _search.text.isEmpty ||
                                  _group.sources.any(
                                    (source) => source.pagedSearch,
                                  ))
                          ? () => _load(more: true)
                          : null,
                      action: '加载更多',
                    )
                  : LayoutBuilder(
                      builder: (context, constraints) {
                        if (television) {
                          return _televisionGrid(
                            items,
                            constraints.maxWidth,
                            key:
                                'catalog-${_group.id}-$_category-$_submittedQuery',
                            controller: _scroll,
                            footer: Padding(
                              padding: const EdgeInsets.fromLTRB(18, 0, 18, 24),
                              child: Center(
                                child: _loadingMore
                                    ? const CircularProgressIndicator()
                                    : _hasMore
                                    ? RemoteButton(
                                        label: '加载更多',
                                        icon: Icons.expand_more,
                                        onPressed: () => _load(more: true),
                                      )
                                    : const Text('已经看到这里的全部剧集'),
                              ),
                            ),
                          );
                        }
                        final padding = constraints.maxWidth < 600
                            ? CinemaTokens.pageGutter
                            : 24.0;
                        return RefreshIndicator(
                          onRefresh: _refreshCatalog,
                          child: CustomScrollView(
                            controller: _scroll,
                            physics: const AlwaysScrollableScrollPhysics(),
                            slivers: [
                              SliverPadding(
                                padding: EdgeInsets.fromLTRB(
                                  padding,
                                  0,
                                  padding,
                                  16,
                                ),
                                sliver: SliverGrid(
                                  gridDelegate: dramaGridDelegate(
                                    context,
                                    constraints.maxWidth - 2 * padding,
                                  ),
                                  delegate: SliverChildBuilderDelegate(
                                    (_, index) => _catalogTile(items[index]),
                                    childCount: items.length,
                                  ),
                                ),
                              ),
                              SliverToBoxAdapter(
                                child: Padding(
                                  padding: const EdgeInsets.only(bottom: 24),
                                  child: Center(
                                    child: _loadingMore
                                        ? const CircularProgressIndicator()
                                        : _hasMore
                                        ? OutlinedButton.icon(
                                            onPressed: () => _load(more: true),
                                            icon: const Icon(
                                              Icons.expand_more_rounded,
                                            ),
                                            label: const Text('加载更多'),
                                          )
                                        : Text(
                                            '已经看到这里的全部剧集',
                                            style: TextStyle(
                                              color: Theme.of(
                                                context,
                                              ).colorScheme.onSurfaceVariant,
                                              fontSize: 12,
                                            ),
                                          ),
                                  ),
                                ),
                              ),
                            ],
                          ),
                        );
                      },
                    ),
            ),
          ),
          if (_selectionMode && selectionInBody)
            _selectionBar(safeBottom: false),
        ],
      ],
    );
  }

  Widget _selectionBar({bool safeBottom = true}) {
    final theme = Theme.of(context);
    final count = _selectedDramas.length;
    return Material(
      color: theme.colorScheme.surface,
      child: Container(
        width: double.infinity,
        decoration: BoxDecoration(
          border: Border(
            top: BorderSide(color: theme.colorScheme.outlineVariant),
          ),
        ),
        child: SafeArea(
          top: false,
          bottom: safeBottom,
          minimum: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          child: LayoutBuilder(
            builder: (context, constraints) {
              final canDownload =
                  widget.store.canDownload &&
                  widget.repository.supportsDownloads;
              final summary = Semantics(
                liveRegion: true,
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      count == 0 ? '长按卡片或点选短剧' : '已选 $count 部',
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      count == 0
                          ? '可批量追剧、标记状态、清理记录${canDownload ? '，或批量下载' : ''}'
                          : '选「批量操作」处理选中项${canDownload ? '，或点「下一步」下载' : ''}',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              );
              final batch = OutlinedButton.icon(
                key: const ValueKey('batch-drama-actions'),
                onPressed: count == 0 ? null : _batchAction,
                icon: const Icon(Icons.more_horiz_rounded, size: 18),
                label: const Text('批量操作'),
                style: OutlinedButton.styleFrom(
                  minimumSize: const Size(96, 48),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 16,
                    vertical: 12,
                  ),
                ),
              );
              final next = canDownload
                  ? FilledButton(
                      key: const ValueKey('download-selected-dramas'),
                      onPressed: count == 0 ? null : _downloadSelected,
                      style: FilledButton.styleFrom(
                        minimumSize: const Size(96, 48),
                        padding: const EdgeInsets.symmetric(
                          horizontal: 20,
                          vertical: 12,
                        ),
                      ),
                      child: const Text('下一步'),
                    )
                  : null;
              final actions = Wrap(
                spacing: 8,
                runSpacing: 8,
                alignment: WrapAlignment.end,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [batch, if (next != null) next],
              );
              if (constraints.maxWidth < 320 ||
                  MediaQuery.textScalerOf(context).scale(14) > 21) {
                return Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [summary, const SizedBox(height: 12), actions],
                );
              }
              return Row(
                children: [
                  Expanded(child: summary),
                  const SizedBox(width: 16),
                  actions,
                ],
              );
            },
          ),
        ),
      ),
    );
  }

  Widget _televisionGrid(
    List<Drama> items,
    double width, {
    required String key,
    ScrollController? controller,
    Widget? footer,
  }) {
    final columns = ((width - 36) / 150).floor().clamp(1, 8);
    final tileWidth = (width - 36 - (columns - 1) * 14) / columns;
    return RemoteGrid(
      key: ValueKey('tv-grid-$key'),
      itemKeys: items.map((item) => item.id).toList(),
      columns: columns,
      itemExtent: DramaTile.extentFor(context, tileWidth - 14) + 14,
      controller: controller,
      footer: footer,
      padding: const EdgeInsets.fromLTRB(18, 2, 18, 18),
      itemBuilder: (_, index, node, onFocus) =>
          _catalogTile(items[index], focusNode: node, onFocus: onFocus),
    );
  }
}
