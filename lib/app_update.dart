import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'app_layout.dart';

const _repoFullName = 'Kiastr/guoapp';
const _releasesLatestApi =
    'https://api.github.com/repos/$_repoFullName/releases/latest';
const _releasesPageUrl = 'https://github.com/$_repoFullName/releases/latest';

/// [+221] 更新检查的 API 候选源：直连优先，`gh-proxy.org` 兜底。
///
/// 【为什么只有一个镜像】实测（2026-11-22）绝大多数「国内 GitHub 镜像」是文件下载
/// 加速器（只代理 release/raw/clone 的 github.com 域名），**不代理 api.github.com
/// 这个 REST 端点**：gh-proxy.com / ghproxy.net / ghproxy.homeboyc.cn 返回 403，
/// ghfast.top / mirror.ghproxy.com / moeyy.xyz / gitmirror 等直接超时。唯一稳定
/// 可用的是 gh-proxy.org（3 连测均 200、约 1s）。硬编码一堆无效镜像只会制造
/// 「做了多源」的假象、还拖慢每次检查，所以只保留实测可用的这一个。
///
/// 【为什么不自建 CDN】更新是低频只读查询、直连国内实测可达，为一个辅助功能引入
/// 自建服务不划算；候选源逐个尝试、首个成功即止，镜像挂了也能自动直连，反过来亦然。
const _releaseApiCandidates = <String>[
  _releasesLatestApi,
  'https://gh-proxy.org/$_releasesLatestApi',
];

class AppUpdateInfo {
  const AppUpdateInfo({
    required this.version,
    required this.build,
    required this.tag,
    required this.notes,
    required this.url,
  });

  final String version;
  final int build;
  final String tag;
  final String notes;
  final String url;

  String get displayVersion => '$version+$build';

  bool isNewerThan(int localBuild) => build > localBuild;
}

class AppUpdateService {
  AppUpdateService(this._preferences);

  final SharedPreferences _preferences;

  static const lastCheckKey = 'appUpdate.lastCheckMs';
  static const ignoredBuildKey = 'appUpdate.ignoredBuild';
  static const autoCheckInterval = Duration(hours: 24);

  bool shouldAutoCheck() {
    final last = _preferences.getInt(lastCheckKey) ?? 0;
    if (last <= 0) return true;
    return DateTime.now().millisecondsSinceEpoch - last >=
        autoCheckInterval.inMilliseconds;
  }

  bool isIgnored(AppUpdateInfo info) =>
      _preferences.getInt(ignoredBuildKey) == info.build;

  Future<void> markChecked() async {
    await _preferences.setInt(
      lastCheckKey,
      DateTime.now().millisecondsSinceEpoch,
    );
  }

  Future<void> ignore(AppUpdateInfo info) async {
    await _preferences.setInt(ignoredBuildKey, info.build);
  }

  Future<void> clearIgnored() async {
    await _preferences.remove(ignoredBuildKey);
  }

  Future<AppUpdateInfo?> checkLatest() async {
    // [+221] 逐个候选源尝试：直连失败/被污染就换镜像，首个合法 releases JSON 即止。
    for (final endpoint in _releaseApiCandidates) {
      final info = await _checkOne(endpoint);
      if (info != null) return info;
    }
    return null;
  }

  /// 从单个候选源取一次 releases/latest。返回 null 表示「这个源没拿到合法结果」，
  /// 由 [checkLatest] 决定是否尝试下一个源。任何异常都吞掉——更新是辅助功能，
  /// 不该因为网络抖动把用户打扰到。
  Future<AppUpdateInfo?> _checkOne(String endpoint) async {
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 8);
    try {
      final request = await client.getUrl(Uri.parse(endpoint));
      request.headers
        ..set('Accept', 'application/vnd.github+json')
        ..set('User-Agent', appSlug);
      final response = await request
          .close()
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) {
        await response.drain<void>();
        return null;
      }
      final text = await utf8.decoder.bind(response).join();
      final data = jsonDecode(text) as Map<String, dynamic>;
      final tag = data['tag_name'] as String? ?? '';
      // [+221] 严格校验：镜像/反代常返回 HTML 错误页或 403 JSON，即使 HTTP 200
      // 也可能不是 releases 对象。tag_name 必須匹配本仓库的 app-vX.Y.Z-<build>
      // 形态，否则判为被污染、换下一个源——绝不拿错误页去比版本号。
      final buildMatch = RegExp(r'^app-v\d+\.\d+\.\d+-(\d+)$').firstMatch(tag);
      if (buildMatch == null) return null;
      final build = int.tryParse(buildMatch.group(1) ?? '') ?? 0;
      if (build <= 0) return null;
      final versionMatch = RegExp(r'\d+\.\d+\.\d+').firstMatch(tag);
      final info = AppUpdateInfo(
        version: versionMatch?.group(0) ?? '',
        build: build,
        tag: tag,
        notes: (data['body'] as String? ?? '').trim(),
        url: data['html_url'] as String? ?? _releasesPageUrl,
      );
      await markChecked();
      return info;
    } catch (_) {
      return null;
    } finally {
      client.close();
    }
  }
}

Future<void> showAppUpdateDialog(
  BuildContext context,
  AppUpdateInfo info, {
  AppUpdateService? service,
  bool manual = false,
}) async {
  final choice = await showDialog<String>(
    context: context,
    builder: (context) {
      final body = info.notes.isEmpty ? '本次更新未填写说明，可到下载页查看。' : info.notes;
      final summary = body.length > 600 ? '${body.substring(0, 600)}…' : body;
      return AlertDialog(
        title: Text('发现新版本 ${info.displayVersion}'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('当前版本 $appVersion+$duanjuBuildNumber'),
              const SizedBox(height: 12),
              Text(summary),
              const SizedBox(height: 12),
              const SelectableText(_releasesPageUrl),
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, 'later'),
            child: const Text('稍后'),
          ),
          if (service != null && !manual)
            TextButton(
              onPressed: () => Navigator.pop(context, 'ignore'),
              child: const Text('忽略此版本'),
            ),
          FilledButton(
            onPressed: () => Navigator.pop(context, 'download'),
            child: const Text('去下载'),
          ),
        ],
      );
    },
  );
  if (choice == 'ignore' && service != null) {
    await service.ignore(info);
  } else if (choice == 'download') {
    try {
      // [+223] 用系统浏览器打开发布页。flutter_inappwebview 6.1.5 的 InAppBrowser
      // 没有 5.x 的 open(urlString:) 方法（+221 误用了旧 API，CI 编译报
      // "The method 'open' isn't defined"）；6.x 对应的是静态方法
      // openWithSystemBrowser(url: WebUri(...))，无需应用内浏览器配置，最稳。
      await InAppBrowser.openWithSystemBrowser(url: WebUri(info.url));
    } catch (_) {
      // 对话框已给出可复制的下载地址，打不开浏览器时静默。
    }
  }
}
