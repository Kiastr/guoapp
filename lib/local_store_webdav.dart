part of 'local_store.dart';

class WebDavSettings {
  const WebDavSettings({
    this.enabled = false,
    this.url = '',
    this.username = '',
    this.password = '',
    this.directory = 'zhenguoju-sync',
    this.autoSync = true,
    this.lastError,
    this.lastSyncAt,
  });

  static const defaultDirectory = 'zhenguoju-sync';

  final bool enabled;
  final String url;
  final String username;
  final String password;
  final String directory;
  final bool autoSync;
  final String? lastError;
  final DateTime? lastSyncAt;

  bool get configured =>
      enabled && url.trim().isNotEmpty && username.isNotEmpty && password.isNotEmpty;

  Uri? get directoryUri {
    final base = Uri.tryParse(url.trim());
    if (base == null ||
        !{'http', 'https'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty) {
      return null;
    }
    final folder = directory.trim().replaceAll(RegExp(r'^/+|/+$'), '');
    if (folder.isEmpty ||
        folder.length > 64 ||
        !RegExp(r'^[a-zA-Z0-9_-]+$').hasMatch(folder)) {
      return null;
    }
    final path = base.path.endsWith('/') ? base.path : '${base.path}/';
    return base.replace(path: '$path$folder');
  }

  factory WebDavSettings.fromJson(Object? value) {
    final row = value is Map ? Map<String, dynamic>.from(value) : const {};
    return WebDavSettings(
      enabled: row['enabled'] == true,
      url: row['url'] is String ? row['url'] as String : '',
      username: row['username'] is String ? row['username'] as String : '',
      password: row['password'] is String ? row['password'] as String : '',
      directory: row['directory'] is String &&
              (row['directory'] as String).trim().isNotEmpty
          ? row['directory'] as String
          : defaultDirectory,
      autoSync: row['autoSync'] != false,
      lastError: row['lastError'] is String ? row['lastError'] as String : null,
      lastSyncAt: DateTime.tryParse(
        row['lastSyncAt'] is String ? row['lastSyncAt'] as String : '',
      ),
    );
  }

  Map<String, dynamic> toJson() => {
    'enabled': enabled,
    'url': url,
    'username': username,
    'password': password,
    'directory': directory,
    'autoSync': autoSync,
    if (lastError != null) 'lastError': lastError,
    if (lastSyncAt != null)
      'lastSyncAt': lastSyncAt!.toUtc().toIso8601String(),
  };

  WebDavSettings copyWith({
    bool? enabled,
    String? url,
    String? username,
    String? password,
    String? directory,
    bool? autoSync,
    String? lastError,
    bool clearLastError = false,
    DateTime? lastSyncAt,
  }) => WebDavSettings(
    enabled: enabled ?? this.enabled,
    url: url ?? this.url,
    username: username ?? this.username,
    password: password ?? this.password,
    directory: directory ?? this.directory,
    autoSync: autoSync ?? this.autoSync,
    lastError: clearLastError ? null : (lastError ?? this.lastError),
    lastSyncAt: lastSyncAt ?? this.lastSyncAt,
  );
}

extension LocalStoreWebDav on LocalStore {
  WebDavSettings get webDavSettings {
    if (locked) return const WebDavSettings();
    try {
      return WebDavSettings.fromJson(jsonDecode(_string('webdavSettings') ?? '{}'));
    } catch (_) {
      return const WebDavSettings();
    }
  }

  Future<void> saveWebDavSettings(WebDavSettings value) =>
      _setting('webdavSettings', jsonEncode(value.toJson()));
}
