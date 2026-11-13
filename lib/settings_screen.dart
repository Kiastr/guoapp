import 'dart:convert';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'core_bridge.dart';
import 'app_theme.dart';
import 'app_layout.dart';
import 'background_downloads.dart';
import 'local_store.dart';
import 'profiles_screen.dart';
import 'sources_screen.dart';
import 'widgets.dart';
import 'resource_settings_screen.dart';
import 'lan_screen.dart';
import 'webdav_screen.dart';
import 'diary_service.dart';
import 'hongguo_login_dialog.dart';
import 'hongguo_account_sync.dart';
import 'playback_settings_screen.dart';
import 'site_verification_list_screen.dart';

String storageSize(int bytes) {
  if (bytes < 0) return '暂不可用';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  if (bytes < 1024 * 1024 * 1024) {
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
}

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  bool _busy = false;
  String? _message;

  /// 账号同步的进行态与一次性提示。手动按钮与启动自动同步共用
  /// [syncHongguoAccount]，这里只负责把结果画出来。
  bool _syncingAccount = false;
  String _accountSyncNote = '';

  Future<void> _syncAccount() async {
    if (_syncingAccount) return;
    setState(() {
      _syncingAccount = true;
      _accountSyncNote = '';
    });
    final outcome = await syncHongguoAccount(
      widget.repository,
      widget.store,
    );
    if (!mounted) return;
    setState(() {
      _syncingAccount = false;
      _accountSyncNote = outcome.summary;
    });
  }

  /// 同步条目的副标题：优先本次结果，其次上次同步时间。
  String _accountSyncLabel() {
    if (_syncingAccount) return '正在同步收藏与观看记录…';
    if (_accountSyncNote.isNotEmpty) return _accountSyncNote;
    final at = widget.store.hongguoAccountSyncAt;
    if (at <= 0) return '把账号里的收藏与观看记录拉到本机';
    final time = DateTime.fromMillisecondsSinceEpoch(at * 1000);
    final hour = time.hour.toString().padLeft(2, '0');
    final minute = time.minute.toString().padLeft(2, '0');
    return '上次同步：${time.month}月${time.day}日 $hour:$minute';
  }

  Future<void> _chooseTheme() async {
    final selected = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('外观主题'),
        children: [
          RadioGroup<String>(
            groupValue: widget.store.themeMode,
            onChanged: (value) => Navigator.pop(context, value),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final mode in ['light', 'dark', 'system'])
                  RadioListTile<String>(
                    value: mode,
                    title: Text(AppTheme.label(mode)),
                    subtitle: mode == 'system'
                        ? const Text('随设备的深色模式自动切换')
                        : null,
                  ),
              ],
            ),
          ),
        ],
      ),
    );
    if (selected != null && mounted) {
      await saveUserChange(context, () => widget.store.setThemeMode(selected));
    }
  }

  Future<void> _backup(bool restore) async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      if (!restore) {
        final content = await widget.store.exportBackup();
        final saved = await FilePicker.saveFile(
          fileName:
              '$appSlug-backup-${DateTime.now().toIso8601String().substring(0, 10)}.json',
          bytes: Uint8List.fromList(utf8.encode(content)),
          mimeType: 'application/json',
        );
        if (saved != null && mounted) setState(() => _message = '备份已保存');
      } else {
        final file = await FilePicker.pickFile(
          type: FileType.custom,
          allowedExtensions: ['json'],
        );
        if (file == null || !mounted) return;
        final size = await file.length();
        if (size == null || size > 8 * 1024 * 1024) {
          throw const FormatException('备份文件过大或无法读取');
        }
        final content = utf8.decode(await file.readAsBytes());
        final data = widget.store.validateBackup(content);
        if (!mounted) return;
        final accepted = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('恢复备份？'),
            content: Text(
              '包含 ${(data['profiles'] as List).length} 个用户。将替换本机的用户、追剧、观看记录和偏好设置；已下载视频保留。恢复后使用备份内的管理员密码登录。',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('恢复'),
              ),
            ],
          ),
        );
        if (accepted != true || !mounted) return;
        await widget.store.importBackup(content);
        if (mounted) Navigator.of(context).popUntil((route) => route.isFirst);
      }
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _section(BuildContext context, String title) => Padding(
    padding: const EdgeInsets.fromLTRB(0, 20, 0, 6),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const Divider(height: 1),
        const SizedBox(height: 12),
        Text(
          title,
          style: Theme.of(context).textTheme.titleMedium?.copyWith(
            color: Theme.of(context).colorScheme.primary,
          ),
        ),
      ],
    ),
  );

  Future<void> _chooseDisplayMode() async {
    final selection = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('界面模式'),
        children: [
          RadioGroup<String>(
            groupValue: widget.store.displayMode,
            onChanged: (value) => Navigator.pop(context, value),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final mode in const {
                  'auto': '自动识别设备',
                  'television': '电视 / 遥控器',
                  'standard': '手机 / 电脑',
                }.entries)
                  RadioListTile<String>(
                    value: mode.key,
                    autofocus: mode.key == widget.store.displayMode,
                    title: Text(mode.value),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
    if (selection != null && mounted) {
      await saveUserChange(
        context,
        () => widget.store.setDisplayMode(selection),
      );
    }
  }

  Future<void> _about() async => showAboutDialog(
    context: context,
    applicationName: appName,
    applicationVersion: AppLayout.versionOf(context),
    applicationIcon: const Icon(
      Icons.play_circle_filled_rounded,
      size: 48,
      color: Color(0xFFFF765F),
    ),
    children: const [
      Text('独立运行，打开即可浏览和播放。观看记录与追剧收藏保存在当前设备。'),
    ],
  );

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: AppBar(title: const Text('设置与备份')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(0, 4, 0, 0),
                child: Text(
                  '按用途分组。播放类偏好也可以在播放器底部「播放设置」里改。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
              _section(context, '外观与交互'),
              Padding(
                padding: const EdgeInsets.fromLTRB(0, 0, 0, 8),
                child: Text(
                  '影院视觉以深色为基准，封面与播放画面保留更多纯黑；'
                  '底部四个入口使用同一套线性图标，选中时只改变颜色与状态圆点。'
                  '首页顶部是「发现好剧」品牌名，下方小字为当前站源，点开即可切换；'
                  '品牌头与分类标签始终常驻；向上滑动时搜索框自动收起，向下滑动或回到顶部时重新出现，正在输入时不会收起，'
                  '「继续观看」横卡与「为你推荐」标题行也随内容一起收起；横卡条数随屏幕宽度自适应，'
                  '横屏平板会一屏放下更多；卡片只等比缩放，不会被横向拉长压扁，'
                  '超出一屏的记录可向右滑动查看。'
                  '红果推荐的三个档位收进标题行的文字按钮，点开菜单切换，其他站源不显示该按钮。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
              ListTile(
                key: const ValueKey('theme-setting'),
                leading: const Icon(Icons.palette_outlined),
                title: const Text('外观主题'),
                subtitle: Text(AppTheme.label(widget.store.themeMode)),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _chooseTheme,
              ),
              ListTile(
                key: const ValueKey('display-mode-setting'),
                leading: const Icon(Icons.devices_outlined),
                title: const Text('界面模式'),
                subtitle: const Text('自动识别设备 / 电视遥控器 / 手机电脑'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _chooseDisplayMode,
              ),
              _section(context, '播放'),
              ListTile(
                key: const ValueKey('playback-settings'),
                leading: const Icon(Icons.tune_rounded),
                title: const Text('播放偏好与画质增强'),
                subtitle: const Text('倍速、长按倍速、清晰度、弹幕、连播、预加载、画质增强'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => PlaybackSettingsScreen(store: widget.store),
                  ),
                ),
              ),
              _section(context, '内容源'),
              if (widget.repository.supportsSourceManagement)
                ListTile(
                  leading: const Icon(Icons.dns_outlined),
                  title: const Text('站源管理'),
                  subtitle: const Text('独立更新、连接与播放检测'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => SourcesScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
              ListTile(
                key: const ValueKey('site-verification-setting'),
                leading: const Icon(Icons.verified_user_outlined),
                title: const Text('站点验证'),
                subtitle: const Text('Cloudflare 挑战页验证、重新验证与清除凭据'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => SiteVerificationListScreen(
                      repository: widget.repository,
                    ),
                  ),
                ),
              ),
              _section(context, '下载与存储'),
              if (widget.store.canDownload) ...[
                ListTile(
                  leading: const Icon(Icons.download_outlined),
                  title: const Text('下载偏好'),
                  subtitle: Text(widget.store.downloadPreferences.qualityLabel),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) =>
                          DownloadPreferencesScreen(store: widget.store),
                    ),
                  ),
                ),
                ListTile(
                  leading: const Icon(Icons.folder_outlined),
                  title: const Text('下载目录与空间'),
                  subtitle: const Text('查看存储用量、迁移已下载文件'),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => StorageScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
              ],
              if (widget.store.profile.admin) ...[
                SwitchListTile(
                  value: widget.store.autoExport,
                  title: const Text('下载完成后自动导出 Emby'),
                  subtitle: const Text(
                    '在下载目录的 exports 中生成视频和海报 URL 元数据，可将该目录加入 Emby 媒体库。',
                  ),
                  onChanged: _busy
                      ? null
                      : (value) async {
                          setState(() => _busy = true);
                          await BackgroundDownloads.ensureStarted();
                          if (!context.mounted) return;
                          await saveUserChange(
                            context,
                            () => widget.store.setAutoExport(value),
                          );
                          if (mounted) setState(() => _busy = false);
                        },
                ),
                SwitchListTile(
                  value: widget.store.exportPosters,
                  title: const Text('同时导出海报文件'),
                  subtitle: const Text(
                    '默认只写海报 URL。源站海报需要解密或外部读取失败时可开启。',
                  ),
                  onChanged: _busy
                      ? null
                      : (value) => saveUserChange(
                          context,
                          () => widget.store.setExportPosters(value),
                        ),
                ),
              ],
              _section(context, '账号与同步'),
              ListTile(
                key: const ValueKey('lan-settings'),
                leading: const Icon(Icons.devices_rounded),
                title: const Text('设备互联'),
                subtitle: const Text('局域网自动同步追剧与推送播放'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => openLanSync(context),
              ),
              ListTile(
                key: const ValueKey('webdav-settings'),
                leading: const Icon(Icons.cloud_sync_outlined),
                title: const Text('WebDAV 同步'),
                subtitle: const Text(
                  '通过 WebDAV 网盘在多设备间同步追剧、观看记录、片源与收藏状态',
                ),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => WebDavSyncScreen(
                      store: widget.store,
                      repository: widget.repository,
                    ),
                  ),
                ),
              ),
              if (widget.repository.supportsSourceManagement) ...[
                ListTile(
                  key: const ValueKey('hongguo-account'),
                  leading: const Icon(Icons.qr_code_2_rounded),
                  title: const Text('红果账号'),
                  subtitle: const Text('扫码登录，用于同步账号收藏与观看记录'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => openHongguoLogin(context, widget.repository),
                ),
                SwitchListTile(
                  key: const ValueKey('hongguo-account-auto-sync'),
                  secondary: const Icon(Icons.sync_rounded),
                  value: widget.store.hongguoAccountAutoSync,
                  title: const Text('打开应用时同步账号'),
                  subtitle: const Text('启动后自动拉一次收藏与观看记录'),
                  onChanged: (value) => saveUserChange(
                    context,
                    () => widget.store.setHongguoAccountAutoSync(value),
                  ),
                ),
                ListTile(
                  key: const ValueKey('hongguo-account-sync-now'),
                  leading: const Icon(Icons.cloud_download_outlined),
                  title: const Text('立即同步'),
                  subtitle: Text(_accountSyncLabel()),
                  trailing: _syncingAccount
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.chevron_right_rounded),
                  onTap: _syncingAccount ? null : _syncAccount,
                ),
              ],
              ListTile(
                leading: const Icon(Icons.people_outline),
                title: const Text('用户管理'),
                subtitle: Text('当前：${widget.store.profile.name}'),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => ProfilesScreen(store: widget.store),
                  ),
                ),
              ),
              _section(context, '高级与维护'),
              if (widget.store.profile.admin)
                ListTile(
                  leading: const Icon(Icons.settings_ethernet_rounded),
                  title: const Text('网络与资源'),
                  subtitle: const Text('代理、目录请求间隔、下载并发与站源目录'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => ResourceSettingsScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
              ListTile(
                key: const ValueKey('diary-setting'),
                leading: const Icon(Icons.receipt_long_rounded),
                title: const Text('播放调试日记'),
                subtitle: const Text('查看并复制应用运行与播放器的详细事件日志'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => DiaryService.showDiaryDialog(context),
              ),
              if (widget.store.profile.admin) ...[
                ListTile(
                  leading: const Icon(Icons.backup_outlined),
                  title: const Text('导出配置备份'),
                  subtitle: const Text('包含本地用户、追剧、历史和设置，不含视频文件'),
                  onTap: _busy ? null : () => _backup(false),
                ),
                ListTile(
                  leading: const Icon(Icons.restore),
                  title: const Text('恢复配置备份'),
                  onTap: _busy ? null : () => _backup(true),
                ),
              ],
              ListTile(
                leading: const Icon(Icons.info_outline_rounded),
                title: Text('关于$appName'),
                onTap: _about,
              ),
              if (Platform.isIOS)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Text(
                    'iOS 下载和媒体处理需要保持应用在前台；切到后台会暂停，回到前台后可继续。',
                  ),
                ),
              if (_busy) const LinearProgressIndicator(),
              if (_message != null)
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: SelectableText(_message!),
                ),
            ],
          ),
        ),
      ),
    ),
  );
}

