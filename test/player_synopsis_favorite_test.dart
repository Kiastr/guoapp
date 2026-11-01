// [+167] 播放页「简介」标签里的片源收藏按钮。
//
// 背景：用户反馈在播放页简介里根本找不到「收藏该片源」的入口——详情页
// （detail_screen）有星，但播放页的简介面板只显示标题 / 源信息 / 简介，
// 没有任何收藏入口；详情页早期还把内置源排除在外（只认 libretv:/drpy:）。
//
// 现在的约定：简介里的片源行带一颗收藏星，所有源都显示（与站源管理页
// 同一个按钮、同一份 LocalStore.toggleFavoriteSource）；收藏后首页把该
// 片源从所属大组拆出、独占一组，取消后回落。
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'player_fixtures.dart';

void main() {
  Future<void> settle(WidgetTester tester) async {
    for (var i = 0; i < 12; i++) {
      await tester.pump(const Duration(milliseconds: 10));
    }
  }

  testWidgets('播放页简介里可以收藏当前片源（简介为空也显示入口）', (tester) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final repository = FixtureRepository();
    final player = ScriptedPlayer();
    // FixtureRepository.free 没有简介，正好覆盖「暂无简介」时也有收藏入口。
    final detail = await repository.detail(FixtureRepository.free);

    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: PlayerScreen(
          detail: detail,
          initialIndex: 0,
          repository: repository,
          store: store,
          playerFactory: () => Player(platformPlayer: player),
          videoBuilder: (controls) => controls,
        ),
      ),
    );
    await settle(tester);

    await tester.tap(find.text('简介'));
    await tester.pumpAndSettle();
    expect(find.text('暂无简介'), findsOneWidget);
    // 内置源（红果）也显示收藏星：与详情页、站源管理页同一入口。
    expect(find.byTooltip('收藏红果'), findsOneWidget);

    await tester.tap(find.byTooltip('收藏红果'));
    await tester.pumpAndSettle();
    expect(store.isFavoriteSource('hongguo'), isTrue);
    expect(find.byTooltip('取消收藏红果'), findsOneWidget);
    expect(find.text('已收藏「红果」，首页将独立显示该片源'), findsOneWidget);

    // SnackBar 串行排队：先让它退场，再点第二次。
    await tester.pump(const Duration(seconds: 4));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('取消收藏红果'));
    await tester.pumpAndSettle();
    expect(store.isFavoriteSource('hongguo'), isFalse);
    expect(find.byTooltip('收藏红果'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    await settle(tester);
    expect(tester.takeException(), isNull);
  });
}
