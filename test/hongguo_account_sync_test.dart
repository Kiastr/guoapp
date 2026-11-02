import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/hongguo_account.dart';
import 'package:duanju_app/hongguo_account_sync.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _AccountRepository extends FixtureRepository {
  _AccountRepository(this.snapshot);

  HongguoAccountSnapshot snapshot;
  Object? failure;
  int calls = 0;

  @override
  Future<HongguoAccountSnapshot> hongguoAccountSync() async {
    calls++;
    if (failure != null) throw failure!;
    return snapshot;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('账号收藏与观看记录并入本地，并记下同步时间', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    final repository = _AccountRepository(
      const HongguoAccountSnapshot(
        signedIn: true,
        nickname: '测试账号',
        syncedAt: 1760000000,
        favorites: [
          HongguoAccountEntry(
            seriesId: '7300000000000000001',
            title: '账号收藏的剧',
            episodes: 66,
            category: '都市',
          ),
        ],
        history: [
          HongguoAccountEntry(
            seriesId: '7300000000000000002',
            title: '看到一半的剧',
            episodes: 30,
            episode: 12,
            episodeId: '7400000000000000009',
            position: 125,
            duration: 600,
            updatedAt: 1760000000000,
          ),
        ],
      ),
    );

    final outcome = await syncHongguoAccount(repository, store);

    expect(outcome.ok, isTrue);
    expect(outcome.signedIn, isTrue);
    expect(outcome.merged, 2);
    expect(store.isFavorite('hongguo:7300000000000000001'), isTrue);
    final entry = store.watched('hongguo:7300000000000000002');
    expect(entry, isNotNull);
    expect(entry!.episode, 12);
    expect(entry.position, 125);
    expect(entry.duration, 600);
    expect(entry.drama.source, SourceSite.hongguo.id);
    expect(store.hongguoAccountSyncAt, 1760000000);
  });

  test('云端记录更旧时不覆盖本机进度', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    const drama = Drama(
      id: 'hongguo:7300000000000000003',
      source: 'hongguo',
      sourceId: '7300000000000000003',
      title: '本机记录',
    );
    await store.saveWatch(
      WatchEntry(
        drama: drama,
        episode: 8,
        position: 300,
        duration: 600,
        updatedAt: DateTime(2026, 10, 1),
      ),
    );
    final repository = _AccountRepository(
      HongguoAccountSnapshot(
        signedIn: true,
        history: [
          HongguoAccountEntry(
            seriesId: '7300000000000000003',
            title: '云端旧记录',
            episode: 2,
            episodeId: '7400000000000000010',
            position: 10,
            duration: 600,
            updatedAt: DateTime(2026, 9, 1).millisecondsSinceEpoch,
          ),
        ],
      ),
    );

    final outcome = await syncHongguoAccount(repository, store);

    expect(outcome.ok, isTrue);
    expect(
      outcome.merged,
      0,
      reason:
          'local=${store.watched(drama.id)?.updatedAt.toIso8601String()} '
          'episode=${store.watched(drama.id)?.episode} '
          'incoming=${DateTime(2026, 9, 1).toIso8601String()}',
    );
    final entry = store.watched(drama.id);
    expect(entry!.episode, 8);
    expect(entry.position, 300);
    expect(entry.drama.title, '本机记录');
  });

  test('已有收藏不被账号数据覆盖', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    const drama = Drama(
      id: 'hongguo:7300000000000000004',
      source: 'hongguo',
      sourceId: '7300000000000000004',
      title: '本机标题',
      episodes: 10,
    );
    await store.toggleFavorite(drama);
    final repository = _AccountRepository(
      const HongguoAccountSnapshot(
        signedIn: true,
        favorites: [
          HongguoAccountEntry(
            seriesId: '7300000000000000004',
            title: '账号标题',
            episodes: 99,
          ),
        ],
      ),
    );

    final outcome = await syncHongguoAccount(repository, store);

    expect(outcome.merged, 0);
    final favorite = store.favorites.firstWhere((item) => item.id == drama.id);
    expect(favorite.title, '本机标题');
    expect(favorite.episodes, 10);
  });

  test('未登录不报错也不写盘', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    final repository = _AccountRepository(const HongguoAccountSnapshot());

    final outcome = await syncHongguoAccount(repository, store);

    expect(outcome.ok, isTrue);
    expect(outcome.signedIn, isFalse);
    expect(outcome.summary, '未登录红果账号');
    expect(store.favorites, isEmpty);
    expect(store.history, isEmpty);
  });

  test('仓库异常收敛成文案，不向上抛', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    final repository = _AccountRepository(const HongguoAccountSnapshot())
      ..failure = AppFailure('登录态已失效，请重新扫码');

    final outcome = await syncHongguoAccount(repository, store);

    expect(outcome.ok, isFalse);
    expect(outcome.summary, '登录态已失效，请重新扫码');
  });

  test('自动同步默认开启，可以关闭', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    expect(store.hongguoAccountAutoSync, isTrue);
    await store.setHongguoAccountAutoSync(false);
    expect(store.hongguoAccountAutoSync, isFalse);
  });

  test('部分失败时仍保留成功的那一半', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    final repository = _AccountRepository(
      const HongguoAccountSnapshot(
        signedIn: true,
        favorites: [
          HongguoAccountEntry(seriesId: '7300000000000000005', title: '能同步的收藏'),
        ],
        warning: '观看记录没同步成功：红果账号接口 HTTP 502',
      ),
    );

    final outcome = await syncHongguoAccount(repository, store);

    expect(outcome.ok, isTrue);
    expect(outcome.merged, 1);
    expect(outcome.warning, contains('观看记录没同步成功'));
    expect(store.isFavorite('hongguo:7300000000000000005'), isTrue);
  });

  test('账号收藏按「最新在前」落库，不随快照的倒序翻转', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);

    // 快照按收藏时间倒序下发（最新在最前），本地要落成「旧 → 新」，
    // 列表页反转后才是「新 → 旧」。
    await store.mergeHongguoAccount(
      const HongguoAccountSnapshot(
        signedIn: true,
        favorites: [
          HongguoAccountEntry(
            seriesId: '7300000000000000013',
            title: '最新',
            collectedAt: 1760000003000,
          ),
          HongguoAccountEntry(
            seriesId: '7300000000000000012',
            title: '中间',
            collectedAt: 1760000002000,
          ),
          HongguoAccountEntry(
            seriesId: '7300000000000000011',
            title: '最早',
            collectedAt: 1760000001000,
          ),
        ],
      ),
    );

    expect(store.favorites.map((drama) => drama.title).toList(), [
      '最新',
      '中间',
      '最早',
    ]);
  });

  test('按倒序落过库的账号收藏会被重排回正确顺序', () async {
    final prefs = await SharedPreferences.getInstance();
    final store = LocalStore(prefs);
    // 旧版本留下的状态：倒序写入，列表里最早的在最上面。
    for (final row in [
      ('7300000000000000023', '最新', 1760000003000),
      ('7300000000000000022', '中间', 1760000002000),
      ('7300000000000000021', '最早', 1760000001000),
    ]) {
      await store.toggleFavorite(
        Drama(
          id: 'hongguo:${row.$1}',
          source: 'hongguo',
          sourceId: row.$1,
          title: row.$2,
        ),
      );
    }
    expect(store.favorites.map((drama) => drama.title).toList(), [
      '最早',
      '中间',
      '最新',
    ]);

    final merged = await store.mergeHongguoAccount(
      const HongguoAccountSnapshot(
        signedIn: true,
        favorites: [
          HongguoAccountEntry(
            seriesId: '7300000000000000023',
            title: '最新',
            collectedAt: 1760000003000,
          ),
          HongguoAccountEntry(
            seriesId: '7300000000000000022',
            title: '中间',
            collectedAt: 1760000002000,
          ),
          HongguoAccountEntry(
            seriesId: '7300000000000000021',
            title: '最早',
            collectedAt: 1760000001000,
          ),
        ],
      ),
    );

    // 没有新增条目，但顺序变了，也要写盘。
    expect(merged, 0);
    expect(store.favorites.map((drama) => drama.title).toList(), [
      '最新',
      '中间',
      '最早',
    ]);
  });
}
