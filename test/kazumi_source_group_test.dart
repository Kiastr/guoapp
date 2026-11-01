import 'package:duanju_app/kazumi.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_platform_interface.dart';

// Kazumi 规则源（+155）与站源重分组：
// - Kazumi 规则源是运行时源（kazumi:<key>），默认并入「动漫站源」；
// - 内置源按主题合并：青空 + Kazumi → 动漫源集合，韩小圈 + 鬼片 → 预制源集合；
// - 出厂默认只收藏青空：它在首页独占一组、与红果同级，同时仍留在动漫源集合
//   里（+156：既能单独选、也能在组里和同族源一起选）；韩小圈不再默认收藏，
//   回到预制源集合整组一个入口。取消收藏后只剩分组入口；用户没动过的源才用
//   出厂默认值。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SourceSite.setRuntimeSources([
      const KazumiRuleSite(
        key: 'xfdmnext',
        name: 'xfdmnext',
        host: 'api.example.test',
      ).toSourceSite(),
      const KazumiRuleSite(
        key: 'giriGiriLove',
        name: 'giriGiriLove',
        host: 'm3u8.example.test',
      ).toSourceSite(),
    ]);
  });

  tearDown(() {
    SourceSite.setRuntimeSources(const []);
  });

  /// 测试用的站源全集。
  ///
  /// 不用 [SourceSite.values]：它受 `ALL_SOURCES` 编译开关影响，CI 会跑
  /// 带 / 不带该 dart-define 两遍，分组断言要在两遍里都成立，所以这里固定
  /// 取「内置主站源 + 已注册的运行时源」。
  List<SourceSite> testSources() => [
    ...SourceSite.primaryValues,
    ...SourceSite.runtimeValues,
  ];

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

  test('Kazumi 规则源归入动漫源集合：未收藏时与青空同一组', () {
    final groups = SourceGroup.fromSources(testSources());
    final anime = groups.firstWhere((group) => group.id == 'anime');
    expect(anime.name, '动漫源集合');
    expect(
      anime.sources.map((site) => site.id),
      containsAll(['sorani', 'kazumi:xfdmnext', 'kazumi:giriGiriLove']),
    );
  });

  test('预制源集合：韩小圈与鬼片同组，未收藏时整组是一个入口', () {
    final groups = SourceGroup.fromSources(testSources());
    final preset = groups.firstWhere((group) => group.id == 'preset');
    expect(preset.name, '预制源集合');
    expect(
      preset.sources.map((site) => site.id),
      containsAll(['hanxiaoquan', 'guipian']),
    );
  });

  test('出厂默认只收藏青空：韩小圈回到预制源集合，不再独占一组', () async {
    final store = await createStore();
    expect(store.isFavoriteSource('sorani'), isTrue);
    // [+163] 韩小圈不再默认收藏：预制源集合整组一个入口，不再与红果同级重复。
    expect(store.isFavoriteSource('hanxiaoquan'), isFalse);
    expect(store.isFavoriteSource('guipian'), isFalse);

    final groups = SourceGroup.fromSources(
      testSources(),
      favoriteIds: store.favoriteSourceIds,
    );
    final ids = groups.map((group) => group.id).toList();
    expect(ids, contains('sorani'));
    expect(ids, isNot(contains('hanxiaoquan')));
    // [+156] 收藏后仍留在所属分组里：动漫源集合 = 青空 + Kazumi 规则源，
    // 既能单独选「青空」，也能在组里和同族源一起选。
    final anime = groups.firstWhere((group) => group.id == 'anime');
    expect(anime.name, '动漫源集合');
    expect(
      anime.sources.map((site) => site.id),
      containsAll(['sorani', 'kazumi:xfdmnext', 'kazumi:giriGiriLove']),
    );
    // 韩小圈不收藏 → 老老实实待在预制源集合：韩小圈 + 鬼片。
    final preset = groups.firstWhere((group) => group.id == 'preset');
    expect(preset.name, '预制源集合');
    expect(
      preset.sources.map((site) => site.id),
      containsAll(['hanxiaoquan', 'guipian']),
    );
  });

  test('取消收藏后只剩分组入口，且能再收藏回来', () async {
    final platform = InMemorySharedPreferencesStore.withData({});
    final store = await createStore(platform);
    await store.toggleFavoriteSource('sorani');
    expect(store.isFavoriteSource('sorani'), isFalse);
    var groups = SourceGroup.fromSources(
      testSources(),
      favoriteIds: store.favoriteSourceIds,
    );
    expect(groups.map((group) => group.id), isNot(contains('sorani')));
    final anime = groups.firstWhere((group) => group.id == 'anime');
    expect(anime.sources.map((site) => site.id), contains('sorani'));

    // 取消过的源重启后仍是取消态（出厂默认只填「用户没动过」的坑）。
    final restarted = await createStore(platform);
    expect(restarted.isFavoriteSource('sorani'), isFalse);

    // 再收藏回来。
    await restarted.toggleFavoriteSource('sorani');
    expect(restarted.isFavoriteSource('sorani'), isTrue);
    groups = SourceGroup.fromSources(
      testSources(),
      favoriteIds: restarted.favoriteSourceIds,
    );
    expect(groups.map((group) => group.id), contains('sorani'));
  });

  test('Kazumi 规则源模型：源 ID 与展示标签', () {
    const site = KazumiRuleSite(
      key: 'giriGiriLove',
      name: 'giriGiriLove',
      version: '2.6',
      host: 'm3u8.example.test',
    );
    expect(site.sourceId, 'kazumi:giriGiriLove');
    expect(site.versionLabel, '规则 v2.6');
    expect(site.detailLabel, 'm3u8.example.test');
    expect(site.toSourceSite().isRuntime, isTrue);
    expect(site.toSourceSite().groupId, 'anime');
    expect(site.toSourceSite().groupName, '动漫源集合');
    // 需要嗅探的规则在 UI 上要说明下载不可用。
    expect(
      const KazumiRuleSite(key: 'a', name: 'a', webview: true).hint,
      contains('不支持下载'),
    );
  });
}
