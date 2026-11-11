import 'package:flutter/material.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';

import 'core_bridge.dart';
import 'site_verification.dart';
import 'webview_sniffer.dart';

/// [站点验证] 全屏验证页。
///
/// 站点用 Cloudflare 挑战挡住脚本请求时，只有真实浏览器能通过。这个页面
/// 用内置 WebView 打开站点，用户在页面里完成验证后点「完成」，页面把
/// WebView 里的全部 cookie 交给 Go 侧复用。
///
/// 为什么不自动检测通过：挑战成功的判定只有「服务端下发了 cf_clearance」，
/// 而用户可能只是想看一眼页面。让用户点「完成」更直观，也能避免在还没
/// 通过时就把空 cookie 存进去。
///
/// 桌面端（Windows）没有可用的 WebView 实现，退化成「浏览器验证 + 粘贴
/// Cookie」；两条路最后走同一个 repository.saveSiteVerification。
class SiteVerificationScreen extends StatefulWidget {
  const SiteVerificationScreen({
    super.key,
    required this.repository,
    required this.target,
  });

  final AppRepository repository;
  final SiteVerificationTarget target;

  @override
  State<SiteVerificationScreen> createState() => _SiteVerificationScreenState();
}

class _SiteVerificationScreenState extends State<SiteVerificationScreen> {
  final _cookie = TextEditingController();
  bool _busy = false;
  String? _error;
  String _status = '';
  /// 没拿到 cf_clearance 时先警告一次；用户再点「完成」才强制保存。
  /// 有些站点放行后只下发 __cf_bm，硬拦着不让存反而堵死这条路。
  bool _warnedMissingClearance = false;

  bool get _webViewAvailable => WebViewSniffer.supported;

  @override
  void dispose() {
    _cookie.dispose();
    super.dispose();
  }

  String get _host {
    try {
      return Uri.parse(widget.target.url).host;
    } catch (_) {
      return '';
    }
  }

