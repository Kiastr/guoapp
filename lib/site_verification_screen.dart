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
  final _agent = TextEditingController();
  bool _busy = false;
  String? _error;
  String _status = '';
  /// 没拿到 cf_clearance 时先警告一次；用户再点「完成」才强制保存。
  /// 有些站点放行后只下发 __cf_bm，硬拦着不让存反而堵死这条路。
  bool _warnedMissingClearance = false;

  /// WebView 读到的真实 UA（已去掉 wv / Version 标记）。
  ///
  /// 必须用真实的：UA 头说 Windows 桌面 Chrome，而 JS 侧
  /// navigator.userAgentData.platform 说 Android、屏幕是手机尺寸，
  /// Turnstile 一看就知道环境被改过，只会无限挑战。
  String _webViewAgent = '';

  /// WebView 内核实名版本，低于 100 时给用户一句明确提示。
  int _webViewChrome = 0;

  bool get _webViewAvailable => WebViewSniffer.supported;

  @override
  void initState() {
    super.initState();
    _agent.text = widget.target.desktopUserAgent.isNotEmpty
        ? widget.target.desktopUserAgent
        : widget.target.userAgent;
  }

  @override
  void dispose() {
    _cookie.dispose();
    _agent.dispose();
    super.dispose();
  }

  String get _host {
    try {
      return Uri.parse(widget.target.url).host;
    } catch (_) {
      return '';
    }
  }

  /// 最终写进凭据的 UA。
  ///
  /// Android：跟随 WebView 真实 UA，Go 侧会换成同一个。
  /// Windows：没有内置浏览器，用粘贴页里用户填的 UA。
  String get _effectiveAgent {
    if (!widget.target.followBrowserUa) return widget.target.userAgent;
    // 有内置浏览器的平台只认 WebView 读出来的 UA。为空说明页面还没初始化
    // 完，此时回落到桌面 UA 等于给凭据埋一颗雷，宁可让 _save 拦住。
    if (_webViewAvailable) return _webViewAgent;
    final pasted = _agent.text.trim();
    if (pasted.isNotEmpty) return pasted;
    if (widget.target.desktopUserAgent.isNotEmpty) {
      return widget.target.desktopUserAgent;
    }
    return widget.target.userAgent;
  }

  /// 把 WebView 的 UA 改成「同版本手机 Chrome」的 UA。
  ///
  /// Android WebView 的默认 UA 带 `; wv` 和 `Version/4.0` 两个标记，站点
  /// 一眼就知道这是 App 内嵌浏览器，Turnstile 会调到最严。去掉之后 UA 与
  /// 真机 Chrome 完全一致，而 JS 环境本来就是真的——两边终于对得上。
  static String browserUserAgent(String raw) {
    var ua = raw.replaceAll(RegExp(r';\s*wv\b'), '');
    ua = ua.replaceAll(RegExp(r'\s*Version/[\d.]+\s*'), ' ');
    return ua.replaceAll(RegExp(r'\s{2,}'), ' ').trim();
  }

  static int chromeVersionOf(String ua) {
    final match = RegExp(r'Chrome/(\d+)\.').firstMatch(ua);
    return int.tryParse(match?.group(1) ?? '') ?? 0;
  }

  Future<void> _save(String cookie) async {
    final text = cookie.trim();
    if (text.isEmpty) {
      setState(() => _error = 'Cookie 为空，请先在站点页面完成验证。');
      return;
    }
    final agent = _effectiveAgent.trim();
    if (agent.isEmpty) {
      setState(() {
        _error = _webViewAvailable
            ? '内置浏览器还没初始化完，等页面出来再点「完成」。'
            : 'User-Agent 为空。cf_clearance 绑定 UA，缺了它凭据一定无效。';
      });
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
          userAgent: agent,
          cookie: text,
          updatedAt: 0,
        ),
      );
      // 存完立刻用脚本通道试一次。验证页里过了只代表浏览器能用，脚本走
      // 的是另一条链路（代理、TLS 指纹都不同），不试一次等于让用户去猜。
      final check = await widget.repository.verifySiteVerification(
        widget.target.source,
      );
      if (!mounted) return;
      if (check.ok) {
        Navigator.of(context).pop(true);
        return;
      }
      setState(() {
        _error = '凭据已保存，但脚本通道自检没通过：${check.reason}';
        _status = '留在本页可以换个节点重新验证；也可以直接返回，'
            '主页会按新凭据重试。';
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = '保存失败：$error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// 清掉该站点在当前 WebView 里的全部 cookie。
  ///
  /// 为什么非清不可：系统里可能躺着一份别的 UA 签发的 cf_clearance。带着
  /// 它去挑战，Cloudflare 会判断「cookie 与当前指纹不符」，给的不是放行也
  /// 不是普通挑战，而是一遍又一遍的验证。必须从白纸开始。
  ///
  /// 只清目标站：播放嗅探器（webview_sniffer）依赖别的站点的登录态。
  /// 插件一次调用会把这个 url 下能看到的 cookie 全部置为过期，所以按
  /// host-only 与显式 Domain 各删一遍，避免漏掉 `.hanime1.me` 那种写法。
  Future<void> _clearSiteCookies() async {
    final manager = CookieManager.instance();
    try {
      await manager.deleteCookies(url: WebUri(widget.target.url), path: '/');
    } catch (_) {}
    try {
      await manager.deleteCookies(
        url: WebUri(widget.target.url),
        domain: _host,
        path: '/',
      );
    } catch (_) {}
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
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '在下方页面完成站点验证（勾选「确认您是真人」或等待自动通过），'
                '拿到 cf_clearance 后点右上角「完成」。'
                '换代理节点后需要重新验证；日本节点会被站点直接封禁，别用。',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              if (_webViewChrome > 0 && _webViewChrome < 100)
                Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text(
                    '内置浏览器内核是 Chrome/$_webViewChrome，版本太旧，'
                    'Cloudflare 的挑战脚本跑不起来。去应用商店更新'
                    '「Android System WebView」或「Chrome」之后重试。',
                    style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
            ],
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
            // 不在 initialUrlRequest 里加载：必须先拿到 WebView 的真实 UA、
            // 设好再发请求，否则第一跳用的是默认 UA，cookie 就绑错了。
            initialSettings: InAppWebViewSettings(
              javaScriptEnabled: true,
              domStorageEnabled: true,
              databaseEnabled: true,
              thirdPartyCookiesEnabled: true,
              mediaPlaybackRequiresUserGesture: false,
            ),
            onWebViewCreated: (controller) async {
              await _clearSiteCookies();
              String raw = '';
              try {
                raw = await InAppWebViewController.getDefaultUserAgent();
              } catch (_) {}
              final ua = widget.target.followBrowserUa
                  ? browserUserAgent(raw)
                  : widget.target.userAgent;
              final version = chromeVersionOf(ua);
              try {
                await controller.setSettings(
                  settings: InAppWebViewSettings(
                    userAgent: ua,
                    javaScriptEnabled: true,
                    domStorageEnabled: true,
                    databaseEnabled: true,
                    thirdPartyCookiesEnabled: true,
                    mediaPlaybackRequiresUserGesture: false,
                  ),
                );
              } catch (_) {}
              if (mounted) {
                setState(() {
                  _webViewAgent = ua;
                  _webViewChrome = version;
                });
              }
              await controller.loadUrl(
                urlRequest: URLRequest(url: WebUri(widget.target.url)),
              );
            },
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
              final current = url?.toString() ?? '';
              // CF 的挑战页有三个特征：停在 /cdn-cgi/ 路径、标题是
              // "Just a moment..." 或「请稍候」。命中就说明还没过，
              // 直接告诉用户「换节点」比让他反复点更有用。
              final onChallenge = current.contains('/cdn-cgi/') ||
                  title.toLowerCase().contains('just a moment') ||
                  title.contains('请稍候');
              setState(() {
                if (onChallenge) {
                  _status = '仍在 Cloudflare 挑战页'
                      '${title.isEmpty ? '' : '（$title）'}。'
                      '连续点不过说明该节点已被判定为高风险，换一个节点；'
                      '不要在同一节点上反复刷新。';
                } else if (clearance) {
                  _status = '已获取 cf_clearance，可以点右上角「完成」保存。';
                } else if (current.isNotEmpty) {
                  _status = '当前页面：${title.isEmpty ? current : title}';
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
        // UA 必须和「在浏览器里过挑战的那一次」逐字一致：cf_clearance 绑
        // UA。填默认值是给个能用的起点，但 Edge 的 UA 带 Edg/、Firefox 带
        // Gecko，跟这里对不上就白验——所以留成可编辑。
        TextField(
          controller: _agent,
          minLines: 2,
          maxLines: 3,
          enableSuggestions: false,
          autocorrect: false,
          decoration: const InputDecoration(
            labelText: 'User-Agent（必须与浏览器里那次一致）',
            hintText: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) ...',
            helperText: '在开发者工具的「请求标头」里复制 User-Agent 整行覆盖这里。',
            helperMaxLines: 3,
            border: OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
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
