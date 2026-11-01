import 'package:duanju_app/drpy.dart';
import 'package:duanju_app/kazumi.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/source_status.dart';
import 'package:duanju_app/sources_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_platform_interface.dart';

import 'fixtures.dart';

// 站源分组顺序（+157）：
// - 出厂默认顺序 [SourceGroup.defaultGroupOrder]：动漫源集合紧跟青空，
//   预制源集合排在动漫源集合之后，两者都在 drpy 脚本源集合之前，不再取决于
//   「分组由哪个源首次创建」
//   （青空被隐藏 / 当前用户无权访问时，动漫源集合只剩 Kazumi 运行时源撑着，
//   按旧逻辑会掉到 drpy 后面）；
// - 收藏独占的源（青空 / 韩小圈 / 某个 drpy 脚本）跟着所属分组排位，并
//   排在所属分组之前；
// - 站源管理页可上移 / 下移调整，写回本地偏好、重启后保留，可一键还原；
// - 顺序只在本机生效，不参与 WebDAV / 局域网同步。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SourceSite.setRuntimeSources([
      const DrpySite(key: 'demo', name: '演示脚本源').toSourceSite(),
      const KazumiRuleSite(
        key: 'xfdmnext',
        name: 'xfdmnext',
        host: 'api.example.test',
      ).toSourceSite(),
    ]);
  });

  tearDown(() {
    SourceSite.setRuntimeSources(const []);
  });

  Future<LocalStore> createStore([
    InMemorySharedPreferencesStore? platform,
  ]) async {
    SharedPreferences.resetStatic();
    if (platform == null) {
      SharedPreferences.setMockInitialValues({});
    } else {
      SharedPreferencesStorePlatform.instance = platform;
    }
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    return store;
  }

  test('出厂默认顺序：动漫源集合排在 drpy 脚本源集合之前', () {
    // 故意把 drpy 运行时源放在最前、且不传青空（模拟青空不可见）：
    // 旧逻辑下动漫源集合只能由 Kazumi 运行时源创建，会掉到 drpy 后面。
    final groups = SourceGroup.fromSources([
      SourceSite.hongguo,
      ...SourceSite.runtimeValues.where((site) => site.id.startsWith('drpy:')),
      ...SourceSite.runtimeValues.where(
        (site) => site.id.startsWith('kazumi:'),
      ),
    ]);
    final ids = groups.map((group) => group.id).toList();
    expect(ids, ['hongguo', 'anime', 'drpy']);
    expect(groups.firstWhere((group) => group.id == 'anime').name, '动漫源集合');
  });

  test('出厂默认顺序：预制源集合排在动漫源集合之后、drpy 之前', () {
    final groups = SourceGroup.fromSources([
      SourceSite.hongguo,
      ...SourceSite.runtimeValues.where((site) => site.id.startsWith('drpy:')),
      SourceSite.hanxiaoquan,
      SourceSite.guipian,
      ...SourceSite.runtimeValues.where(
        (site) => site.id.startsWith('kazumi:'),
      ),
    ]);
    expect(groups.map((group) => group.id).toList(), [
      'hongguo',
      'anime',
      'preset',
      'drpy',
    ]);
  });

  test('收藏独占组跟着所属分组排位，排在所属分组之前', () {
    final groups = SourceGroup.fromSources(
      [
        SourceSite.hongguo,
        ...SourceSite.runtimeValues.where(
          (site) => site.id.startsWith('drpy:'),
        ),
        SourceSite.sorani,
        SourceSite.hanxiaoquan,
        SourceSite.guipian,
      ],
      favoriteIds: const {'sorani', 'hanxiaoquan'},
    );
    expect(groups.map((group) => group.id).toList(), [
      'hongguo',
      'hanxiaoquan',
      'sorani',
      'anime',
      'preset',
      'drpy',
    ]);
  });

  test('用户排过的顺序优先，且缺省分组补在末尾', () async {
    final platform = InMemorySharedPreferencesStore.withData({});
    final store = await createStore(platform);
    expect(store.explicitSourceOrder, isEmpty);
    // 未排过时用出厂默认（sourceOrder 等于出厂表）。
    expect(store.sourceOrder, SourceGroup.defaultGroupOrder);

    await store.setSourceOrder(['drpy', 'hongguo']);
    expect(store.explicitSourceOrder, ['drpy', 'hongguo']);
    expect(store.sourceOrder.take(2).toList(), ['drpy', 'hongguo']);
    // 出厂表里没排到的分组补在后面，保证新分组也有确定位置。
    expect(store.sourceOrder, contains('anime'));

    final groups = SourceGroup.fromSources([
      SourceSite.hongguo,
      ...SourceSite.runtimeValues.where((site) => site.id.startsWith('drpy:')),
      SourceSite.sorani,
    ], groupOrder: store.sourceOrder);
    expect(groups.map((group) => group.id).toList(), [
      'drpy',
      'hongguo',
      'anime',
    ]);

    // 重启后仍在；传空列表即恢复出厂默认。
    final restarted = await createStore(platform);
    expect(restarted.explicitSourceOrder, ['drpy', 'hongguo']);
    await restarted.setSourceOrder(const []);
    expect(restarted.explicitSourceOrder, isEmpty);
    expect(restarted.sourceOrder, SourceGroup.defaultGroupOrder);
  });

  testWidgets('站源管理页可上移 / 下移分组，并可恢复默认顺序', (tester) async {
    final platform = InMemorySharedPreferencesStore.withData({});
    SharedPreferences.resetStatic();
    SharedPreferencesStorePlatform.instance = platform;
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final repository = _OrderFixtureRepository();
    await tester.pumpWidget(
      MaterialApp(
        home: SourcesScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();

    expect(store.explicitSourceOrder, isEmpty);
    // 未调整过时没有「恢复默认顺序」按钮。
    expect(find.byKey(const ValueKey('source-order-reset')), findsNothing);

    // 红果（第一个）的上移按钮不可点，下移可点。
    expect(
      tester
          .widget<IconButton>(
            find.byKey(const ValueKey('source-order-up-hongguo')),
          )
          .onPressed,
      isNull,
    );
    await tester.tap(find.byKey(const ValueKey('source-order-down-hongguo')));
    await tester.pumpAndSettle();

    expect(store.explicitSourceOrder.first, isNot('hongguo'));
    expect(store.explicitSourceOrder[1], 'hongguo');

    // 调过顺序后出现「恢复默认顺序」，点了回到出厂默认。
    expect(find.byKey(const ValueKey('source-order-reset')), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('source-order-reset')));
    await tester.pumpAndSettle();
    expect(store.explicitSourceOrder, isEmpty);
    expect(store.sourceOrder, SourceGroup.defaultGroupOrder);
  });
}

/// 站源管理页的最小桩：只提供状态查询与任务入口，其余走 [FixtureRepository]
/// 默认实现（导入接口抛错时页面会静默跳过）。
class _OrderFixtureRepository extends FixtureRepository {
  final statuses = <String, SourceStatus>{};

  /// 页面初始化会把运行时源注册表整体替换成这三个接口的结果，所以要用
  /// 站源撑出多个分组（红果 / 动漫源集合 / drpy 脚本源集合）才能调顺序。
  @override
  Future<List<DrpySite>> drpySites() async => const [
    DrpySite(key: 'demo', name: '演示脚本源'),
  ];

  @override
  Future<List<KazumiRuleSite>> kazumiSites() async => const [
    KazumiRuleSite(key: 'xfdmnext', name: 'xfdmnext', host: 'api.example.test'),
  ];

  @override
  bool get supportsSourceManagement => true;

  @override
  Future<SourceStatus> sourceStatus(String source) async =>
      statuses[source] ??= SourceStatus.fromJson({
        'source': source,
        'count': 1,
        'page': 1,
        'hasMore': false,
      });
}
