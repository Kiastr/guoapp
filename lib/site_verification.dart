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
    this.followBrowserUa = false,
    this.desktopUserAgent = '',
  });

  final String source;
  final String name;

  /// 验证入口地址（站点首页），不是具体 API。
  final String url;

  /// 该源请求实际使用的 UA。验证页必须用同一串，否则 cf_clearance 失效。
  ///
  /// [followBrowserUa] 为 true 时这个值只是出厂兜底，真正用的是验证页从
  /// WebView 读到的真实 UA。
  final String userAgent;
  final String note;

  /// 验证页改用本机浏览器的真实 UA。
  ///
  /// Android WebView 的 JS 环境永远透着移动端特征，把 UA 头伪造成 Windows
  /// 桌面 Chrome 会让 Turnstile 看出矛盾、无限挑战。跟随真实 UA 才能让
  /// 环境自洽；Go 侧会跟着换成同一个 UA。
  final bool followBrowserUa;

  /// [followBrowserUa] 为 true 时给 Windows 用的桌面 UA（Windows 没有
  /// 内置浏览器，走粘贴 Cookie，只能沿用桌面身份）。
  final String desktopUserAgent;

  factory SiteVerificationTarget.fromJson(Map<String, dynamic> json) =>
      SiteVerificationTarget(
        source: json['source'] as String? ?? '',
        name: json['name'] as String? ?? '',
        url: json['url'] as String? ?? '',
        userAgent: json['userAgent'] as String? ?? '',
        note: json['note'] as String? ?? '',
        followBrowserUa: json['followBrowserUa'] as bool? ?? false,
        desktopUserAgent: json['desktopUserAgent'] as String? ?? '',
      );
}

class SiteVerification {
  const SiteVerification({
    required this.source,
    required this.host,
    required this.userAgent,
    required this.cookie,
    required this.updatedAt,
    this.stale = false,
  });

  final String source;
  final String host;
  final String userAgent;
  final String cookie;

  /// Unix 秒。只用于显示「验证于 …」。
  final int updatedAt;

  /// 凭据里的 UA 与当前构建的 UA 对不上（版本升级换过 UA），此时
  /// cf_clearance 必然失效，UI 要提示重新验证。由 Go 侧运行时计算。
  final bool stale;

  factory SiteVerification.fromJson(Map<String, dynamic> json) =>
      SiteVerification(
        source: json['source'] as String? ?? '',
        host: json['host'] as String? ?? '',
        userAgent: json['userAgent'] as String? ?? '',
        cookie: json['cookie'] as String? ?? '',
        updatedAt: (json['updatedAt'] as num?)?.toInt() ?? 0,
        stale: json['stale'] as bool? ?? false,
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

/// Go 侧「存完凭据立刻用脚本通道试一次」的结果。
///
/// 验证页没通过只说明浏览器里没成，通过也不代表脚本能用——出口 IP 不一
/// 致、UA 没跟上、cookie 没覆盖到路径，都会让验证白做。这个结果就是把
/// 「白做」当场告诉用户，而不是让他十分钟后再来问一句为什么还是 403。
class SiteVerificationCheck {
  const SiteVerificationCheck({
    required this.ok,
    this.status = 0,
    this.reason = '',
  });

  final bool ok;
  final int status;
  final String reason;

  factory SiteVerificationCheck.fromJson(Map<String, dynamic> json) =>
      SiteVerificationCheck(
        ok: json['ok'] as bool? ?? false,
        status: (json['status'] as num?)?.toInt() ?? 0,
        reason: json['reason'] as String? ?? '',
      );
}
