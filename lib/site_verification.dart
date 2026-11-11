/// [站点验证] 通用骨架的数据模型。
///
/// Cloudflare 挑战只能由真实浏览器通过，通过后站点下发 `cf_clearance` 等
/// cookie。这里描述「哪个源需要验证」「当前存了哪份凭据」，具体存储与
/// 请求注入在 Go 侧（app_site_verification.go），页面在
/// site_verification_screen.dart。
///
/// 三个字段必须与 Go 侧 JSON 逐字对应：source / host / userAgent / cookie /
/// updatedAt，以及 target 的 source / name / url / userAgent / note。
class SiteVerificationTarget {
  const SiteVerificationTarget({
    required this.source,
    required this.name,
    required this.url,
    required this.userAgent,
    this.note = '',
  });

  final String source;
  final String name;

  /// 验证入口地址（站点首页），不是具体 API。
  final String url;

  /// 该源请求实际使用的 UA。验证页必须用同一串，否则 cf_clearance 失效。
  final String userAgent;
  final String note;

  factory SiteVerificationTarget.fromJson(Map<String, dynamic> json) =>
      SiteVerificationTarget(
        source: json['source'] as String? ?? '',
        name: json['name'] as String? ?? '',
        url: json['url'] as String? ?? '',
        userAgent: json['userAgent'] as String? ?? '',
        note: json['note'] as String? ?? '',
      );
}

class SiteVerification {
  const SiteVerification({
    required this.source,
    required this.host,
    required this.userAgent,
    required this.cookie,
    required this.updatedAt,
  });

  final String source;
  final String host;
  final String userAgent;
  final String cookie;

  /// Unix 秒。只用于显示「验证于 …」。
  final int updatedAt;

  factory SiteVerification.fromJson(Map<String, dynamic> json) =>
      SiteVerification(
        source: json['source'] as String? ?? '',
        host: json['host'] as String? ?? '',
        userAgent: json['userAgent'] as String? ?? '',
        cookie: json['cookie'] as String? ?? '',
        updatedAt: (json['updatedAt'] as num?)?.toInt() ?? 0,
      );

  Map<String, dynamic> toJson() => {
    'source': source,
    'host': host,
    'userAgent': userAgent,
    'cookie': cookie,
  };
}

class SiteVerificationView {
  const SiteVerificationView({
    this.targets = const [],
    this.entries = const [],
  });

  final List<SiteVerificationTarget> targets;
  final List<SiteVerification> entries;

  factory SiteVerificationView.fromJson(Map<String, dynamic> json) =>
      SiteVerificationView(
        targets: [
          for (final item in (json['targets'] as List? ?? const []))
            if (item is Map)
              SiteVerificationTarget.fromJson(Map<String, dynamic>.from(item)),
        ],
        entries: [
          for (final item in (json['entries'] as List? ?? const []))
            if (item is Map)
              SiteVerification.fromJson(Map<String, dynamic>.from(item)),
        ],
      );

  SiteVerification? entryFor(String source) {
    for (final entry in entries) {
      if (entry.source == source) return entry;
    }
    return null;
  }
}
