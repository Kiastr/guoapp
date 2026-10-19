import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:duanju_app/catalog_filters.dart';
import 'package:duanju_app/models.dart';

// 分类标签栏的下拉交互：轻拉刷新、重拉展开「分类面板」，
// 面板内可按默认 / 字母 / 倒序重排分类。
void main() {
  const categories = [
    CatalogCategory.all,
    CatalogCategory('a', 'AI 剧'),
    CatalogCategory('m', '漫剧'),
    CatalogCategory('z', '综合'),
  ];

  Widget mount({
    ValueChanged<String>? onCategory,
    VoidCallback? onRefresh,
  }) => MaterialApp(
    home: Scaffold(
      body: Column(
        children: [
          CatalogFilters(
            categories: categories,
            category: 'a',
            onCategory: onCategory ?? (_) {},
            onRefresh: onRefresh ?? () {},
            onRetry: () {},
          ),
        ],
      ),
    ),
  );

  test('下拉阈值：轻拉刷新、重拉展开、微拖忽略', () {
    expect(catalogPullAction(0, 0), CatalogPullAction.none);
    expect(catalogPullAction(10, 0), CatalogPullAction.none);
    expect(catalogPullAction(40, 0), CatalogPullAction.refresh);
    // 快速轻甩也算刷新。
    expect(catalogPullAction(5, 300), CatalogPullAction.refresh);
    // 拖得够深或甩得够快才展开面板。
    expect(catalogPullAction(120, 0), CatalogPullAction.expand);
    expect(catalogPullAction(5, 1000), CatalogPullAction.expand);
  });

  test('排序：默认保持原序，字母排序与倒序生效', () {
    expect(sortCategories(categories, CatalogCategorySort.original).map((c) => c.name), [
      '全部',
      'AI 剧',
      '漫剧',
      '综合',
    ]);
    // 英文按字母在前，中文按 Unicode 码位（不等于拼音，码位分组即可预期）。
    final alpha = sortCategories(categories, CatalogCategorySort.alphabetical)
        .map((c) => c.name)
        .toList();
    expect(alpha.first, 'AI 剧');
    final reverse = sortCategories(categories, CatalogCategorySort.reverse)
        .map((c) => c.name)
        .toList();
    expect(reverse.first, alpha.last);
  });

  testWidgets('展开按钮打开分类面板，可排序并选择分类', (tester) async {
    String? selected;
    await tester.pumpWidget(
      mount(onCategory: (id) => selected = id),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const ValueKey('catalog-panel-open')));
    await tester.pumpAndSettle();
    expect(find.text('分类面板'), findsOneWidget);

    // 默认顺序：全部在最前。
    expect(
      find.descendant(
        of: find.byType(SingleChildScrollView),
        matching: find.text('全部'),
      ),
      findsWidgets,
    );

    // 切到字母排序后点一个分类：回调 + 关闭面板。
    await tester.tap(find.byKey(const ValueKey('catalog-panel-sort-alphabetical')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('catalog-panel-m')));
    await tester.pumpAndSettle();
    expect(selected, 'm');
    expect(find.text('分类面板'), findsNothing);
  });

  testWidgets('重拉展开面板、轻拉只刷新', (tester) async {
    var refreshes = 0;
    await tester.pumpWidget(mount(onRefresh: () => refreshes++));
    await tester.pumpAndSettle();

    // 轻拉：距离 40px，只触发刷新。
    await tester.drag(
      find.byType(CatalogFilters),
      const Offset(0, 40),
      touchSlopY: 0,
    );
    await tester.pumpAndSettle();
    expect(refreshes, 1);
    expect(find.text('分类面板'), findsNothing);

    // 重拉：距离 160px，打开面板。
    await tester.drag(
      find.byType(CatalogFilters),
      const Offset(0, 160),
      touchSlopY: 0,
    );
    await tester.pumpAndSettle();
    expect(find.text('分类面板'), findsOneWidget);
    // 刷新没有被重拉误触。
    expect(refreshes, 1);
  });
}
