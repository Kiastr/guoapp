import 'dart:io';
import 'dart:ui';

import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

Size? videoDisplaySize(VideoParams parameters) {
  final width = (parameters.dw ?? 0) > 0 ? parameters.dw! : parameters.w ?? 0;
  final height = (parameters.dh ?? 0) > 0 ? parameters.dh! : parameters.h ?? 0;
  if (width <= 0 || height <= 0) return null;
  final rotation = (parameters.rotate ?? 0) % 180;
  return rotation == 90
      ? Size(height.toDouble(), width.toDouble())
      : Size(width.toDouble(), height.toDouble());
}

/// 画质增强的"输出表面尺寸"适配器。
///
/// ## 平台差异（2026-10 移植修复的核心结论）
///
/// 桌面端（Windows/macOS/Linux）与安卓端修改视频输出尺寸的路径完全不同：
///
/// - **桌面端**：`VideoController.setSize()` → `VideoOutputManager.SetSize`
///   → 原生 C++ 直接改渲染目标尺寸。不动 VO、不 seek、不碰 demuxer，
///   没有任何播放副作用，因此上游（Windows 独占版）可以随意放大表面，
///   让 Anime4K 的 2× 内部渲染以放大后的表面呈现，换取额外锐度。
/// - **安卓端**：`AndroidVideoController.setSize()` 直接抛
///   `UnsupportedError`（media_kit_video 2.0.1
///   android_video_controller/real.dart:220-228），原先只能改道
///   `VideoOutputManager.SetSurfaceSize` MethodChannel。
///
/// ## 为什么安卓端最终选择"完全不放大表面"
///
/// 真机验证发现 `SetSurfaceSize` 路径有三个连锁问题：
///
/// 1. **VO 重建 + seek → 转圈**：
///    `VideoOutput.setSurfaceSize`（media_kit_video 2.0.1
///    VideoOutput.java:75-101）会重设 SurfaceTexture 缓冲尺寸并回调
///    `onSurfaceAvailable()`，回传 `VideoOutput.Resize`；一旦 wid 指针变化，
///    `AndroidVideoController.widListener`（同包 real.dart:49-76）执行
///    `vo=null` → 重设 `android-surface-size`/`wid`/`vo` →
///    `player.seek(当前位置)`。seek 使 mpv `core-idle` /
///    `paused-for-cache` 置位，media_kit 据此把 `state.buffering` 置 true
///    （media_kit-1.2.6 native/player/real.dart:1517/1536），
///    播放器界面随即画出转圈图标（player_controls.dart:254）。
/// 2. **与 media_kit 自身监听乒乓**：media_kit 自带的 `videoParamsSubscription`
///    （real.dart:87-129）会在每次 videoParams 事件时把表面重置回视频显示
///    尺寸 dw×dh；增强侧又强制放大，形成"每次 videoParams 事件翻转两次
///    表面"的循环，无法收敛，表现为"转圈→停止→再转圈"。
/// 3. **误触增强自愈 → 后端被永久拉黑**：抖动期间 mpv 打出 vo/gpu 相关
///    告警，被 video_enhancement.dart 的 `_onLog` /
///    `handlePlaybackError` 判定为"增强渲染失败"而 `_failBackend`；
///    `_rejected` / `_costLimit` 整集有效，导致进全屏后神经超分再也不回来。
///
/// ## 为什么不放大会表面也有超分效果
///
/// Anime4K 链（assets/video_enhancement/Anime4K_Upscale_Denoise_CNN_x2_S.glsl）
/// 内部按 **2× 源分辨率** 渲染：前 4 个 Conv pass 在源尺寸的 SAVE 纹理上
/// 运算，最终 Depth-to-Space pass 声明 `//!WIDTH conv2d_last_tf.w 2 *`
/// 输出 2× 画面，再由 mpv 缩放呈现到当前表面。因此表面保持视频显示尺寸时，
/// CNN 重建/降噪效果完整保留，仅放弃"表面真放大"带来的额外空间锐度——
/// 与 Kazumi 安卓端（仅 `change-list glsl-shaders set`，不动表面尺寸）一致。
///
/// 若未来确需表面放大：只能在 `VideoController` 创建时通过
/// `VideoControllerConfiguration` 一次性指定 width/height，播放中改必踩上述
/// 三个问题；届时需同步调整 video_enhancement_policy 的 output 语义与
/// video_enhancement.dart `_monitor` 的 sizeMatches 校验。
class VideoOutputSizeAdapter {
  VideoOutputSizeAdapter(this.video, {required this.onFailure});

  final VideoController video;

  /// 表面尺寸操作失败时的回调。
  ///
  /// [移植改造] 安卓端放弃放大表面后此回调不再被触发（桌面端 setSize 的异常
  /// 由 VideoEnhancementController._enqueue 统一捕获）。保留形参以维持调用方
  /// （video_enhancement.dart）构造签名不变。
  final void Function() onFailure;
  Future<void>? _initialization;
  Size? _target;
  bool _closed = false;

  /// 当前实际表面尺寸（mpv/Flutter 侧上报，非增强期望值）。
  Size? get actualSize => video.rect.value?.size;

  Future<void> initialize() => _initialization ??= _initialize();

  Future<void> _initialize() async {
    // 等待平台侧 VideoController 就绪即可；安卓端不再注册任何 videoParams
    // 监听——原先在此监听是为了在 media_kit 把表面重置回 dw×dh 后重新放大，
    // 与 media_kit 自身的 videoParamsSubscription 形成乒乓，是播放中转圈与
    // 超分被拉黑的直接原因，详见类注释。
    await video.platform.future;
  }

  /// 设置增强期望的输出表面尺寸。
  ///
  /// [移植改造] 安卓端整体为 no-op：表面尺寸交给 media_kit 自身维护
  /// （其 videoParamsSubscription 会把表面同步为视频显示尺寸 dw×dh）。
  /// 期望值仍记录在 [_target] 中供诊断/将来使用，但绝不触碰表面。
  /// 桌面端行为与上游完全一致（仅在尺寸真正变化时调用 setSize）。
  Future<void> setTarget(Size? value) async {
    await initialize();
    if (_closed) return;
    final normalized = value == null
        ? null
        : Size(value.width.roundToDouble(), value.height.roundToDouble());
    final changed = _target != normalized;
    _target = normalized;
    if (Platform.isAndroid) {
      // 安卓：仅记录，绝不调用 VideoOutputManager.SetSurfaceSize。
      // 原因（VO 重建 + seek → 转圈、与 media_kit 乒乓、误触自愈拉黑）
      // 详见类注释。
      return;
    }
    if (changed || normalized == null) {
      await video.setSize(
        width: normalized?.width.toInt(),
        height: normalized?.height.toInt(),
      );
    }
  }

  Future<void> close() async {
    _closed = true;
    await _initialization;
  }
}
