// 红果扫码登录对话框。
//
// 交互流程：
//   打开 → 申请二维码 → 显示图片 → 每 2 秒轮询一次 →
//   手机确认 → 关闭并提示成功；二维码过期 → 一键刷新。
//
// 两个刻意的设计取舍：
//
//  1. **轮询间隔 2 秒**。服务端二维码有效期实测只有 60 秒，间隔太长会
//     错过确认时机，太短则是无意义的空转。2 秒是「60 秒内最多 30 次请求」
//     与「确认后 2 秒内反馈」的折中。
//  2. **倒计时归零就停**。不靠「poll 返回 expired」被动收尾——那样在用户
//     已经离开的情况下还会持续打服务端。归零即停，界面显示可刷新。
//
// 文案上不说「登录解锁更多内容」：登录只影响账号态（收藏 / 历史 / 续播），
// 取流与画质与登录无关。说错话会让用户以为登录能看 VIP。

import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'hongguo_login.dart';

/// 打开红果账号对话框。返回值仅表示「对话框关掉了」，登录态请重新查。
Future<void> openHongguoLogin(
  BuildContext context,
  AppRepository repository,
) {
  return showDialog<void>(
    context: context,
    builder: (_) => HongguoLoginDialog(repository: repository),
  );
}

class HongguoLoginDialog extends StatefulWidget {
  const HongguoLoginDialog({super.key, required this.repository});

  final AppRepository repository;

  @override
  State<HongguoLoginDialog> createState() => _HongguoLoginDialogState();
}

class _HongguoLoginDialogState extends State<HongguoLoginDialog> {
  HongguoLoginState _state = const HongguoLoginState();
  HongguoLoginQRCode? _qr;
  String _status = '';
  String _message = '';
  bool _loading = true;
  bool _polling = false;
  int _remaining = 0;

  Timer? _pollTimer;
  Timer? _countdownTimer;

  @override
  void initState() {
    super.initState();
    _bootstrap();
  }

  @override
  void dispose() {
    _stopTimers();
    super.dispose();
  }

  void _stopTimers() {
    _pollTimer?.cancel();
    _pollTimer = null;
    _countdownTimer?.cancel();
    _countdownTimer = null;
    _polling = false;
  }

  Future<void> _bootstrap() async {
    setState(() => _loading = true);
    try {
      final state = await widget.repository.hongguoLoginStatus();
      if (!mounted) return;
      if (state.signedIn) {
        setState(() {
          _state = state;
          _loading = false;
        });
        return;
      }
    } catch (_) {
      // 读本地状态失败不该挡住扫码入口，继续走申请二维码。
    }
    if (!mounted) return;
    await _refresh();
  }