  Future<void> _save(String cookie) async {
    final text = cookie.trim();
    if (text.isEmpty) {
      setState(() => _error = 'Cookie 为空，请先在站点页面完成验证。');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.repository.saveSiteVerification(
        SiteVerification(
          source: widget.target.source,
          host: _host,
          userAgent: widget.target.userAgent,
          cookie: text,
          updatedAt: 0,
        ),
      );
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = '保存失败：$error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// 从 WebView 的 CookieManager 取全部 cookie。
  ///
  /// 必须取全部而不是只找 cf_clearance：__cf_bm 约 30 分钟过期，但每次
  /// 响应都会续期，缺了它请求照样会被挑战。
  ///
  /// 但 cf_clearance 的有无是「挑战到底过没过」的唯一可判定信号，所以它
  /// 单独检查一次：没有就先警告，让用户别在没通过时白存一条无效凭据。
  Future<void> _collectFromWebView() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final cookies = await CookieManager.instance().getCookies(
        url: WebUri(widget.target.url),
      );
      final usable = cookies
          .where((cookie) => cookie.name.isNotEmpty && cookie.value.isNotEmpty)
          .toList();
      final header = usable
          .map((cookie) => '${cookie.name}=${cookie.value}')
          .join('; ');
      if (header.isEmpty) {
        if (!mounted) return;
        setState(() {
          _error = '还没有拿到站点 Cookie。请在页面里完成验证，能看到站点内容后再点「完成」。';
        });
        return;
      }
      final hasClearance =
          usable.any((cookie) => cookie.name == 'cf_clearance');
      if (!hasClearance && !_warnedMissingClearance) {
        if (!mounted) return;
        setState(() {
          _warnedMissingClearance = true;
          _error = '没有检测到 cf_clearance。通常有两种情况：① 挑战还没通过'
              '（页面仍停在「请稍候 / Just a moment」）；② 该节点被直接放行，'
              '本来就不需要验证。如果页面确实已经能正常浏览，再点一次「完成」强制保存。';
        });
        return;
      }
      await _save(header);
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = '读取 Cookie 失败：$error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('验证 ${widget.target.name}'),
        actions: [
          if (_webViewAvailable)
            TextButton(
              onPressed: _busy ? null : _collectFromWebView,
              child: const Text('完成'),
            ),
        ],
      ),
      body: _webViewAvailable ? _buildWebView() : _buildPaste(),
    );
  }

  Widget _buildWebView() {
    return Column(
      children: [
        Container(
          width: double.infinity,
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
          child: Text(
            '在下方页面完成站点验证（勾选「确认您是真人」或等待自动通过），'
            '拿到 cf_clearance 后点右上角「完成」。'
            '换代理节点后需要重新验证；日本节点会被站点直接封禁，别用。',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
        if (_error != null)
          Container(
            width: double.infinity,
            color: Theme.of(context).colorScheme.errorContainer,
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
            child: Text(
              _error!,
              style: TextStyle(
                color: Theme.of(context).colorScheme.onErrorContainer,
              ),
            ),
          ),
        if (_status.isNotEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 6),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(
                _status,
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ),
          ),
        Expanded(
          child: InAppWebView(
            initialUrlRequest: URLRequest(url: WebUri(widget.target.url)),
            initialSettings: InAppWebViewSettings(
              // 必须与 Go 侧该源请求的 UA 逐字一致，否则 cf_clearance 绑不上。
              userAgent: widget.target.userAgent,
              javaScriptEnabled: true,
              domStorageEnabled: true,
              databaseEnabled: true,
              thirdPartyCookiesEnabled: true,
              sharedCookiesEnabled: true,
              mediaPlaybackRequiresUserGesture: false,
            ),
            onLoadStop: (controller, url) async {
              if (!mounted) return;
              String title = '';
              bool clearance = false;
              try {
                title = await controller.getTitle() ?? '';
              } catch (_) {}
              try {
                final cookies = await CookieManager.instance().getCookies(
                  url: WebUri(widget.target.url),
                );
                clearance =
                    cookies.any((cookie) => cookie.name == 'cf_clearance');
              } catch (_) {}
              if (!mounted) return;
              final target = url?.toString() ?? '';
              // CF 的挑战页有三个特征：停在 /cdn-cgi/ 路径、标题是
              // "Just a moment..." 或「请稍候」。命中就说明还没过，
              // 直接告诉用户「换节点」比让他反复点更有用。
              final onChallenge = target.contains('/cdn-cgi/') ||
                  title.toLowerCase().contains('just a moment') ||
                  title.contains('请稍候');
              setState(() {
                if (onChallenge) {
                  _status = '仍在 Cloudflare 挑战页'
                      '${title.isEmpty ? '' : '（$title）'}。'
                      '反复点不过说明该节点已被判定为高风险，换一个节点；'
                      '不要连续刷新。';
                } else if (clearance) {
                  _status = '已获取 cf_clearance，可以点右上角「完成」保存。';
                } else if (target.isNotEmpty) {
                  _status = '当前页面：${title.isEmpty ? target : title}';
                } else {
                  _status = '';
                }
              });
            },
            onReceivedError: (controller, request, error) {
              if (!mounted || request.isForMainFrame != true) return;
              setState(() {
                _status = '页面加载失败：${error.description}';
              });
            },
          ),
        ),
      ],
    );
  }

  /// 桌面端：没有内置 WebView，用系统浏览器验证后粘贴 Cookie。
  Widget _buildPaste() {
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Text(
          '当前平台没有内置浏览器，请按下面步骤操作：',
          style: Theme.of(context).textTheme.titleSmall,
        ),
        const SizedBox(height: 8),
        Text(
          '1. 用系统浏览器打开 ${widget.target.url}\n'
          '2. 完成站点验证，能看到站点内容\n'
          '3. 按 F12 打开开发者工具，切到「网络 / Network」\n'
          '4. 刷新页面，点任意一个请求，在「请求标头」里找到 Cookie\n'
          '5. 复制整行（形如 name=value; name2=value2），粘贴到下面',
          style: Theme.of(context).textTheme.bodySmall,
        ),
        if (widget.target.note.isNotEmpty) ...[
          const SizedBox(height: 8),
          Text(
            widget.target.note,
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
        const SizedBox(height: 16),
        TextField(
          controller: _cookie,
          minLines: 4,
          maxLines: 8,
          enableSuggestions: false,
          autocorrect: false,
          decoration: const InputDecoration(
            labelText: 'Cookie',
            hintText: 'cf_clearance=...; __cf_bm=...',
            helperText: '只保存在本机，不进配置备份；换代理节点后需要重新验证。',
            helperMaxLines: 3,
            border: OutlineInputBorder(),
          ),
        ),
        if (_error != null) ...[
          const SizedBox(height: 12),
          Text(
            _error!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ],
        const SizedBox(height: 16),
        FilledButton(
          onPressed: _busy ? null : () => _save(_cookie.text),
          child: Text(_busy ? '保存中…' : '保存'),
        ),
      ],
    );
  }
}
