import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'lan_sync_models.dart';
import 'local_store.dart';

class WebDavRemoteFile {
  const WebDavRemoteFile({required this.name, required this.uri, this.bytes = -1});
  final String name;
  final Uri uri;
  final int bytes;
}

abstract class WebDavTransport {
  Future<void> ensureDirectory(Uri directory);
  Future<List<WebDavRemoteFile>> list(Uri directory);
  Future<String> read(Uri file);
  Future<void> write(Uri file, String content);
}

class HttpWebDavTransport implements WebDavTransport {
  HttpWebDavTransport({required this.username, required this.password});

  static const fileLimit = 8 * 1024 * 1024;

  final String username;
  final String password;
  HttpClient? _client;

  HttpClient get _http =>
      _client ??= (HttpClient()
        ..connectionTimeout = const Duration(seconds: 15)
        ..idleTimeout = const Duration(seconds: 60));

  void close() {
    _client?.close(force: true);
    _client = null;
  }

  Future<HttpClientResponse> _send(
    String method,
    Uri url, {
    String? body,
    Map<String, String>? headers,
  }) async {
    final request = await _http.openUrl(method, url);
    request.headers.set(
      'Authorization',
      'Basic ${base64Encode(utf8.encode('$username:$password'))}',
    );
    if (body != null) {
      final bytes = utf8.encode(body);
      request.headers.set('Content-Type', 'application/json; charset=utf-8');
      request.headers.contentLength = bytes.length;
    }
    for (final entry in headers?.entries ?? const <MapEntry<String, String>>[]) {
      request.headers.set(entry.key, entry.value);
    }
    if (body != null) request.add(utf8.encode(body));
    return request.close();
  }

  StateError _failure(int code, String action) => code == 401 || code == 403
      ? StateError('$action：账号或密码不正确')
      : StateError('$action（HTTP $code）');

  Future<String> _readBody(
    HttpClientResponse response,
    String action,
  ) async {
    final builder = BytesBuilder(copy: false);
    await for (final chunk in response) {
      builder.add(chunk);
      if (builder.length > fileLimit) {
        await response.drain<void>();
        throw StateError('$action：响应超过 8 MiB 上限');
      }
    }
    final text = utf8.decode(builder.takeBytes());
    return text.startsWith('\uFEFF') ? text.substring(1) : text;
  }

  @override
  Future<void> ensureDirectory(Uri directory) async {
    final response = await _send('MKCOL', directory);
    await response.drain<void>();
    if ({201, 200, 405}.contains(response.statusCode)) return;
    throw _failure(response.statusCode, '创建同步目录失败');
  }

  static const _propFindBody =
      '<?xml version="1.0" encoding="utf-8"?>'
      '<D:propfind xmlns:D="DAV:"><D:prop><D:getcontentlength/></D:prop></D:propfind>';

  @override
  Future<List<WebDavRemoteFile>> list(Uri directory) async {
    final response = await _send(
      'PROPFIND',
      directory,
      body: _propFindBody,
      headers: const {'Depth': '1'},
    );
    final text = await _readBody(response, '读取同步目录');
    if (!{207, 200}.contains(response.statusCode)) {
      throw _failure(response.statusCode, '读取同步目录失败');
    }
    final files = <WebDavRemoteFile>[];
    for (final block in RegExp(
      r'<(?:[\w-]+:)?response\b[^>]*>([\s\S]*?)</(?:[\w-]+:)?response>',
    ).allMatches(text)) {
      final body = block.group(1)!;
      final href = RegExp(
        r'<(?:[\w-]+:)?href>([^<]+)<',
      ).firstMatch(body)?.group(1);
      if (href == null) continue;
      final uri = directory.resolve(href);
      final name = uri.pathSegments.isEmpty ? '' : uri.pathSegments.last;
      if (!name.endsWith('.json')) continue;
      final size = int.tryParse(
        RegExp(
          r'<(?:[\w-]+:)?getcontentlength>(\d+)<',
        ).firstMatch(body)?.group(1) ?? '',
      );
      files.add(WebDavRemoteFile(name: name, uri: uri, bytes: size ?? -1));
    }
    return files;
  }

  @override
  Future<String> read(Uri file) async {
    final response = await _send('GET', file);
    if (response.statusCode != 200) {
      await response.drain<void>();
      throw _failure(response.statusCode, '下载同步文件失败');
    }
    if (response.contentLength > fileLimit) {
      await response.drain<void>();
      throw StateError('远端同步文件超过 8 MiB 上限');
    }
    return _readBody(response, '下载同步文件');
  }

  @override
  Future<void> write(Uri file, String content) async {
    final response = await _send('PUT', file, body: content);
    await response.drain<void>();
    if (!{200, 201, 204}.contains(response.statusCode)) {
      throw _failure(response.statusCode, '上传同步文件失败');
    }
  }
}