  Future<void> _refresh() async {
    _stopTimers();
    setState(() {
      _loading = true;
      _message = '';
      _status = '';
      _qr = null;
    });
    try {
      final qr = await widget.repository.hongguoLoginCreate();
      if (!mounted) return;
      setState(() {
        _qr = qr;
        _loading = false;
        _remaining = qr.expiresIn > 0 ? qr.expiresIn : 60;
        _status = '等待手机扫码';
      });
      _startTimers();
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _message = '$error';
      });
    }
  }

  void _startTimers() {
    _pollTimer = Timer.periodic(const Duration(seconds: 2), (_) => _poll());
    _countdownTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      if (_remaining <= 1) {
        _stopTimers();
        setState(() {
          _remaining = 0;
          _status = '二维码已过期';
          _message = '点「刷新二维码」重新获取';
        });
        return;
      }
      setState(() => _remaining -= 1);
    });
  }

  Future<void> _poll() async {
    final token = _qr?.token ?? '';
    if (token.isEmpty || _polling) return;
    _polling = true;
    try {
      final result = await widget.repository.hongguoLoginPoll(token);
      if (!mounted) return;
      if (result.confirmed) {
        _stopTimers();
        setState(() {
          _state = result.state ?? const HongguoLoginState(signedIn: true);
          _status = '';
          _message = '';
        });
        return;
      }
      if (result.expired) {
        _stopTimers();
        setState(() {
          _remaining = 0;
          _status = '二维码已过期';
          _message = result.description.isNotEmpty
              ? result.description
              : '点「刷新二维码」重新获取';
        });
        return;
      }
      // new（或服务端其他中间态）：保持二维码，刷新提示语。
      if (result.description.isNotEmpty && result.description != _status) {
        setState(() => _status = result.description);
      }
    } catch (_) {
      // 单次轮询失败不打断整体流程（网络抖动、切后台都可能），
      // 下一轮继续；真正的失效由服务端 status 或倒计时收尾。
    } finally {
      _polling = false;
    }
  }

  Future<void> _verify() async {
    setState(() => _loading = true);
    try {
      final state = await widget.repository.hongguoLoginVerify();
      if (!mounted) return;
      setState(() {
        _state = state;
        _loading = false;
        _message = state.signedIn ? '登录态有效' : '登录已失效，请重新扫码';
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _message = '校验失败：$error';
      });
    }
  }

  Future<void> _logout() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('退出红果账号？'),
        content: const Text('本机保存的登录凭据会被删除，观看记录不受影响。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('退出'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    setState(() => _loading = true);
    try {
      await widget.repository.hongguoLoginLogout();
      if (!mounted) return;
      setState(() {
        _state = const HongguoLoginState();
        _loading = false;
        _message = '已退出登录';
      });
      await _refresh();
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _message = '退出失败：$error';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Row(
        children: [
          Icon(Icons.qr_code_2_rounded),
          SizedBox(width: 8),
          Text('红果账号'),
        ],
      ),
      content: SizedBox(
        width: 320,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text(
                '用红果 App 扫码即可登录。登录后用于收藏、观看历史与云端续播；'
                '播放取流不需要登录，画质也不受登录状态影响。',
                style: TextStyle(fontSize: 12),
              ),
              const SizedBox(height: 16),
              if (_loading)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 32),
                  child: Center(child: CircularProgressIndicator()),
                )
              else if (_state.signedIn)
                _buildSignedIn()
              else
                _buildQRCode(),
              if (_message.isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(
                  _message,
                  style: TextStyle(
                    fontSize: 12,
                    color: Theme.of(context).colorScheme.error,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('关闭'),
        ),
        if (!_loading && _state.signedIn) ...[
          TextButton(onPressed: _verify, child: const Text('校验登录态')),
          TextButton(
            onPressed: _logout,
            style: TextButton.styleFrom(
              foregroundColor: Theme.of(context).colorScheme.error,
            ),
            child: const Text('退出登录'),
          ),
        ],
        if (!_loading && !_state.signedIn)
          FilledButton(
            onPressed: _refresh,
            child: Text(_qr == null ? '获取二维码' : '刷新二维码'),
          ),
      ],
    );
  }

  Widget _buildSignedIn() {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(12),
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
      ),
      child: Column(
        children: [
          const Icon(Icons.verified_user_rounded, size: 36),
          const SizedBox(height: 8),
          Text(
            _state.displayName,
            style: Theme.of(context).textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 4),
          const Text('已登录', style: TextStyle(fontSize: 12)),
        ],
      ),
    );
  }

  Widget _buildQRCode() {
    final qr = _qr;
    if (qr == null || qr.image.isEmpty) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 24),
        child: Center(child: Text('还没有二维码')),
      );
    }
    final bytes = base64Decode(qr.image);
    return Column(
      children: [
        Container(
          padding: const EdgeInsets.all(8),
          decoration: BoxDecoration(
            color: Colors.white,
            borderRadius: BorderRadius.circular(8),
          ),
          child: Image.memory(
            bytes,
            width: 220,
            height: 220,
            gaplessPlayback: true,
            filterQuality: FilterQuality.none,
            errorBuilder: (context, error, stack) => const SizedBox(
              width: 220,
              height: 220,
              child: Center(child: Text('二维码渲染失败，请刷新')),
            ),
          ),
        ),
        const SizedBox(height: 12),
        Text(
          _remaining > 0 ? '$_status · 剩余 $_remaining 秒' : _status,
          style: const TextStyle(fontSize: 12),
          textAlign: TextAlign.center,
        ),
      ],
    );
  }
}
