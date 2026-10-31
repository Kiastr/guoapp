import 'models.dart';

/// 一个已导入的 Kazumi 规则源（+155）。
///
/// Kazumi 是开源动漫 App，它的站源由一份 JSON 规则描述（怎么搜、怎么取
/// 分集），官方规则库在 https://github.com/Predidit/KazumiRules 。
/// 本项目内置一个 Go 解释器跑「api 模式」的规则——不需要 JS 引擎，也不
/// 需要 XPath 求值器。对应原生核心 kazumi.json 的一条记录，
/// 规则正文快照在 kazumi/<key>.json。
class KazumiRuleSite {
  const KazumiRuleSite({
    required this.key,
    required this.name,
    this.version = '',
    this.host = '',
    this.sourceUrl = '',
    this.webview = false,
    this.count = 0,
  });

  final String key;
  final String name;

  /// 规则版本（官方库里的 version 字段），升级同 key 规则时用得上。
  final String version;

  /// 数据来源域名（取自规则的搜索接口），展示用。
  final String host;

  /// 导入来源（粘贴规则原文时为空）。
  final String sourceUrl;

  /// 规则是否依赖浏览器：为 true 时分集给的是播放页 URL，需要 WebView
  /// 嗅探才能拿到真实视频地址（下载链路拿不到）。
  final bool webview;

  /// 本机已缓存的剧集数（站源管理里展示）。
  final int count;

  /// 完整源 ID，与 [SourceSite] 运行时注册表一致。
  String get sourceId => 'kazumi:$key';

  /// 副标题：域名优先，其次规则来源地址，最后标出「规则」。
  String get detailLabel {
    if (host.isNotEmpty) return host;
    if (sourceUrl.isNotEmpty) return sourceUrl;
    return 'Kazumi 规则';
  }

  String get versionLabel => version.isEmpty ? '规则' : '规则 v$version';

  /// App 内引导文案：需要 WebView 的规则源要提前说清限制。
  String get hint =>
      webview ? '该规则的分集是播放页，需内置浏览器嗅探真实地址（不支持下载）' : '该规则的分集是直链，可直接播放与下载';

  SourceSite toSourceSite() =>
      SourceSite(sourceId, name, 'Kazumi $versionLabel · $detailLabel');

  factory KazumiRuleSite.fromJson(Map<String, dynamic> json) => KazumiRuleSite(
    key: json['key'] as String? ?? '',
    name: json['name'] as String? ?? '',
    version: json['version'] as String? ?? '',
    host: json['host'] as String? ?? '',
    sourceUrl: json['sourceUrl'] as String? ?? '',
    webview: json['webview'] == true,
    count: intValue(json['count']),
  );
}

/// 官方规则库索引里的一条（用于「从 Kazumi 官方库导入」的列表）。
class KazumiOfficialRule {
  const KazumiOfficialRule({
    required this.name,
    required this.url,
    this.version = '',
    this.deprecated = false,
  });

  final String name;
  final String url;
  final String version;

  /// 官方是否标记废弃。规则库 86 条里大部分已废弃或依赖 XPath，
  /// 导入失败（不支持的模式）时要有心理预期。
  final bool deprecated;

  factory KazumiOfficialRule.fromJson(Map<String, dynamic> json) =>
      KazumiOfficialRule(
        name: json['name'] as String? ?? '',
        url: json['url'] as String? ?? '',
        version: json['version'] as String? ?? '',
        deprecated: json['deprecated'] == true,
      );
}

/// Kazumi 规则导入结果。
class KazumiImportResult {
  const KazumiImportResult({
    required this.key,
    required this.name,
    this.version = '',
    this.host = '',
    this.sourceUrl = '',
    this.webview = false,
    this.added = true,
  });

  final String key;
  final String name;
  final String version;
  final String host;
  final String sourceUrl;
  final bool webview;

  /// 是否新增（false 表示覆盖导入了同 key 的旧规则）。
  final bool added;

  KazumiRuleSite toSite() => KazumiRuleSite(
    key: key,
    name: name,
    version: version,
    host: host,
    sourceUrl: sourceUrl,
    webview: webview,
  );

  factory KazumiImportResult.fromJson(Map<String, dynamic> json) =>
      KazumiImportResult(
        key: json['key'] as String? ?? '',
        name: json['name'] as String? ?? '',
        version: json['version'] as String? ?? '',
        host: json['host'] as String? ?? '',
        sourceUrl: json['sourceUrl'] as String? ?? '',
        webview: json['webview'] == true,
        added: json['added'] == true,
      );
}