class WebDavSyncOutcome {
  const WebDavSyncOutcome({
    this.added = 0,
    this.updated = 0,
    this.removed = 0,
    this.conflicts = 0,
    this.savedAt,
  });
  final int added;
  final int updated;
  final int removed;
  final int conflicts;
  final DateTime? savedAt;
  String get label => '新增 $added · 更新 $updated · 移除 $removed · 冲突 $conflicts';
  bool get hasChanges => added + updated + removed > 0;
}

class WebDavSynchronizer {
  WebDavSynchronizer({
    required this.store,
    required this.transport,
    DateTime Function()? clock,
  }) : _clock = clock ?? DateTime.now;

  static const schema = 1;

  final LocalStore store;
  final WebDavTransport transport;
  final DateTime Function() _clock;

  Future<WebDavSyncOutcome> synchronize() async {
    final settings = store.webDavSettings;
    if (!settings.configured) {
      throw StateError('请先填写 WebDAV 服务器地址、账号和密码');
    }
    final directory = settings.directoryUri;
    if (directory == null) {
      throw StateError('同步地址无效：需要 http/https 地址，目录名仅限字母、数字、- 和 _');
    }
    if (store.locked) throw StateError('当前用户已锁定，请先解锁');
    final epoch = store.profileEpoch;
    final sources = store.sources.map((source) => source.id).toSet();
    if (sources.isEmpty) throw StateError('当前用户没有可同步的站源');
    final document = store.lanDocument;
    final localBase = document.hashFor(sources);
    final localRecords = <String, LanRecord>{
      for (final entry in document.records.entries)
        if (sources.contains(entry.value.drama.source)) entry.key: entry.value,
    };

    await transport.ensureDirectory(directory);
    final remotes = await transport.list(directory);

    final remoteRecords = <String, LanRecord>{};
    for (final remote in remotes) {
      if (remote.name == '${document.replica}.json') continue;
      if (remote.bytes > HttpWebDavTransport.fileLimit) continue;
      final text = await transport.read(remote.uri);
      if (utf8.encode(text).length > HttpWebDavTransport.fileLimit) continue;
      LanDocument incoming;
      try {
        final payload = Map<String, dynamic>.from(jsonDecode(text) as Map);
        if (payload['schema'] != schema || payload['document'] is! Map) {
          throw const FormatException('同步文件格式无效');
        }
        incoming = LanDocument.fromJson(payload['document']);
      } on FormatException {
        continue;
      }
      for (final entry in incoming.records.entries) {
        if (!sources.contains(entry.value.drama.source)) continue;
        final existing = remoteRecords[entry.key];
        remoteRecords[entry.key] =
            existing == null ? entry.value : existing.merge(entry.value);
      }
    }

    final result = <String, LanRecord>{};
    for (final id in {...localRecords.keys, ...remoteRecords.keys}) {
      final local = localRecords[id];
      final remote = remoteRecords[id];
      result[id] = local == null
          ? remote!
          : remote == null
          ? local
          : local.merge(remote);
    }
    if (result.length > LanDocument.limit) {
      throw StateError('合并后的追剧记录超过上限，请先整理记录');
    }

    await store.applyLanRecords(
      operation: lanID(),
      base: localBase,
      sources: sources,
      records: result.values,
      epoch: epoch,
      isCurrent: () => store.profileEpoch == epoch && !store.locked,
    );

    final savedAt = _clock.call().toUtc();
    try {
      final merged = store.lanDocument;
      final payload = jsonEncode({
        'schema': schema,
        'savedAt': savedAt.toIso8601String(),
        'document': merged.toJson(),
      });
      if (utf8.encode(payload).length > HttpWebDavTransport.fileLimit) {
        throw StateError('同步文件超过 8 MiB 上限，请先清理追剧记录');
      }
      await transport.write(directory.resolve('${merged.replica}.json'), payload);
    } catch (failure) {
      _record(savedAt: savedAt, error: '已合并保存到本机，上传失败：$failure');
      rethrow;
    }
    _record(savedAt: savedAt);
    final count = LanChangeCount.between(localRecords, result);
    return WebDavSyncOutcome(
      added: count.added,
      updated: count.updated,
      removed: count.removed,
      conflicts: count.conflicts,
      savedAt: savedAt,
    );
  }

  void _record({required DateTime savedAt, String? error}) {
    final next = store.webDavSettings.copyWith(
      lastSyncAt: savedAt,
      lastError: error,
      clearLastError: error == null,
    );
    unawaited(store.saveWebDavSettings(next).catchError((Object _) {}));
  }
}
