import 'package:duanju_app/home_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _TapRepository extends FixtureRepository {
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

  tearDown(() => SourceSite.setRuntimeSources(const []));

  testWidgets('只有青空时点击动漫源集合，标题与勾选都留在集合上', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SourceSite.setRuntimeSources(const []);
    // 未开启 ALL_SOURCES 时内置源只有红果，动漫源集合不存在，跳过。
    if (!SourceSite.isAvailable('sorani')) return;

    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.setCatalogSource('hongguo', allSources: false);

    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(repository: _TapRepository(), store: store),
      ),
    );
    await tester.pumpAndSettle();

    // 打开左上角源选择，点「动漫源集合」。
    await tester.tap(find.byKey(const ValueKey('source-switch')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('动漫源集合').last);
    await tester.pumpAndSettle();

    // 选中项必须是集合本身，而不是被反推成组内唯一的青空。
    expect(store.catalogView.groupId, 'anime');
    expect(find.text('动漫源集合'), findsWidgets);
    expect(find.text('青空'), findsNothing);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('动漫源集合有多个源时按整组聚合', (tester) async {
    tester.view.physicalSize = const Size(430, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    if (!SourceSite.isAvailable('sorani')) return;
    SourceSite.setRuntimeSources(const [
      SourceSite('kazumi:test', '测试规则', 'Kazumi 规则'),
    ]);
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.setCatalogSource('hongguo', allSources: false);

    final repository = _TapRepository();
    await tester.pumpWidget(
      MaterialApp(
        home: HomeScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('source-switch')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('动漫源集合').last);
    await tester.pumpAndSettle();

    expect(store.catalogView.groupId, 'anime');
    expect(repository.requests, containsAll(['sorani', 'kazumi:test']));
    expect(find.text('动漫源集合'), findsWidgets);
    expect(find.text('来自 sorani'), findsOneWidget);
    expect(find.text('来自 kazumi:test'), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
  });
}
