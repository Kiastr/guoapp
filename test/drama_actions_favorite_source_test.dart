import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/main.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

// 卡片「⋯」菜单里的源级收藏入口：
// - 菜单里固定出现「收藏此站源」，与详情页片源行、播放页简介共用同一份状态；
// - 点一下收藏，再打开菜单变成「取消收藏此站源」，再点回落到未收藏。
void main() {
  Future<LocalStore> store() async {
    SharedPreferences.setMockInitialValues({});
    return LocalStore(await SharedPreferences.getInstance());
  }

  testWidgets('卡片菜单收藏 / 取消收藏当前站源', (tester) async {
    final repository = FixtureRepository();
    final local = await store();
    await tester.pumpWidget(DuanjuApp(repository: repository, store: local));
    await tester.pumpAndSettle();

    // 红果（测试剧所在源）出厂不收藏。
    expect(local.isFavoriteSource('hongguo'), isFalse);

    final more = find.byKey(const ValueKey('drama-actions-hongguo:100'));
    await tester.tap(more);
    await tester.pumpAndSettle();
    expect(find.text('收藏此站源'), findsOneWidget);

    await tester.tap(find.text('收藏此站源'));
    await tester.pumpAndSettle();
    expect(local.isFavoriteSource('hongguo'), isTrue);
    expect(find.text('已收藏「红果」，首页将独立显示该片源'), findsOneWidget);

    // 收掉提示，免得挡住下一次点击。
    await tester.pump(const Duration(seconds: 5));

    await tester.tap(more);
    await tester.pumpAndSettle();
    expect(find.text('取消收藏此站源'), findsOneWidget);
    expect(find.text('收藏此站源'), findsNothing);

    await tester.tap(find.text('取消收藏此站源'));
    await tester.pumpAndSettle();
    expect(local.isFavoriteSource('hongguo'), isFalse);
    expect(find.text('已取消收藏「红果」'), findsOneWidget);
  });
}
