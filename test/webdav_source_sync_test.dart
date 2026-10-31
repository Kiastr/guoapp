import 'dart:convert';

import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/drpy.dart';
import 'package:duanju_app/lan_sync_models.dart';
import 'package:duanju_app/libretv.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/webdav_sync.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_platform_interface.dart';

// WebDAV 片源同步（+124）：
// - 同步范围扩展到「片源订阅 + 收藏 / 隐藏状态」；
// - 偏好合并规则：per-source 最新时间戳优先，取消操作可传播
//   （靠取消墓碑实现，不是只增不减的并集）；
// - 对端设备导入、本机没有的 LibreTV / drpy 源在同步时自动导入，
//   幂等；粘贴导入的 drpy 脚本源无地址可同步，跳过并警告。

class FakeWebDavTransport implements WebDavTransport {
  FakeWebDavTransport({Map<String, String>? files})
    : files = files ?? <String, String>{};
  final Map<String, String> files;

  @override
  Future<void> ensureDirectory(Uri directory) async {}

  @override
  Future<List<WebDavRemoteFile>> list(Uri directory) async => [
    for (final entry in files.entries)
      WebDavRemoteFile(
        name: entry.key,
        uri: directory.resolve(entry.key),
        bytes: utf8.encode(entry.value).length,
      ),
  ];

  @override
  Future<String> read(Uri file) async {
    final content = files[file.pathSegments.last];
    if (content == null) throw StateError('远端没有此文件');
    return content;
  }

  @override
  Future<void> write(Uri file, String content) async {
    files[file.pathSegments.last] = content;
  }
}

/// 模拟原生核心的片源仓库：记录导入调用，导入后把站点登记进列表
/// （对应 Go 侧 libretv.json / drpy.json 落库），使同步器刷新运行时
/// 注册表后 `SourceSite.isKnown` 能识别新源。
class FakeSourceRepository extends AppRepository {
  FakeSourceRepository({
    List<LibreTVSite>? sites,
    List<DrpySite>? scripts,
  }) : _sites = sites ?? <LibreTVSite>[],
       _scripts = scripts ?? <DrpySite>[];

  List<LibreTVSite> _sites;
  List<DrpySite> _scripts;
  final List<String> libretvImports = [];
  final List<String> drpyImports = [];

  /// 会触发导入失败的地址（模拟失效订阅 / 脚本下载 404）。
  final Set<String> failures = {};

  @override
  Future<List<LibreTVSite>> libretvSites() async => _sites;

  @override
  Future<List<DrpySite>> drpySites() async => _scripts;

  @override
  Future<LibreTVImportResult> libretvImport(String subscription) async {
    libretvImports.add(subscription);
    // 同步侧现在传的是单站点订阅 JSON（裸地址 + key / name），失败判定
    // 同时兼容「整份载荷」与「载荷里含该地址」两种登记方式。
    if (failures.any(
      (f) => f == subscription || subscription.contains(f),
    )) {
      throw StateError('模拟导入失败');
    }
    final key = importedSiteKey(subscription);
    _sites = [
      ..._sites,
      // 与真实核心一致：订阅条目里带了 name 就用它，不带才回落地址。
      LibreTVSite(
        key: key,
        name: importedSiteName(subscription) ?? '导入站 $key',
        api: importedApiOf(subscription),
      ),
    ];
    return const LibreTVImportResult(
      added: [],
      skipped: [],
      drpy: [],
      verified: 0,
    );
  }

  @override
  Future<DrpyImportResult> drpyImport(String script) async {
    drpyImports.add(script);
    if (failures.contains(script)) throw StateError('模拟导入失败');
    final key = siteKeyOf(script);
    _scripts = [
      ..._scripts,
      DrpySite(key: key, name: '导入脚本 $key', scriptUrl: script),
    ];
    return DrpyImportResult(key: key, name: '导入脚本 $key');
  }

  // ---- 以下为 AppRepository 其余抽象方法的最小实现（本测试用不到） ----

  @override
  Future<void> initialize() async {}

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async => CatalogPage([]);

  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async =>
      CatalogPage([]);

  @override
  Future<String> cover(Drama drama, {bool force = false}) async => '';

  @override
  Future<DramaDetail> detail(Drama drama) async =>
      throw AppFailure('合成仓库没有详情');

  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
    int route = 0,
  }) async => const PlaybackPlan(url: 'https://example.test/synthetic.mp4');

  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) async =>
      throw AppFailure('没有备用线路');

  @override
  Future<void> cancelPlayback() async {}

  @override
  Future<void> release(String session) async {}

  @override
  Future<List<String>> playbackProbe() async => const [];
}

