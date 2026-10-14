import 'dart:convert';

import 'package:duanju_app/follow_state.dart';
import 'package:duanju_app/lan_sync_models.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/webdav_sync.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

class FakeWebDavTransport implements WebDavTransport {
  FakeWebDavTransport({Map<String, String>? files, this.failWrite = false})
    : files = files ?? <String, String>{};
  final Map<String, String> files;
  final bool failWrite;
  final List<String> reads = [];

  /// 写入时请求的完整地址，用于校验文件确实落在同步目录里。
  final List<String> writes = [];

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
    final name = file.pathSegments.last;
    reads.add(name);
    final content = files[name];
    if (content == null) throw StateError('远端没有此文件');
    return content;
  }

  @override
  Future<void> write(Uri file, String content) async {
    if (failWrite) throw StateError('模拟上传失败');
    writes.add(file.toString());
    files[file.pathSegments.last] = content;
  }
}

Future<LocalStore> createStore() async {
  SharedPreferences.setMockInitialValues({});
  final prefs = await SharedPreferences.getInstance();
  final store = LocalStore(prefs);
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

Future<WebDavSyncOutcome> run(
  LocalStore store,
  WebDavTransport transport,
) => WebDavSynchronizer(store: store, transport: transport).synchronize();

WatchEntry watch(Drama drama, int episode, double position) => WatchEntry(
  drama: drama,
  episode: episode,
  position: position,
  duration: 100,
  updatedAt: DateTime.utc(2026, 10, 14, 12),
);

void main() {
  const dramaA = Drama(id: 'hongguo:1', source: 'hongguo', title: '甲剧');
  const dramaB = Drama(id: 'hongguo:2', source: 'hongguo', title: '乙剧');

  test('webdav settings round trip and validation', () async {
    final store = await createStore();
    expect(store.webDavSettings.enabled, isFalse);
    expect(store.webDavSettings.directory, WebDavSettings.defaultDirectory);
    await configure(store);
    final saved = store.webDavSettings;
    expect(saved.enabled, isTrue);
    expect(saved.url, 'https://dav.example.com');
    expect(
      saved.directoryUri.toString(),
      'https://dav.example.com/zhenguoju-sync/',
    );
    expect(
      WebDavSettings(
        url: 'https://dav.example.com/dav/',
      ).directoryUri.toString(),
      'https://dav.example.com/dav/zhenguoju-sync/',
    );
    expect(saved.directoryUri!.path.endsWith('/'), isTrue);
    expect(WebDavSettings(url: 'ftp://dav.example.com').directoryUri, isNull);
    expect(
      WebDavSettings(url: 'https://dav.example.com', directory: 'a b').directoryUri,
      isNull,
    );
    expect(
      WebDavSettings.fromJson('broken').enabled,
      isFalse,
    );
    expect(
      WebDavSettings.fromJson({'enabled': true}).configured,
      isFalse,
    );
  });

  test('webdav password stays out of exported backup', () async {
    final store = await createStore();
    await configure(store);
    final backup = await store.exportBackup();
    expect(backup.contains('dav.example.com'), isFalse);
    expect(backup.contains('webdav'), isFalse);
  });

  test('unconfigured synchronizer reports missing settings', () async {
    final store = await createStore();
    await expectLater(
      run(store, FakeWebDavTransport()),
      throwsStateError,
    );
  });

  test('two devices merge follows, progress and unfollows', () async {
    final a = await createStore();
    await configure(a);
    final transport = FakeWebDavTransport();
    await run(a, transport);
    await a.toggleFavorite(dramaA);
    await a.saveWatch(watch(dramaA, 2, 30));
    await run(a, transport);

    final b = await createStore();
    await configure(b);
    final outcome = await run(b, transport);
    expect(outcome.added, 1);
    expect(outcome.conflicts, 0);
    expect(b.isFavorite(dramaA.id), isTrue);
    expect(b.watched(dramaA.id)?.episode, 2);

    await b.toggleFavorite(dramaB);
    await run(b, transport);
    await run(a, transport);
    expect(a.isFavorite(dramaB.id), isTrue);

    await a.toggleFavorite(dramaA);
    await run(a, transport);
    await run(b, transport);
    expect(b.isFavorite(dramaA.id), isFalse);
    expect(b.watched(dramaA.id)?.episode, 2);
  });

  test('concurrent edits keep both versions as a conflict and resolve', () async {
    final a = await createStore();
    await configure(a);
    final transport = FakeWebDavTransport();
    await a.toggleFavorite(dramaA);
    await run(a, transport);

    final b = await createStore();
    await configure(b);
    await run(b, transport);
    expect(b.isFavorite(dramaA.id), isTrue);

    await a.setFollowStatus(dramaA, FollowStatus.watched);
    await b.setFollowStatus(dramaA, FollowStatus.watching);
    await run(b, transport);
    final outcome = await run(a, transport);
    expect(outcome.conflicts, 1);
    final record = a.lanDocument.records[dramaA.id]!;
    expect(record.conflicts, 1);
    expect(record.fields['status']!.conflict, isTrue);

    final watched = record.fields['status']!.values.firstWhere(
      (candidate) =>
          (candidate.value as Map<String, dynamic>)['status'] == 'watched',
    );
    await a.resolveLanConflict(dramaA.id, 'status', watched, record.hash);
    expect(a.lanDocument.records[dramaA.id]!.conflicts, 0);

    await run(a, transport);
    await run(b, transport);
    expect(b.lanDocument.records[dramaA.id]!.conflicts, 0);
  });

  test('own and corrupt remote files are skipped', () async {
    final a = await createStore();
    await configure(a);
    final transport = FakeWebDavTransport();
    await a.toggleFavorite(dramaA);
    await run(a, transport);
    final replica = a.lanDocument.replica;
    expect(transport.files.containsKey('$replica.json'), isTrue);

    transport.files['ffffffffffffffffffffffffffffffff.json'] = 'not json';
    final outcome = await run(a, transport);
    expect(outcome.hasChanges, isFalse);
    expect(transport.reads, isNot(contains('$replica.json')));
    expect(transport.reads, contains('ffffffffffffffffffffffffffffffff.json'));
    expect(a.isFavorite(dramaA.id), isTrue);
  });

  test('upload lands inside the sync directory, not its parent', () async {
    final a = await createStore();
    // 坚果云形态：地址本身带 WebDAV 目录前缀，容易少一级路径
    await a.saveWebDavSettings(
      const WebDavSettings(
        enabled: true,
        url: 'https://dav.jianguoyun.com/dav/',
        username: 'user',
        password: 'pass',
      ),
    );
    final transport = FakeWebDavTransport();
    await a.toggleFavorite(dramaA);
    await run(a, transport);
    final replica = a.lanDocument.replica;
    expect(
      transport.writes.single,
      'https://dav.jianguoyun.com/dav/zhenguoju-sync/$replica.json',
    );
  });

  test('network failure keeps local records untouched', () async {
    final a = await createStore();
    await configure(a);
    final broken = _FailingTransport();
    await a.toggleFavorite(dramaA);
    await a.saveWatch(watch(dramaA, 2, 30));
    await expectLater(run(a, broken), throwsStateError);
    expect(a.isFavorite(dramaA.id), isTrue);
    expect(a.watched(dramaA.id)?.episode, 2);
  });

  test('merge is applied locally even when the upload fails', () async {
    final a = await createStore();
    await configure(a);
    final shared = <String, String>{};
    await a.toggleFavorite(dramaA);
    await a.saveWatch(watch(dramaA, 2, 30));
    await run(a, FakeWebDavTransport(files: shared));

    final b = await createStore();
    await configure(b);
    await expectLater(
      run(b, FakeWebDavTransport(files: shared, failWrite: true)),
      throwsStateError,
    );
    expect(b.isFavorite(dramaA.id), isTrue);
    expect(b.watched(dramaA.id)?.episode, 2);
  });

  test('records outside the local user sources are filtered', () async {
    final a = await createStore();
    await a.saveProfile(
      id: 'default',
      name: '管理员',
      sources: [],
      download: true,
      pin: 'abcdef12',
    );
    await configure(a);
    const foreign = Drama(id: 'hanxiaoquan:9', source: 'hanxiaoquan', title: '外站剧');
    await a.toggleFavorite(dramaA);
    await a.toggleFavorite(foreign);
    final transport = FakeWebDavTransport();
    await run(a, transport);

    final b = await createStore();
    await configure(b);
    await b.saveProfile(
      id: 'default',
      name: '管理员',
      sources: [],
      download: true,
      pin: 'abcdef12',
    );
    await b.saveProfile(name: '访客', sources: ['hongguo'], download: false);
    final visitor = b.profiles.firstWhere((profile) => !profile.admin);
    await b.switchProfile(visitor.id);
    await run(b, transport);
    expect(b.isFavorite(dramaA.id), isTrue);
    expect(b.isFavorite(foreign.id), isFalse);
  });
}

class _FailingTransport implements WebDavTransport {
  @override
  Future<void> ensureDirectory(Uri directory) async =>
      throw StateError('模拟网络故障');
  @override
  Future<List<WebDavRemoteFile>> list(Uri directory) async =>
      throw StateError('模拟网络故障');
  @override
  Future<String> read(Uri file) async => throw StateError('模拟网络故障');
  @override
  Future<void> write(Uri file, String content) async =>
      throw StateError('模拟网络故障');
}
