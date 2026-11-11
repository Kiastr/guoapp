import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'download_preferences.dart';
import 'local_store.dart';
import 'resource_settings.dart';
import 'site_verification.dart';
import 'site_verification_screen.dart';
import 'widgets.dart';

class DownloadPreferencesScreen extends StatelessWidget {
  const DownloadPreferencesScreen({super.key, required this.store});
  final LocalStore store;
  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: store,
    builder: (context, _) {
      final preferences = store.downloadPreferences;
      return Scaffold(
        appBar: AppBar(title: const Text('下载偏好')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 680),
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                ListTile(
                  title: const Text('默认画质'),
                  subtitle: const Text('指定画质不可用时，使用源站提供的可用版本。'),
                ),
                DropdownButtonFormField<int>(
                  initialValue: preferences.quality,
                  decoration: const InputDecoration(labelText: '下载画质'),
                  items: [
                    for (final value in DownloadPreferences.qualities)
                      DropdownMenuItem(
                        value: value,
                        child: Text(value == 0 ? '自动 · 优先高清' : '${value}P'),
                      ),
                  ],
                  onChanged: !store.canDownload
                      ? null
                      : (value) {
                          if (value != null) {
                            saveUserChange(
                              context,
                              () => store.setDownloadPreferences(
                                preferences.copyWith(quality: value),
                              ),
                            );
                          }
                        },
                ),
                const SizedBox(height: 12),
                SwitchListTile(
                  value: preferences.includeVip,
                  title: const Text('默认包含 VIP 集'),
                  subtitle: const Text('VIP 集可能仅提供试看内容；下载前仍可调整选择。'),
                  onChanged: !store.canDownload
                      ? null
                      : (value) => saveUserChange(
                          context,
                          () => store.setDownloadPreferences(
                            preferences.copyWith(includeVip: value),
                          ),
                        ),
                ),
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Text('偏好保存在当前用户中，应用于下载选集、批量下载和更新本剧。'),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
}

class ResourceSettingsScreen extends StatefulWidget {
  const ResourceSettingsScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<ResourceSettingsScreen> createState() => _ResourceSettingsScreenState();
}

class _ResourceSettingsScreenState extends State<ResourceSettingsScreen> {
  final _proxy = TextEditingController();
  late final int _epoch = widget.store.profileEpoch;
  ResourceSettings? _settings;
  SiteVerificationView _verifications = const SiteVerificationView();
  String _mode = 'auto';
  int _catalog = 3, _interval = 250, _downloads = 2;
  bool _folders = false, _busy = false, _showProxy = false;
  String? _error;
  bool get _allowed =>
      widget.store.profileEpoch == _epoch &&
      !widget.store.locked &&
      widget.store.profile.admin;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _proxy.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final settings = await widget.repository.resourceSettings();
      // 站点验证状态与资源设置一起加载：两者都在本机、都属于管理员配置，
      // 失败时不影响另一项（接口默认实现会返回空列表）。
      final verifications = await widget.repository.siteVerifications();
      if (!mounted || !_allowed) return;
      setState(() {
        _settings = settings;
        _verifications = verifications;
        _mode = settings.proxyMode;
        _proxy.text = settings.proxyUrl;
        _catalog = settings.catalogConcurrency;
        _interval = settings.catalogIntervalMs;
        _downloads = settings.downloadConcurrency;
        _folders = settings.downloadBySource;
        _error = null;
      });
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _save() async {
    if (_busy || !_allowed) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final settings = await widget.repository.saveResourceSettings(
        ResourceSettings(
          proxyMode: _mode,
          proxyUrl: _proxy.text.trim(),
          catalogConcurrency: _catalog,
          catalogIntervalMs: _interval,
          downloadConcurrency: _downloads,
          downloadBySource: _folders,
        ),
      );
      if (!mounted || !_allowed) return;
      setState(() => _settings = settings);
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('设置已保存，将用于后续请求和下载任务')));
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _openVerification(SiteVerificationTarget target) async {
    final saved = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (context) => SiteVerificationScreen(
          repository: widget.repository,
          target: target,
        ),
      ),
    );
    if (saved == true) await _load();
  }

  Future<void> _clearVerification(SiteVerificationTarget target) async {
    try {
      final view = await widget.repository.clearSiteVerification(target.source);
      if (!mounted) return;
      setState(() => _verifications = view);
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('已清除 ${target.name} 的验证凭据')));
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  static String _verifiedAt(int seconds) {
    if (seconds <= 0) return '';
    final time = DateTime.fromMillisecondsSinceEpoch(seconds * 1000);
    String two(int value) => value.toString().padLeft(2, '0');
    return '${time.year}-${two(time.month)}-${two(time.day)} '
        '${two(time.hour)}:${two(time.minute)}';
  }

  Widget _verificationSection(BuildContext context) {
    final targets = _verifications.targets;
    if (targets.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Divider(height: 32),
        Text('站点验证', style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 6),
        Text(
          '部分站源用 Cloudflare 挑战页拦截脚本请求。用内置浏览器验证一次后，'
          '凭据只保存在本机，不进配置备份；换代理节点或换域名后需要重新验证。'
          '如果站点直接返回 1020 封禁页，验证无效，只能换节点。',
          style: Theme.of(context).textTheme.bodySmall?.copyWith(
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
        for (final target in targets)
          Builder(
            builder: (context) {
              final entry = _verifications.entryFor(target.source);
              // 不用 ListTile：它的 trailing 宽度有限，两个按钮在窄屏会溢出。
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
                                ? '验证已失效（UA 变更）· 请重新验证'
                                : '已验证 · ${_verifiedAt(entry.updatedAt)} · ${entry.host}',
                            style: entry?.stale == true
                                ? TextStyle(
                                    color: Theme.of(context).colorScheme.error,
                                  )
                                : null,
                          ),
                          if (target.note.isNotEmpty)
                            Text(
                              target.note,
                              style: Theme.of(context).textTheme.bodySmall
                                  ?.copyWith(
                                    color: Theme.of(
                                      context,
                                    ).colorScheme.onSurfaceVariant,
                                  ),
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
      ],
    );
  }

  Widget _count(String title, int value, ValueChanged<int> change) =>
      DropdownButtonFormField<int>(
        initialValue: value,
        decoration: InputDecoration(labelText: title),
        items: [
          for (var i = 1; i <= 6; i++)
            DropdownMenuItem(value: i, child: Text('$i')),
        ],
        onChanged: _busy
            ? null
            : (value) {
                if (value != null) setState(() => change(value));
              },
      );

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('网络与资源'),
      actions: [
        TextButton(
          onPressed: _busy || _settings == null || !_allowed ? null : _save,
          child: const Text('保存'),
        ),
      ],
    ),
    body: !_allowed
        ? const StatusPanel(title: '需要管理员权限', message: '请返回后切换管理员。')
        : _settings == null
        ? _error == null
              ? const Center(child: CircularProgressIndicator())
              : StatusPanel(title: '无法读取设置', message: _error!, onRetry: _load)
        : Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 720),
              child: ListView(
                padding: const EdgeInsets.all(20),
                children: [
                  if (_busy) const LinearProgressIndicator(),
                  if (_settings!.warning.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 16),
                      child: Text(_settings!.warning),
                    ),
                  DropdownButtonFormField<String>(
                    initialValue: _mode,
                    decoration: const InputDecoration(labelText: '连接方式'),
                    items: const [
                      DropdownMenuItem(value: 'auto', child: Text('自动')),
                      DropdownMenuItem(value: 'direct', child: Text('直连')),
                      DropdownMenuItem(value: 'manual', child: Text('手动代理')),
                    ],
                    onChanged: _busy
                        ? null
                        : (value) => setState(() => _mode = value ?? 'auto'),
                  ),
                  if (_mode == 'auto')
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Text(
                        _settings!.systemProxyStatus.isEmpty
                            ? '使用系统静态代理；未配置时使用环境变量或直连。'
                            : _settings!.systemProxyStatus,
                      ),
                    ),
                  if (_mode == 'manual')
                    Padding(
                      padding: const EdgeInsets.only(top: 16),
                      child: TextField(
                        controller: _proxy,
                        obscureText: !_showProxy,
                        enableSuggestions: false,
                        autocorrect: false,
                        decoration: InputDecoration(
                          labelText: '代理地址',
                          hintText: 'http://127.0.0.1:7890',
                          helperText: '支持 HTTP、HTTPS、SOCKS5；仅保存在本机，不进入配置备份。',
                          helperMaxLines: 3,
                          suffixIcon: IconButton(
                            tooltip: _showProxy ? '隐藏代理地址' : '显示代理地址',
                            onPressed: () =>
                                setState(() => _showProxy = !_showProxy),
                            icon: Icon(
                              _showProxy
                                  ? Icons.visibility_off_outlined
                                  : Icons.visibility_outlined,
                            ),
                          ),
                        ),
                      ),
                    ),
                  const SizedBox(height: 28),
                  _count('目录请求并发', _catalog, (value) => _catalog = value),
                  const SizedBox(height: 16),
                  Text('目录请求间隔 · $_interval 毫秒'),
                  Slider(
                    value: _interval.toDouble(),
                    min: 0,
                    max: 5000,
                    divisions: 100,
                    label: '$_interval 毫秒',
                    onChanged: _busy
                        ? null
                        : (value) => setState(() => _interval = value.round()),
                  ),
                  const SizedBox(height: 16),
                  _count('同时下载数量', _downloads, (value) => _downloads = value),
                  const SizedBox(height: 12),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    value: _folders,
                    title: const Text('按站源分类保存'),
                    subtitle: const Text('新任务放入各站源的子目录，已有下载继续使用原位置。'),
                    onChanged: _busy
                        ? null
                        : (value) => setState(() => _folders = value),
                  ),
                  _verificationSection(context),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Text(
                        _error!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                ],
              ),
            ),
          ),
  );
}