/// 测试约定的站点 key 派生规则：取 URL 主机名第一段。
/// 远端订阅条目与 FakeSourceRepository 导入登记共用，保证源 ID 一致。
String siteKeyOf(String url) => Uri.parse(url).host.split('.').first;

/// 同步侧交给核心的导入载荷里的唯一站点条目。
///
/// +154 起普通采集站也包装成单站点订阅 JSON（带 key / name）；规则源一直
/// 是订阅 JSON。裸地址（旧行为，仍用于对照）返回 null。
Map<String, dynamic>? importPayloadSite(String subscription) {
  final text = subscription.trim();
  if (!text.startsWith('{')) return null;
  final decoded = jsonDecode(text);
  if (decoded is! Map<String, dynamic>) return null;
  final sites = decoded['sites'];
  if (sites is! List || sites.isEmpty) return null;
  return sites.first is Map<String, dynamic>
      ? sites.first as Map<String, dynamic>
      : null;
}

/// 载荷里的站点 key；裸地址按主机名派生（与核心 libretvKeyFromAPI 同序）。
String importedSiteKey(String subscription) {
  final key = importPayloadSite(subscription)?['key'] as String? ?? '';
  return key.trim().isNotEmpty ? key : siteKeyOf(importedApiOf(subscription));
}

/// 载荷里的站名；为空表示同步侧没带名字（核心会回落成主机名）。
String? importedSiteName(String subscription) {
  final name = importPayloadSite(subscription)?['name'] as String?;
  return name == null || name.trim().isEmpty ? null : name;
}

/// 载荷里的采集地址（规则源这里是 Spider 地址）。
String importedApiOf(String subscription) =>
    importPayloadSite(subscription)?['api'] as String? ?? subscription;

/// 伪造一台「对端设备」已上传的同步文件（只带片源偏好 / 订阅，
/// 无追剧记录——记录合并已有 webdav_sync_test 覆盖）。
Map<String, String> remoteFiles({
  required String replica,
  Map<String, LanSourcePref> prefs = const {},
  Map<String, LanSourceSub> subs = const {},
}) {
  final document = LanDocument(
    replica: replica,
    sourcePrefs: prefs,
    sourceSubs: subs,
  );
  return {
    '$replica.json': jsonEncode({
      'schema': 1,
      'savedAt': DateTime.utc(2026, 10, 20, 12).toIso8601String(),
      'document': document.toJson(),
    }),
  };
}

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

Future<void> configure(LocalStore store) => store.saveWebDavSettings(
  const WebDavSettings(
    enabled: true,
    url: 'https://dav.example.com',
    username: 'user',
    password: 'pass',
  ),
);

