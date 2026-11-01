// [+166] 分类加载的代际取消（「正在处理的请求较多，请稍后重试」的根治）。
//
// 背景：分类加载会重入（切分组 / 从源管理返回 / 刷新都会再跑一轮），而
// `cancelCatalog()` 只取消 `catalog-` 前缀——启动时它和目录加载是并行的，
// 不能连分类一起掐掉。于是旧一轮的 `categories-` 请求会一路跑完几十个源
// （单请求最长 60 秒），几十个源的组上反复切几次就叠满 Go 侧 48 个读槽。
//
// 现在的约定：
// - 每开一轮网络分类加载，先取消上一轮在途的分类请求（槽位立刻归还）；
// - 旧轮的 worker 看到代际变了就不再往队列里发新请求；
// - 只读本机缓存的轮次（cacheOnly）不接管，不打断正在跑的网络轮次。
import 'dart:async';

import 'package:duanju_app/catalog_browser.dart';
import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fixtures.dart';

/// 可控的分类请求：默认挂起，直到 [releaseAll] 放行。
/// [cancelCategories] 让所有在途请求立刻以失败结束——即 Go 侧 cancelRead
/// 取消读请求后 Dart 侧看到的行为。
class _HangingCategoryRepository extends FixtureRepository {
  final categoryRequests = <String>[];
  final _pending = <Completer<List<CatalogCategory>>>{};
  int cancelCalls = 0;
  bool hang = true;

  @override
  Future<void> cancelCategories() async {
    cancelCalls++;
    final pending = _pending.toList();
    _pending.clear();
    for (final completer in pending) {
      if (!completer.isCompleted) {
        completer.completeError(AppFailure('分类请求已取消'));
      }
    }
  }

  @override
  Future<List<CatalogCategory>> categories(
    String source, {
    bool force = false,
  }) {
    categoryRequests.add(source);
    if (!hang) return Future.value(const [CatalogCategory.all]);
    final completer = Completer<List<CatalogCategory>>();
    _pending.add(completer);
    return completer.future;
  }

  void releaseAll() {
    final pending = _pending.toList();
    _pending.clear();
    for (final completer in pending) {
      if (!completer.isCompleted) {
        completer.complete(const [CatalogCategory.all]);
      }
    }
  }
}

SourceGroup _group(int count) => SourceGroup('test', '测试集合', [
  for (var i = 0; i < count; i++) SourceSite('test:$i', '源$i', ''),
]);

/// 放行若干轮微任务 + 事件队列，让并发 worker 推进到下一个等待点。
Future<void> _settle() async {
  for (var i = 0; i < 5; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  test('新一轮分类加载接管后，旧轮不再补发剩余站源', () async {
    final repository = _HangingCategoryRepository();
    final browser = CatalogBrowser(repository);
    final group = _group(12);

    final first = browser.loadCategories(group);
    await _settle();
    expect(repository.categoryRequests.length, 8, reason: '并发上限 8');

    final second = browser.loadCategories(group);
    await _settle();
    expect(repository.cancelCalls, 2, reason: '每轮开场都取消上一轮（首轮无在途）');
    expect(
      repository.categoryRequests.length,
      16,
      reason: '旧轮被接管后停手，不会再把剩下的 4 个源发出去',
    );

    await first;
    expect(repository.categoryRequests.length, 16);

    // 放行当前轮：它没有被接管，应当把 12 个源全部跑完。
    repository.hang = false;
    repository.releaseAll();
    await _settle();
    await second;
    expect(
      repository.categoryRequests.length,
      20,
      reason: '第二轮 8 个已在途 + 剩余 4 个',
    );
  });

  test('只读缓存的轮次不会取消正在跑的网络轮次', () async {
    final repository = _HangingCategoryRepository();
    final browser = CatalogBrowser(repository);
    final group = _group(12);

    final first = browser.loadCategories(group);
    await _settle();
    expect(repository.categoryRequests.length, 8);

    final cacheOnly = await browser.loadCategories(group, cacheOnly: true);
    await _settle();
    expect(cacheOnly, isNull);
    expect(repository.cancelCalls, 1, reason: '只有首轮开场那一次；cacheOnly 轮次不接管');
    expect(
      repository.categoryRequests.length,
      8,
      reason: 'cacheOnly 只读本机缓存，不发分类请求',
    );

    repository.hang = false;
    for (var i = 0; i < 3; i++) {
      repository.releaseAll();
      await _settle();
    }
    await first;
    expect(repository.categoryRequests.length, 12);
  });

  test('切到另一个分组也会停掉上一组的分类请求', () async {
    final repository = _HangingCategoryRepository();
    final browser = CatalogBrowser(repository);
    final source = _group(12);
    final target = SourceGroup('other', '另一个集合', const [
      SourceSite('other:1', '源甲', ''),
      SourceSite('other:2', '源乙', ''),
    ]);

    final first = browser.loadCategories(source);
    await _settle();
    expect(repository.categoryRequests.length, 8);

    final second = browser.loadCategories(target);
    await _settle();
    expect(repository.categoryRequests.length, 10, reason: '目标组只有 2 个源');
    expect(
      repository.categoryRequests.where((id) => id.startsWith('test:')).length,
      8,
      reason: '上一组被取消，剩余 4 个源不再发',
    );

    repository.hang = false;
    repository.releaseAll();
    await _settle();
    expect(await second, isNull);
    await first;
  });
}
