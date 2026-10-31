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
// - [+156] 收藏的源在所属大组里也保留一份，切到大组时它与同族源一起聚合。
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

  testWidgets('切换到扩展片源集合落在组内首个源（收藏源仍在大组内）', (tester) async {
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

    // 组选择器切到扩展片源集合：落在组内第一个源（咪咕）上。
    await tester.tap(find.byKey(const ValueKey('source-switch')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('扩展片源集合').last);
    await tester.pumpAndSettle();

    expect(repository.requests, ['libretv:mihua']);
    expect(find.text('来自 libretv:mihua'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
