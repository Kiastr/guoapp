import 'package:duanju_app/home_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

// [+159] 重载分组的加载提示（有时间上限）：
// - 「所有站源」/ 源数 ≥ 10 的集合，首次进入立刻给大半屏提示；
// - 列表已经出内容后再刷新，延迟 300ms 才提示（秒开不闪），且遮罩更透；
// - 提示最多活 12 秒——卡顿集中在开头十几秒，绝不能挡到最后一个慢源返回；
// - 少量源的普通分组完全不弹。
class _DelayedRepository extends FixtureRepository {
  _DelayedRepository({
    this.firstDelay = Duration.zero,
    this.delay = Duration.zero,
  });

  final Duration firstDelay;
  final Duration delay;
  int _calls = 0;

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    final wait = _calls++ == 0 ? firstDelay : delay;
    if (wait > Duration.zero) await Future<void>.delayed(wait);
    return super.catalog(
      source,
      page: page,
      query: query,
      category: category,
      force: force,
    );
  }
}

List<SourceSite> _manySources(int count) => [
  for (var i = 0; i < count; i++) SourceSite('libretv:s$i', '采集站$i', '采集站'),
];

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  tearDown(() => SourceSite.setRuntimeSources(const []));

  Future<LocalStore> storeFor(List<SourceSite> sources) async {
    SourceSite.setRuntimeSources(sources);
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    // 停在多源集合上：12 个采集站同属 'libretv' 分组。
    await store.setCatalogSource(
      sources.first.id,
      allSources: false,
      groupId: 'libretv',
    );
    return store;
  }

  testWidgets('源数达到阈值时首次进入立刻出现大半屏提示', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final store = await storeFor(_manySources(12));
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(
          repository: _DelayedRepository(
            firstDelay: const Duration(seconds: 1),
            delay: const Duration(seconds: 1),
          ),
          store: store,
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsOneWidget);
    expect(find.text('正在加载扩展片源集合'), findsOneWidget);
    // 首个源可能在第一帧就回来了，进度数字不固定，只断言分母。
    expect(find.textContaining('/ 12 个站源'), findsOneWidget);
    // 有遮罩时不再重复显示旧的「正在加载剧集」空态。
    expect(find.text('正在加载剧集'), findsNothing);

    // 推进到所有源都返回：提示立刻收掉，列表接管。
    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 100));
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);
    expect(find.text('第1页短剧'), findsWidgets);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('已有内容时延迟出现，加载结束后收掉', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final store = await storeFor(_manySources(12));
    addTearDown(store.dispose);
    final repository = _DelayedRepository(delay: const Duration(seconds: 1));

    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);

    // 下拉刷新走 force 加载：列表保留，延迟 300ms 后才出现提示，秒开不闪。
    await tester.tap(find.byKey(const ValueKey('catalog-refresh')));
    await tester.pump();
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);

    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsOneWidget);
    // 已有内容时用更透的遮罩 + 卡片，明确说明会自动收起。
    expect(find.text('加载高峰期可能略卡，稍后自动收起'), findsOneWidget);

    await tester.pump(const Duration(seconds: 2));
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('提示最多 12 秒：慢源还没回来也会自动收起', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final store = await storeFor(_manySources(12));
    addTearDown(store.dispose);

    // 每个源都挂 20 秒：提示出现后很长一段时间加载都不会结束。
    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(
          repository: _DelayedRepository(
            firstDelay: const Duration(seconds: 20),
            delay: const Duration(seconds: 20),
          ),
          store: store,
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsOneWidget);

    // 前 11 秒还在卡顿期，提示保留。
    await tester.pump(const Duration(seconds: 11));
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsOneWidget);

    // 到点自动收起，此时请求并未完成——空态重新出现才是「仍在加载」的证据。
    await tester.pump(const Duration(seconds: 2));
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);
    expect(find.text('正在加载剧集'), findsOneWidget);

    // 收尾：把所有慢请求推进完，避免留下 pending timer。
    await tester.pump(const Duration(minutes: 2));
    await tester.pump(const Duration(milliseconds: 100));
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('订阅多个源后自动重载当前组，并立刻出现提示（不等 300ms）', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    // 先停在只有 2 个采集站的「扩展片源集合」：这组秒开，本来不该弹提示。
    final store = await storeFor(_manySources(2));
    addTearDown(store.dispose);
    final repository = _DelayedRepository(delay: const Duration(seconds: 1));

    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);
    final before = repository.requests.length;

    // 一次性订阅 10 个新采集站，全部落在当前分组里。
    SourceSite.setRuntimeSources(_manySources(12));
    store.notifySourceRegistryChanged();

    // 关键：不推进任何时间。这批源没有缓存，属于冷启动，提示必须立刻出现；
    // 如果走「刷新同组」的 300ms 延迟档，这里会是 findsNothing。
    await tester.pump();
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsOneWidget);
    expect(find.textContaining('/ 12 个站源'), findsOneWidget);

    // 当前组被整体重载：新增的源都发出了请求，列表不会停在旧内容上。
    // 夹具在延迟之后才记 requests，所以推一段时间再核对（并发 8，两批各 1 秒）。
    await tester.pump(const Duration(seconds: 2));
    expect(repository.requests.length, before + 12);

    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 100));
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('订阅的源不属于当前分组时不重载，也不弹提示', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SourceSite.setRuntimeSources(_manySources(2));
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    // 停在红果单源组：新订阅的采集站跟它无关。
    await store.setCatalogSource('hongguo', allSources: false);

    final repository = _DelayedRepository(delay: const Duration(seconds: 1));
    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();
    final before = repository.requests.length;

    SourceSite.setRuntimeSources(_manySources(12));
    store.notifySourceRegistryChanged();
    await tester.pump(const Duration(milliseconds: 500));

    // 当前组没多源，不该顺手重载，也不该弹提示。
    expect(repository.requests.length, before);
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('源数低于阈值的普通分组不出现加载提示', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final store = await storeFor(_manySources(2));
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(
          repository: _DelayedRepository(delay: const Duration(seconds: 1)),
          store: store,
        ),
      ),
    );
    await tester.pump();

    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);

    await tester.pump(const Duration(seconds: 3));
    await tester.pump(const Duration(milliseconds: 100));
    expect(find.byKey(const ValueKey('heavy-load-overlay')), findsNothing);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });
}
