import 'models.dart';

/// 一个已导入的 LibreTV（Apple CMS / MacCMS provide/vod）片源。
/// 对应原生核心 libretv.json 里的一条站点记录。
class LibreTVSite {
  const LibreTVSite({
    required this.key,
    required this.name,
    required this.api,
    this.count = 0,
  });

  final String key;
  final String name;
  /// 采集接口地址（provide/vod）。
  final String api;
  /// 本机已缓存的剧集数（站源管理里展示）。
  final int count;

  /// 完整源 ID，与 [SourceSite] 运行时注册表一致。
  String get sourceId => 'libretv:$key';

  SourceSite toSourceSite() =>
      SourceSite(sourceId, name, 'LibreTV 采集 · ${_apiHost(api)}');

  factory LibreTVSite.fromJson(Map<String, dynamic> json) => LibreTVSite(
    key: json['key'] as String? ?? '',
    name: json['name'] as String? ?? '',
    api: json['api'] as String? ?? '',
    count: intValue(json['count']),
  );

  static String _apiHost(String api) {
    final trimmed = api.trim();
    final start = trimmed.indexOf('://');
    if (start < 0) return trimmed;
    final rest = trimmed.substring(start + 3);
    final slash = rest.indexOf('/');
    return slash < 0 ? rest : rest.substring(0, slash);
  }
}

/// 导入结果：成功入库的站点、被跳过的条目与探测统计。
class LibreTVImportResult {
  const LibreTVImportResult({
    required this.added,
    required this.skipped,
    required this.verified,
  });

  final List<LibreTVSite> added;
  final List<String> skipped;
  final int verified;

  bool get isEmpty => added.isEmpty;

  factory LibreTVImportResult.fromJson(Map<String, dynamic> json) =>
      LibreTVImportResult(
        added:
            (json['added'] as List? ?? const [])
                .whereType<Map>()
                .map((row) => LibreTVSite.fromJson(Map<String, dynamic>.from(row)))
                .toList(),
        skipped:
            (json['skipped'] as List? ?? const [])
                .whereType<String>()
                .toList(),
        verified: intValue(json['verified']),
      );
}