Future<WebDavSyncOutcome> sync(
  LocalStore store,
  WebDavTransport transport,
  AppRepository? repository,
) => WebDavSynchronizer(
  store: store,
  transport: transport,
  repository: repository,
).synchronize();

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // SourceSite 运行时注册表是进程内全局状态：每个用例前后清空，
  // 避免「对端导入的源」泄漏到其他用例的 isKnown 判断里。
  setUp(() => SourceSite.setRuntimeSources(const []));
  tearDown(() => SourceSite.setRuntimeSources(const []));

  test('收藏与隐藏状态经 WebDAV 双向同步，取消操作可传播', () async {
    final a = await createStore();
    await configure(a);
    final transport = FakeWebDavTransport();
    await a.toggleFavoriteSource('libretv:aaa');
    await sync(a, transport, null);

    // 设备 B 首次同步即拿到 A 的收藏状态。
    final b = await createStore();
    await configure(b);
    await sync(b, transport, null);
    expect(b.isFavoriteSource('libretv:aaa'), isTrue);

    // B 追加隐藏，A 同步后生效。
    await b.toggleSourceHidden('libretv:aaa');
    await sync(b, transport, null);
    await sync(a, transport, null);
    expect(a.isHiddenSource('libretv:aaa'), isTrue);

    // A 取消收藏：取消墓碑随同步传播到 B（非只增不减并集）。
    await a.toggleFavoriteSource('libretv:aaa');
    await sync(a, transport, null);
    await sync(b, transport, null);
    expect(b.isFavoriteSource('libretv:aaa'), isFalse);
    expect(b.isHiddenSource('libretv:aaa'), isTrue);

    // 再次收敛同步不应产生新的偏好变化。
    final settled = await sync(b, transport, null);
    expect(settled.sourcePrefs, 0);
    expect(settled.hasChanges, isFalse);
  });

  test('旧版本地格式（无时间戳数组）兼容读取并参与同步', () async {
    SharedPreferences.resetStatic();
    SharedPreferences.setMockInitialValues({
      'favoriteSources': '["libretv:legacy"]',
    });
    final a = LocalStore(await SharedPreferences.getInstance());
    addTearDown(a.dispose);
    expect(a.isFavoriteSource('libretv:legacy'), isTrue);
    // 旧格式没有时间戳，按 0 处理：本机任何重新操作都会更新。
    expect(a.sourcePrefsSnapshot()['libretv:legacy']!.at, 0);

    await configure(a);
    final transport = FakeWebDavTransport();
    await sync(a, transport, null);
    final replica = a.lanDocument.replica;
    final uploaded = jsonDecode(transport.files['$replica.json']!) as Map;
    final document = uploaded['document'] as Map;
    expect(
      (document['sourcePrefs'] as Map)['libretv:legacy'],
      isNotNull,
    );

    final b = await createStore();
    await configure(b);
    await sync(b, transport, null);
    expect(b.isFavoriteSource('libretv:legacy'), isTrue);
  });

  test('对端片源订阅自动导入，且重复同步不重复导入', () async {
    final a = await createStore();
    await configure(a);
    final repository = FakeSourceRepository();
    final api = 'https://x.example.com/provide/vod';
    final transport = FakeWebDavTransport(
      files: remoteFiles(
        replica: 'b' * 32,
        subs: {
          'libretv:${siteKeyOf(api)}': LanSourceSub(
            name: '测试站',
            api: api,
            at: 2,
          ),
        },
      ),
    );

    final outcome = await sync(a, transport, repository);
    expect(repository.libretvImports, hasLength(1));
    // 导入载荷是单站点订阅 JSON：地址 + 源设备的 key / 站名（+154 起，
    // 修「同步过去站名变成网址」）。
    expect(importedApiOf(repository.libretvImports.single), api);
    expect(importedSiteKey(repository.libretvImports.single), siteKeyOf(api));
    expect(importedSiteName(repository.libretvImports.single), '测试站');
    expect(SourceSite.isKnown('libretv:${siteKeyOf(api)}'), isTrue);
    expect(outcome.sourceImports, 1);

    // 幂等：本机导入后再次同步不再重复导入。
    final again = await sync(a, transport, repository);
    expect(repository.libretvImports, hasLength(1));
    expect(again.sourceImports, 0);
  });

  test('同步导入携带站名与 key：对端不退化成主机名，源 ID 与源设备一致', () async {
    final before = SourceSite.runtimeValues;
    addTearDown(() => SourceSite.setRuntimeSources(before));
    final a = await createStore();
    await configure(a);
    final repository = FakeSourceRepository();
    const xmlApi = 'https://xml.example.com/provide/vod/at/xml';
    // 规则源同步过来的本身就是单站点订阅 JSON（含 key / name / 规则地址），
    // 同步侧原样交给核心，不做二次包装。
    const rulePayload = '{"sites":[{"key":"xbpq","name":"XBPQ 影视","type":3,'
        '"api":"http://127.0.0.1:9978/api/ext","ext":"http://r.example/x.json"}]}';
    final transport = FakeWebDavTransport(
      files: remoteFiles(
        replica: 'c' * 32,
        subs: {
          // 源 ID 用的就是站点 key：载荷必须把它带过去，否则对端按地址
          // 派生出另一个 key，每个同步周期都会被当成新源重新导入。
          'libretv:mihua': const LanSourceSub(name: '咪咕', api: xmlApi, at: 2),
          'libretv:xbpq': const LanSourceSub(
            name: 'XBPQ 影视',
            api: rulePayload,
            at: 2,
          ),
        },
      ),
    );

    final outcome = await sync(a, transport, repository);
    expect(outcome.sourceImports, 2);
    expect(repository.libretvImports, hasLength(2));
    final imports = <String, String>{
      for (final payload in repository.libretvImports)
        importedSiteKey(payload): payload,
    };
    // 普通采集站：key / name / type 都来自源设备条目；XML 地址判为 type 2
    // （与核心 looksLikeLibreTVXMLAPI 同口径）。名字非空 → 不会变网址。
    final xml = imports['mihua']!;
    expect(importedApiOf(xml), xmlApi);
    expect(importedSiteName(xml), '咪咕');
    expect((importPayloadSite(xml)!['type'] as num).toInt(), 2);
    // 规则源：同步过来的订阅 JSON 原样透传。
    expect(imports['xbpq'], rulePayload);
    // 站名进入运行时注册表（对端站源管理 / 首页显示的就是它）。
    expect(SourceSite.byId('libretv:mihua').name, '咪咕');
  });

  test('粘贴导入的 drpy 脚本源无法跨设备同步，跳过并警告', () async {
    final a = await createStore();
    await configure(a);
    final repository = FakeSourceRepository();
    final transport = FakeWebDavTransport(
      files: remoteFiles(
        replica: 'b' * 32,
        subs: const {
          'drpy:pasted': LanSourceSub(name: '粘贴脚本源', api: '', at: 2),
        },
      ),
    );

    final outcome = await sync(a, transport, repository);
    expect(repository.drpyImports, isEmpty);
    expect(outcome.sourceImports, 0);
    expect(
      outcome.warnings.any((warning) => warning.contains('粘贴导入')),
      isTrue,
    );
  });

  test('个别片源导入失败只记警告，不影响其余同步', () async {
    final a = await createStore();
    await configure(a);
    final repository = FakeSourceRepository()
      ..failures.add('https://bad.example.com/provide/vod');
    final transport = FakeWebDavTransport(
      files: remoteFiles(
        replica: 'b' * 32,
        subs: const {
          'libretv:bad': LanSourceSub(
            name: '坏站',
            api: 'https://bad.example.com/provide/vod',
            at: 2,
          ),
        },
      ),
    );

    final outcome = await sync(a, transport, repository);
    expect(outcome.sourceImports, 0);
    expect(
      outcome.warnings.any((warning) => warning.contains('失败')),
      isTrue,
    );
    // 同步本身完成：设置已保存，上传已发生。
    expect(outcome.savedAt, isNotNull);
    expect(transport.files.containsKey('${a.lanDocument.replica}.json'), isTrue);
  });

  test('applySourcePrefs：旧记录不回退、新记录覆盖、并列取并集', () async {
    final store = await createStore();
    await store.toggleFavoriteSource('libretv:aaa');
    final at = store.sourcePrefsSnapshot()['libretv:aaa']!.at;
    expect(store.isFavoriteSource('libretv:aaa'), isTrue);

    // 更旧的对端记录：不生效。
    expect(
      await store.applySourcePrefs({
        'libretv:aaa': const LanSourcePref(
          favorite: false,
          hidden: false,
          at: 0,
        ),
      }),
      0,
    );
    expect(store.isFavoriteSource('libretv:aaa'), isTrue);

    // 时间戳并列：对端同时刻的操作按字段取并集（隐藏生效、收藏保持）。
    expect(
      await store.applySourcePrefs({
        'libretv:aaa': LanSourcePref(
          favorite: true,
          hidden: true,
          at: at,
        ),
      }),
      1,
    );
    expect(store.isFavoriteSource('libretv:aaa'), isTrue);
    expect(store.isHiddenSource('libretv:aaa'), isTrue);

    // 更新的记录：取消传播（两个字段一起回到 false，按条目计 1 项）。
    expect(
      await store.applySourcePrefs({
        'libretv:aaa': LanSourcePref(
          favorite: false,
          hidden: false,
          at: at + 1,
        ),
      }),
      1,
    );
    expect(store.isFavoriteSource('libretv:aaa'), isFalse);
    expect(store.isHiddenSource('libretv:aaa'), isFalse);

    // 非运行时片源 ID 直接被忽略。
    expect(
      await store.applySourcePrefs({
        'hongguo': LanSourcePref(favorite: true, hidden: false, at: at + 2),
      }),
      0,
    );
  });

  test('重启后收藏 / 隐藏状态与取消墓碑保持', () async {
    final platform = InMemorySharedPreferencesStore.withData({});
    final store = await createStore(platform);
    await store.toggleFavoriteSource('libretv:aaa');
    await store.toggleSourceHidden('libretv:aaa');

    final restarted = await createStore(platform);
    expect(restarted.isFavoriteSource('libretv:aaa'), isTrue);
    expect(restarted.isHiddenSource('libretv:aaa'), isTrue);

    // 取消收藏后重启：偏好不回来，隐藏仍在。
    await restarted.toggleFavoriteSource('libretv:aaa');
    final again = await createStore(platform);
    expect(again.isFavoriteSource('libretv:aaa'), isFalse);
    expect(again.isHiddenSource('libretv:aaa'), isTrue);
    // 用户自己的收藏记录已清空，favoriteSourceIds 里剩下的只是出厂默认值。
    expect(again.explicitFavoriteSourceIds, isEmpty);
    expect(again.hiddenSourceIds, {'libretv:aaa'});
  });
}
