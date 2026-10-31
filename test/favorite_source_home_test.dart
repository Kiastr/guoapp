import 'package:duanju_app/home_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

// 收藏的运行时片源在首页独占一组后：
// - 选中它只能看到该片源自己的分类与剧集（不再落到 LibreTV / drpy 大组）；
// - 同前缀的未收藏源仍在大组里聚合，不受影响；
// - [+156] 收藏的源在所属大组里也保留一份，切到大组时它与同族源一起聚合；
// - [+158] 点多源分组 = 按整组聚合，且分组 id 被记住——否则组内首个源
//   是收藏源时（青空 / 韩小圈 / 咪咕），分组会被反推成「源 id 独占组」，
//   点「动漫源集合」等于点「青空」。
//
// 回归背景：_group 曾用 SourceSite.groupId 匹配分组，收藏源的 groupId
// 仍是 'libretv' 大组，导致选中收藏组实际落到大组上，显示聚合主页。
class _FavoriteHomeRepository extends FixtureRepository {
  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    requests.add(source);
    return CatalogPage(
      [
        Drama(
          id: '$source:auto-$page',
          source: source,
          title: '来自 $source',
          episodes: 1,
        ),
      ],
      page: page,
      hasMore: false,
    );
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SourceSite.setRuntimeSources([
      const SourceSite('libretv:mihua', '咪咕', '采集站'),
      const SourceSite('libretv:other', '其它采集站', '采集站'),
    ]);
  });

  tearDown(() {
    SourceSite.setRuntimeSources(const []);
  });

  testWidgets('选中收藏的片源只加载该源，不再落到 LibreTV 大组', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.toggleFavoriteSource('libretv:mihua');
    await store.setCatalogSource('libretv:mihua', allSources: false);

    final repository = _FavoriteHomeRepository();
    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();

    // 只请求收藏源自己；未收藏的其它采集站不在请求里。
    expect(repository.requests, ['libretv:mihua']);
    expect(find.text('来自 libretv:mihua'), findsOneWidget);
    expect(find.text('来自 libretv:other'), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('点击多源分组按整组聚合，标题保持组名', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.toggleFavoriteSource('libretv:mihua');
    // 先停在红果上，再切到扩展片源集合，才能看到切换效果（停在组内任一源
    // 上时当前组就是该大组，点它不会有反应）。
    await store.setCatalogSource('hongguo', allSources: false);

    // [+156] 收藏的源在所属大组里仍保留一份，且排在组内首位。
    expect(
      SourceGroup.fromSources(
        store.sources,
        favoriteIds: store.favoriteSourceIds,
      ).firstWhere((group) => group.id == 'libretv').sources.map((s) => s.id),
      containsAll(['libretv:mihua', 'libretv:other']),
    );

    final repository = _FavoriteHomeRepository();
    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();
    repository.requests.clear();

    // 组选择器切到扩展片源集合：[+158] 按整组聚合——组内首个源咪咕是收藏源，
    // 分组不能被反推成「咪咕」独占组，否则点集合等于点咪咕。
    await tester.tap(find.byKey(const ValueKey('source-switch')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('扩展片源集合').last);
    await tester.pumpAndSettle();

    expect(
      repository.requests,
      containsAll(['libretv:mihua', 'libretv:other']),
    );
    expect(find.text('来自 libretv:mihua'), findsOneWidget);
    expect(find.text('来自 libretv:other'), findsOneWidget);
    // 顶部标题仍是组名，不是组内首个源的名字。
    expect(find.text('扩展片源集合'), findsWidgets);
    expect(store.catalogView.groupId, 'libretv');
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('选中的分组会持久化：重建页面后仍按整组聚合', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.setCatalogSource('hongguo', allSources: false);

    final first = _FavoriteHomeRepository();
    await tester.pumpWidget(
      MaterialApp(home: HomeScreen(repository: first, store: store)),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('source-switch')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('扩展片源集合').last);
    await tester.pumpAndSettle();

    final second = _FavoriteHomeRepository();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpWidget(
      MaterialApp(home: HomeScreen(repository: second, store: store)),
    );
    await tester.pumpAndSettle();

    // 重建后不重新点，分组选择也要还在：两个源都请求、标题仍是组名。
    expect(
      second.requests,
      containsAll(['libretv:mihua', 'libretv:other']),
    );
    expect(find.text('扩展片源集合'), findsWidgets);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
