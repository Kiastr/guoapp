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
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 8);
    try {
      final request = await client.getUrl(Uri.parse(_releasesLatestApi));
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
      // release tag 形如 app-v1.5.21-219：末尾数字即构建号。
      final buildMatch = RegExp(r'-(\d+)$').firstMatch(tag);
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
      await InAppBrowser().open(urlString: info.url);
    } catch (_) {
      // 对话框已给出可复制的下载地址，打不开浏览器时静默。
    }
  }
}
