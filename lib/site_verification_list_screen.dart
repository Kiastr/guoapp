import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'site_verification.dart';
import 'site_verification_screen.dart';

class SiteVerificationListScreen extends StatefulWidget {
  const SiteVerificationListScreen({super.key, required this.repository});

  final AppRepository repository;

  @override
  State<SiteVerificationListScreen> createState() =>
      _SiteVerificationListScreenState();
}

class _SiteVerificationListScreenState
    extends State<SiteVerificationListScreen> {
  SiteVerificationView _view = const SiteVerificationView();
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final view = await widget.repository.siteVerifications();
      if (!mounted) return;
      setState(() {
        _view = view;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = '读取站点验证状态失败：$error');
    }
  }

  Future<void> _openVerification(SiteVerificationTarget target) async {
    final saved = await Navigator.of(context).push<bool>(
      MaterialPageRoute<bool>(
        builder: (_) => SiteVerificationScreen(
          repository: widget.repository,
          target: target,
        ),
      ),
    );
    if (saved == true) await _load();
  }

  Future<void> _clearVerification(SiteVerificationTarget target) async {
    setState(() => _busy = true);
    try {
      final view = await widget.repository.clearSiteVerification(target.source);
      if (!mounted) return;
      setState(() {
        _view = view;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = '清除失败：$error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  static String _verifiedAt(int seconds) {
    if (seconds <= 0) return '';
    final time = DateTime.fromMillisecondsSinceEpoch(seconds * 1000);
    String two(int value) => value.toString().padLeft(2, '0');
    return '${time.year}-${two(time.month)}-${two(time.day)} '
        '${two(time.hour)}:${two(time.minute)}';
  }

  static String _agentLabel(String agent) {
    final version =
        RegExp(r'Chrome/(\d+)\.').firstMatch(agent)?.group(1) ?? '?';
    final platform = agent.contains('Android')
        ? 'Android'
        : agent.contains('iPhone') || agent.contains('iPad')
        ? 'iOS'
        : agent.contains('Windows')
        ? 'Windows'
        : agent.contains('Macintosh')
        ? 'macOS'
        : agent.contains('Linux')
        ? 'Linux'
        : '未知平台';
    return '$platform Chrome $version';
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('站点验证')),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 680),
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text(
              '部分站源用 Cloudflare 挑战页拦截脚本请求。用内置浏览器验证一次后，'
              '凭据只保存在本机（不进配置备份）；换代理节点或换域名后需要重新验证。'
              '如果站点直接返回 1020 封禁页，验证无效，只能换节点。',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  _error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            if (_view.targets.isEmpty) ...[
              const SizedBox(height: 24),
              const Text('当前构建没有需要验证的站源。'),
            ] else
              for (final target in _view.targets)
                Builder(
                  builder: (context) {
                    final entry = _view.entryFor(target.source);
                    return Padding(
                      padding: const EdgeInsets.symmetric(vertical: 8),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  target.name,
                                  style: Theme.of(context).textTheme.titleSmall,
                                ),
                                const SizedBox(height: 2),
                                Text(
                                  entry == null
                                      ? '未验证'
                                      : entry.stale
                                      ? '验证已失效 · 请重新验证'
                                      : '已验证 · ${_verifiedAt(entry.updatedAt)} · ${entry.host}',
                                  style: entry?.stale == true
                                      ? TextStyle(
                                          color: Theme.of(
                                            context,
                                          ).colorScheme.error,
                                        )
                                      : null,
                                ),
                                if (entry != null && entry.userAgent.isNotEmpty)
                                  Text(
                                    _agentLabel(entry.userAgent),
                                    style: Theme.of(context).textTheme.bodySmall,
                                  ),
                                if (target.note.isNotEmpty)
                                  Text(
                                    target.note,
                                    style: Theme.of(context).textTheme.bodySmall,
                                  ),
                              ],
                            ),
                          ),
                          const SizedBox(width: 12),
                          Column(
                            crossAxisAlignment: CrossAxisAlignment.end,
                            children: [
                              FilledButton.tonal(
                                onPressed: _busy
                                    ? null
                                    : () => _openVerification(target),
                                child: Text(entry == null ? '验证' : '重新验证'),
                              ),
                              if (entry != null)
                                TextButton(
                                  onPressed: _busy
                                      ? null
                                      : () => _clearVerification(target),
                                  child: const Text('清除'),
                                ),
                            ],
                          ),
                        ],
                      ),
                    );
                  },
                ),
            const SizedBox(height: 16),
            TextButton.icon(
              onPressed: _busy ? null : _load,
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('刷新状态'),
            ),
          ],
        ),
      ),
    ),
  );
}
