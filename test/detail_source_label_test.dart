import 'package:duanju_app/detail_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'library_feature_fixtures.dart';

// +121：视频简介内注明「片源：<名称>」——多片源合并展示时（LibreTV /
// drpy 片源收藏后与内置源同级），用户需要知道这一部具体来自哪个站源。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SourceSite.setRuntimeSources([
      const SourceSite('libretv:mihua', '咪咕', '采集站'),
    ]);
  });

  tearDown(() {
    SourceSite.setRuntimeSources(const []);
  });

  testWidgets('简介内显示片源名称（运行时片源与内置源）', (tester) async {
    tester.view.physicalSize = const Size(390, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    const runtimeDrama = Drama(
      id: 'libretv:mihua:one',
      source: 'libretv:mihua',
      title: '咪咕剧一',
      description: '这是一部来自咪咕采集站的短剧。',
      episodes: 2,
    );
    const builtinDrama = Drama(
      id: 'hongguo:one',
      source: 'hongguo',
      title: '红果剧一',
      description: '这是一部来自红果的短剧。',
      episodes: 2,
    );
    final repository = LibraryFeatureRepository()
      ..details[runtimeDrama.id] = LibraryFeatureRepository.makeDetail(
        runtimeDrama,
        2,
      )
      ..details[builtinDrama.id] = LibraryFeatureRepository.makeDetail(
        builtinDrama,
        2,
      );

    await tester.pumpWidget(
      MaterialApp(
        home: DetailScreen(
          drama: runtimeDrama,
          repository: repository,
          store: store,
        ),
      ),
    );
    await tester.pumpAndSettle();
    // 运行时片源：显示导入时注册的名称；标红（error 色）便于识别归属。
    expect(find.text('片源：咪咕'), findsOneWidget);
    expect(find.text('这是一部来自咪咕采集站的短剧。'), findsOneWidget);
    final sourceLabel = tester.widget<Text>(find.text('片源：咪咕'));
    expect(sourceLabel.style?.color, isNotNull);
    expect(
      sourceLabel.style!.color,
      equals(Theme.of(tester.element(find.byType(Scaffold).first)).colorScheme.error),
    );

    // 先卸载再挂载：DetailScreen 的 detail 只在 initState 加载，
    // 原地替换 widget 会复用 State 拿到上一个剧集的详情。
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    await tester.pumpWidget(
      MaterialApp(
        home: DetailScreen(
          drama: builtinDrama,
          repository: repository,
          store: store,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('片源：红果'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('简介里的收藏按钮：一键收藏片源，内置源不显示', (tester) async {
    tester.view.physicalSize = const Size(390, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    const runtimeDrama = Drama(
      id: 'libretv:mihua:one',
      source: 'libretv:mihua',
      title: '咪咕剧一',
      description: '这是一部来自咪咕采集站的短剧。',
      episodes: 2,
    );
    const builtinDrama = Drama(
      id: 'hongguo:one',
      source: 'hongguo',
      title: '红果剧一',
      description: '这是一部来自红果的短剧。',
      episodes: 2,
    );
    final repository = LibraryFeatureRepository()
      ..details[runtimeDrama.id] = LibraryFeatureRepository.makeDetail(
        runtimeDrama,
        2,
      )
      ..details[builtinDrama.id] = LibraryFeatureRepository.makeDetail(
        builtinDrama,
        2,
      );

    await tester.pumpWidget(
      MaterialApp(
        home: DetailScreen(
          drama: runtimeDrama,
          repository: repository,
          store: store,
        ),
      ),
    );
    await tester.pumpAndSettle();
    // 导入的 LibreTV 片源：片源行带一颗空心星（未收藏）。
    expect(find.byTooltip('收藏咪咕'), findsOneWidget);
    await tester.tap(find.byTooltip('收藏咪咕'));
    await tester.pumpAndSettle();
    // 与站源管理页同一份数据：收藏后首页把该片源独立成组。
    expect(store.isFavoriteSource('libretv:mihua'), isTrue);
    expect(find.byTooltip('取消收藏咪咕'), findsOneWidget);
    expect(find.text('已收藏「咪咕」，首页将独立显示该片源'), findsOneWidget);

    // 再点一次取消收藏。上一条提示还在屏上，而 SnackBar 是串行排队的：
    // 先推进时间让它退场，否则第二条会一直排在队列里，文本断言落空。
    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('取消收藏咪咕'));
    await tester.pumpAndSettle();
    expect(store.isFavoriteSource('libretv:mihua'), isFalse);
    expect(find.text('已取消收藏「咪咕」'), findsOneWidget);

    // 内置源（红果）没有收藏概念：不显示收藏按钮。
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    await tester.pumpWidget(
      MaterialApp(
        home: DetailScreen(
          drama: builtinDrama,
          repository: repository,
          store: store,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('片源：红果'), findsOneWidget);
    expect(find.byIcon(Icons.star_border_rounded), findsNothing);
    expect(find.byIcon(Icons.star_rounded), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
