import 'models.dart';

enum PlaybackRecoveryAction { alternative, refresh, stop }

/// 自动恢复策略：先把这一集的其他线路都试一遍（本地切换，不重新解析），
/// 全部试过之后再重新解析一次（可能是播放凭证过期），最后才报错。
///
/// 线路按环形处理：记忆停在最后一条时，下一条回到首选线路；但如果下一条
/// 已经试过（说明是绕回来的），就不再重复，直接进入刷新或停止，避免在同
/// 一组坏线之间打转，也避免多余的请求。
class PlaybackRecovery {
  static const maxAttempts = 3;
  int _attempts = 0;
  bool _refreshed = false;
  final Set<int> _tried = {};

  void reset() {
    _attempts = 0;
    _refreshed = false;
    _tried.clear();
  }

  PlaybackRecoveryAction next(PlaybackPlan plan) {
    if (_attempts >= maxAttempts) {
      return PlaybackRecoveryAction.stop;
    }
    _tried.add(plan.routeIndex);
    final nextRoute = plan.routeCount > 1
        ? (plan.routeIndex + 1) % plan.routeCount
        : plan.routeIndex;
    if (plan.hasAlternative && !_tried.contains(nextRoute)) {
      _attempts++;
      return PlaybackRecoveryAction.alternative;
    }
    if (!_refreshed) {
      _attempts++;
      _refreshed = true;
      // 重新解析后线路可能已经刷新（凭证/地址变化），允许再把各条线试一遍。
      _tried.clear();
      return PlaybackRecoveryAction.refresh;
    }
    return PlaybackRecoveryAction.stop;
  }
}

class PlaybackHealth {
  static const stallTimeout = Duration(seconds: 20);
  Duration? _position;
  Duration? _buffer;
  DateTime? _lastProgress;

  void reset() {
    _position = null;
    _buffer = null;
    _lastProgress = null;
  }

  /// [buffer] 是播放器已缓冲到的位置（mpv 的 `demuxer-cache-time`）。
  ///
  /// 慢网络下缓存供不上时，播放位置会停在缓存末端等分片，但缓存本身仍在
  /// 推进——这是「正在缓冲」，不是「卡死」。只盯 position 会把正常缓冲误判
  /// 成 stall 并强制重开整条流，表现为进度条反复从头加载、已缓冲进度全丢。
  /// 位置或缓存任一前进都算有进展；两者同时 20 秒不动才是真的卡死。
  bool stalled({
    required Duration position,
    required Duration buffer,
    required bool playing,
    required bool foreground,
    required DateTime now,
  }) {
    if (!playing ||
        !foreground ||
        position != _position ||
        buffer != _buffer) {
      _position = position;
      _buffer = buffer;
      _lastProgress = now;
      return false;
    }
    _lastProgress ??= now;
    return now.difference(_lastProgress!) >= stallTimeout;
  }
}