class StorageScreen extends StatefulWidget {
  const StorageScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<StorageScreen> createState() => _StorageScreenState();
}

class _StorageScreenState extends State<StorageScreen> {
  Map<String, dynamic>? _info;
  String? _error;
  bool _busy = false;
  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final info = await widget.repository.storage();
      if (mounted) {
        setState(() {
          _info = info;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _move() async {
    String? parent;
    if (Platform.isAndroid || Platform.isIOS) {
      final support = await getApplicationSupportDirectory();
      final directories = <String, String>{support.path: '应用内部存储'};
      if (Platform.isAndroid) {
        final external = await getExternalStorageDirectories() ?? [];
        for (var i = 0; i < external.length; i++) {
          directories[external[i].path] = i == 0
              ? '设备共享存储（应用目录）'
              : 'SD 卡 ${i + 1}（应用目录）';
        }
      } else {
        final documents = await getApplicationDocumentsDirectory();
        directories[documents.path] = '文件 App 可见目录';
      }
      if (!mounted) return;
      parent = await showDialog<String>(
        context: context,
        builder: (context) => SimpleDialog(
          title: const Text('选择下载位置'),
          children: [
            for (final entry in directories.entries)
              SimpleDialogOption(
                onPressed: () => Navigator.pop(context, entry.key),
                child: Text(entry.value),
              ),
          ],
        ),
      );
    } else {
      parent = await FilePicker.getDirectoryPath(dialogTitle: '选择下载保存位置');
    }
    if (parent == null || !mounted) return;
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('迁移已下载内容？'),
        content: Text(
          '将视频、合并成品及导出内容迁移到：\n$parent\n\n下载会先暂停，复制成功后清理旧目录。请保证目标有足够空间，并在完成后继续下载。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('迁移'),
          ),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.repository.moveDownloads(parent);
      await _refresh();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => PopScope(
    canPop: !_busy,
    child: Scaffold(
      appBar: AppBar(title: const Text('下载目录与空间')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (_info != null) ...[
                Text(
                  '已使用 ${storageSize((_info!['bytes'] as num?)?.toInt() ?? 0)}',
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 8),
                Text(
                  '剩余 ${storageSize((_info!['free'] as num?)?.toInt() ?? -1)} · ${_info!['files'] ?? 0} 个文件',
                ),
                const SizedBox(height: 24),
                const Text('当前下载目录'),
                const SizedBox(height: 8),
                SelectableText(_info!['directory'] as String? ?? ''),
                TextButton.icon(
                  onPressed: () => Clipboard.setData(
                    ClipboardData(text: _info!['directory'] as String? ?? ''),
                  ),
                  icon: const Icon(Icons.copy),
                  label: const Text('复制路径'),
                ),
                const SizedBox(height: 20),
                const Text('在下载页删除不需要的分集，在本地媒体页删除合并成品或 Emby 导出，可释放空间。'),
              ],
              if (_busy || (_info == null && _error == null))
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 20),
                  child: LinearProgressIndicator(),
                ),
              if (_busy) const Text('正在迁移，请保持应用运行…'),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Text(_error!),
                ),
              if (widget.store.profile.admin)
                FilledButton.icon(
                  onPressed: _busy ? null : _move,
                  icon: const Icon(Icons.drive_file_move_outline),
                  label: const Text('更改并迁移目录'),
                ),
              TextButton(
                onPressed: _busy ? null : _refresh,
                child: const Text('刷新用量'),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
