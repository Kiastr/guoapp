import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_platform_interface.dart';

// 运行时片源（LibreTV / drpy）的收藏与隐藏：
// - 收藏：SourceGroup 分组时独占一组（与红果同级），不并入 LibreTV / drpy 大组；
// - 隐藏：与黄果等敏感源同逻辑——sources 不再列出、allowsSource 拒绝；
// - 两者都要在重启后保持（持久化在 local_store 内）。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    SourceSite.setRuntimeSources([
      const SourceSite('libretv:mihua', '咪咕', '采集站'),
      const SourceSite('drpy:jianpian', '荐片', 'drpy 脚本'),
    ]);
  });

  tearDown(() {
    SourceSite.setRuntimeSources(const []);
  });

  /// 建库；[platform] 非空时复用它（模拟重启：同一份持久化数据、
  /// 新的 LocalStore 实例）。
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

  test('收藏的运行时片源独占一组，未收藏的仍并入 LibreTV / drpy 大组', () {
    final sources = SourceSite.values;
    final plain = SourceGroup.fromSources(sources);
    final libretvGroup = plain.firstWhere((group) => group.id == 'libretv');
    expect(libretvGroup.sources.length, 1);
    expect(libretvGroup.name, '扩展片源集合');

    final favorited = SourceGroup.fromSources(
      sources,
      favoriteIds: const {'libretv:mihua'},
    );
    // 收藏后独立成组，组名就是源名，与内置源同级。
    final own = favorited.firstWhere((group) => group.id == 'libretv:mihua');
    expect(own.name, '咪咕');
    expect(own.sources.single.id, 'libretv:mihua');
    // [+156] 收藏的源在 LibreTV 大组里仍保留一份：既能单独选，也能和大组
    // 里的同族源一起选。
    final libretvFav = favorited.firstWhere((group) => group.id == 'libretv');
    expect(libretvFav.name, '扩展片源集合');
    expect(libretvFav.sources.map((site) => site.id), contains('libretv:mihua'));
    // drpy 未收藏，仍在 drpy 大组里。
    final drpyGroup = favorited.firstWhere((group) => group.id == 'drpy');
    expect(drpyGroup.sources.single.id, 'drpy:jianpian');
  });

  test('收藏状态持久化，重启后仍独立分组', () async {
    final platform = InMemorySharedPreferencesStore.withData({});
    final store = await createStore(platform);
    expect(store.isFavoriteSource('libretv:mihua'), isFalse);
    await store.toggleFavoriteSource('libretv:mihua');
    // favoriteSourceIds 还包含出厂默认收藏的内置源（青空 / 韩小圈），
    // 断言用户自己的设置时用 explicitFavoriteSourceIds。
    expect(store.explicitFavoriteSourceIds, {'libretv:mihua'});
    expect(store.favoriteSourceIds, contains('libretv:mihua'));

    final restarted = await createStore(platform);
    expect(restarted.isFavoriteSource('libretv:mihua'), isTrue);
    final groups = SourceGroup.fromSources(
      restarted.sources,
      favoriteIds: restarted.favoriteSourceIds,
    );
    expect(groups.map((group) => group.id), contains('libretv:mihua'));
    // 大组里也仍有一份（+156）。
    expect(groups.map((group) => group.id), contains('libretv'));
  });

  test('隐藏的片源与敏感源同逻辑：不可见、不允许，可恢复', () async {
    final platform = InMemorySharedPreferencesStore.withData({});
    final store = await createStore(platform);
    expect(store.sources.map((site) => site.id), contains('libretv:mihua'));
    expect(store.allowsSource('libretv:mihua'), isTrue);

    await store.toggleSourceHidden('libretv:mihua');
    expect(store.sources.map((site) => site.id), isNot(contains('libretv:mihua')));
    expect(store.allowsSource('libretv:mihua'), isFalse);
    // 其他片源不受影响。
    expect(store.allowsSource('drpy:jianpian'), isTrue);

    // 重启后保持隐藏。
    final restarted = await createStore(platform);
    expect(restarted.allowsSource('libretv:mihua'), isFalse);

    // 恢复后重新可见。
    await restarted.toggleSourceHidden('libretv:mihua');
    expect(restarted.allowsSource('libretv:mihua'), isTrue);
    expect(restarted.sources.map((site) => site.id), contains('libretv:mihua'));
  });

  test('内置源可以收藏（出厂默认可取消），但不能隐藏', () async {
    final store = await createStore();
    // [+155] 内置源同样能收藏：青空 / 韩小圈出厂默认就在首页与红果同级，
    // 取消收藏回落到所属分组的子级，所以这里不再抛错。
    expect(store.isFavoriteSource('hongguo'), isFalse);
    await store.toggleFavoriteSource('hongguo');
    expect(store.isFavoriteSource('hongguo'), isTrue);
    await store.toggleFavoriteSource('hongguo');
    expect(store.isFavoriteSource('hongguo'), isFalse);
    // 隐藏仍然只针对导入的片源：内置源隐藏会让首页无从选择。
    await expectLater(
      store.toggleSourceHidden('hongguo'),
      throwsStateError,
    );
  });

  test('收藏与隐藏互不干扰：可以先收藏再隐藏，恢复显示后仍是收藏态', () async {
    final store = await createStore();
    await store.toggleFavoriteSource('drpy:jianpian');
    await store.toggleSourceHidden('drpy:jianpian');
    expect(store.isFavoriteSource('drpy:jianpian'), isTrue);
    expect(store.isHiddenSource('drpy:jianpian'), isTrue);
    // 隐藏优先：即使收藏，也不出现在首页片源里。
    expect(store.sources.map((site) => site.id), isNot(contains('drpy:jianpian')));

    await store.toggleSourceHidden('drpy:jianpian');
    expect(store.sources.map((site) => site.id), contains('drpy:jianpian'));
    expect(store.isFavoriteSource('drpy:jianpian'), isTrue);
  });
}
