import 'dart:async';
import 'dart:io';
import 'dart:ui';

import 'package:media_kit_video/media_kit_video.dart';

/// 画质增强的「平台分流」唯一出口。
///
/// ## 为什么要这一层
///
/// 上游（zhoufuweigg/guoapp 0.2.64）的画质增强是 **Windows 独占**：改表面、
/// d3d11vpp 硬件滤镜、成本上限 4 这些语义直接长在 VideoEnhancementController
/// 里，安卓根本不会进入那些分支（`supported` 恒 false）。
///
/// 本仓库（+83 起）把 Anime4K 移植到安卓后，两套语义挤进了同一个控制器：
/// - **桌面**：表面 = 视口放大后的 output，靠 `VideoController.setSize` 真正
///   改渲染目标，mpv 在放大后的表面上跑 Upscale 链；
/// - **安卓**：表面恒为源尺寸（+140 起可被 `VideoFixedSurface` 一次性锁定），
///   `setSize` 必须 **no-op**，否则会踩 VO 重建 + seek 转圈那三个坑
///   （见下方 [AndroidVideoEnhancementPlatform] 注释）。
///
/// 于是 controller 里散着 5 处 `Platform.isAndroid/isWindows` 判断，Windows
/// 出问题时很难一眼看出走的是哪条路。这里把差异收敛成对象，controller 只认
/// [android] / [hardwareFilter] / [setSurface]，平台判断只在本文件出现。
///
/// ## 分流表
///
/// | 能力 | Windows | macOS / Linux | Android |
/// | --- | --- | --- | --- |
/// | 改表面 | `setSize` 放大 | `setSize` 放大 | no-op（固定表面在创建时锁定） |
/// | 成本上限 | 4（含 hardware） | 4（无 hardware 分支） | 3（anime / fsrcnnx） |
/// | d3d11vpp | 支持 | 不支持 | 不支持 |
/// | detail 文案 | 源→表面尺寸对比 | 同左 | 「已执行超分降噪」 |
abstract class VideoEnhancementPlatform {
  VideoEnhancementPlatform(this.video, {this.onFailure});

  final VideoController video;

  /// 表面尺寸操作失败时的回调（上游安卓 resize 路径遗留，桌面端不触发，
  /// 失败由 controller 的 `_apply` catch 统一处理）。保留以维持调用方签名。
  final void Function()? onFailure;

  /// 是否按安卓语义参与选档：表面 = 源尺寸、神经后端成本上限 3、
  /// detail 只给一句结果文案。对应 `chooseVideoEnhancement(android:)`。
  bool get android;

  /// 是否允许 d3d11vpp 硬件滤镜（只有 Windows 有这条 D3D11 直通路径）。
  bool get hardwareFilter;

  /// detail 里「神经增强已生效」的文案。空串表示交给 controller 输出
  /// 「源尺寸 → 表面尺寸」的通用对比。
  String get neuralDetail;

  /// 等待 VideoController 就绪（platform completer 完成）。
  Future<void> initialize();

  /// 把渲染表面设置为增强期望的尺寸；null 表示交还 media_kit 自身维护。
  Future<void> setSurface(Size? target);

  /// 当前实际表面尺寸（mpv/Flutter 侧上报，非增强期望值）。
  Size? get actualSize;

  Future<void> close();
}

/// 按当前平台创建实现。Windows 单独成一个类型，避免「桌面端」再套
/// `Platform.isWindows` 判断——硬件滤镜的有无就是两条不同的注入路径。
VideoEnhancementPlatform createVideoEnhancementPlatform(
  VideoController video, {
  void Function()? onFailure,
}) {
  if (Platform.isAndroid) {
    return AndroidVideoEnhancementPlatform(video, onFailure: onFailure);
  }
  if (Platform.isWindows) {
    return WindowsVideoEnhancementPlatform(video, onFailure: onFailure);
  }
  return DesktopVideoEnhancementPlatform(video, onFailure: onFailure);
}

/// 桌面端（macOS / Linux）：行为与上游一致——按需把输出表面放大到决策尺寸。
///
/// 上游（Windows 独占版）依赖的正是这条路径：`VideoController.setSize()` →
/// `VideoOutputManager.SetSize` → 原生直接改渲染目标，不动 VO、不 seek、
/// 不碰 demuxer，所以可以随意放大，让 Anime4K 的 2× 内部渲染以放大后的
/// 表面呈现。
class DesktopVideoEnhancementPlatform extends VideoEnhancementPlatform {
  DesktopVideoEnhancementPlatform(super.video, {super.onFailure});

  Future<void>? _initialization;
  Size? _target;
  bool _closed = false;

  @override
  bool get android => false;

  @override
  bool get hardwareFilter => false;

  @override
  String get neuralDetail => '';

  @override
  Size? get actualSize => video.rect.value?.size;

  @override
  Future<void> initialize() => _initialization ??= _initialize();

  Future<void> _initialize() async {
    await video.platform.future;
  }

  @override
  Future<void> setSurface(Size? value) async {
    await initialize();
    if (_closed) return;
    final normalized =
        value == null
            ? null
            : Size(value.width.roundToDouble(), value.height.roundToDouble());
    final changed = _target != normalized;
    _target = normalized;
    // 与上游完全一致：尺寸真变了才调；置空时无条件恢复（交给 media_kit
    // 按视频显示尺寸维护表面）。
    if (changed || normalized == null) {
      await video.setSize(
        width: normalized?.width.toInt(),
        height: normalized?.height.toInt(),
      );
    }
  }

  @override
  Future<void> close() async {
    _closed = true;
    await _initialization;
  }
}

/// Windows：桌面路径 + d3d11vpp 硬件滤镜。
///
/// 硬件后端（`VideoEnhancementBackend.hardware`）只在 Windows 出现：
/// N 卡 / 核显 + d3d11va 解码 + mpv ≥ 0.39 才由 controller 采信，注入方式是
/// `vf add @duanju_vsr:d3d11vpp=...`，把缩放交给驱动视频处理器，几乎不耗
/// 着色器算力。上游 Windows 版就是靠它兜住 4K 片源。
class WindowsVideoEnhancementPlatform extends DesktopVideoEnhancementPlatform {
  WindowsVideoEnhancementPlatform(super.video, {super.onFailure});

  @override
  bool get hardwareFilter => true;
}

/// 安卓：表面绝不由增强侧改动。
///
/// [setSurface] 只记录期望值、绝不下发；需要真超分时由宿主在**创建
/// VideoController 时**下发 `VideoFixedSurface`，fork 在首次 videoParams
/// 事件上一次性放大并锁定（+140）。原因见下方移植结论。
///
/// ## 表面尺寸的平台差异（2026-10 移植修复的核心结论）
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
/// Anime4K 链（+87 起为精细档 7 文件链，+144 前名轻量档，见
/// lib/video_enhancement_assets.dart 的 animeChain）按 WHEN 指令分平台生效：
/// 默认形态下安卓端表面恒为视频显示尺寸（OUTPUT≡MAIN），mpv 自动跳过全部
/// Upscale / AutoDownscalePre pass，仅 Clamp_Highlights + Restore_CNN 生效——
/// 这是**同分辨率重建/降噪**（去压缩噪点、重建线条），也是安卓端默认
/// 唯一生效的形态；桌面端表面仍放大 2×（WHEN 为真），Upscale 链全生效，
/// 内部按 2× 源渲染、Depth-to-Space 输出后由 mpv 缩放到表面。
/// 两条路径都只 `change-list glsl-shaders set`，不动表面尺寸。
///
/// ## 默认形态下如何放大表面（已落地：VideoFixedSurface）
///
/// 真机验证过：播放中改表面必踩上方三个问题，因此放大只能在
/// `VideoController` 创建时一次性指定——宿主给
/// `VideoControllerConfiguration.fixedSurface` 传一个
/// [VideoFixedSurface] 策略（纯数据），fork 在**首次** videoParams 事件上
/// 按「源尺寸 × 倍率」放大表面并锁定，不新增任何调用时机，三个连锁问题
/// 均不会触发。倍率按源尺寸（而非屏幕尺寸）计算以保持视频宽高比，否则
/// mpv 黑边 + Flutter `FittedBox(contain)` 会把视频挤成中央一小块。
///
/// 约束（均已在代码/注释中固化）：
/// - 表面只在播放器创建时锁定一次。竖屏进入后旋转横屏不会重新放大
///   （重新 SetSurfaceSize 就是播放中改表面，会转圈），该次会话只剩
///   同分辨率重建；中途把增强从「关」改到「开」同理，需退出重进播放器。
/// - 放大表面后，video_enhancement.dart 的 `_monitor` 用
///   `expectsUpscale` 强制要求 Anime4K 链出现 `Depth-to-Space` 末 pass
///   （极简档无 Upscale 链，不强制，否则必误报 render-pass），防止
///   Upscale 仍被 WHEN 跳过却误报成功。
/// - 增强策略侧（video_enhancement_policy.dart）拿到的 surfaceOutput 即
///   实际表面尺寸，与 fork 应用的值恒等，sizeMatches 才不会误拉黑后端。
class AndroidVideoEnhancementPlatform extends VideoEnhancementPlatform {
  AndroidVideoEnhancementPlatform(super.video, {super.onFailure});

  Future<void>? _initialization;
  Size? _target;
  bool _closed = false;

  @override
  bool get android => true;

  @override
  bool get hardwareFilter => false;

  @override
  String get neuralDetail => '已执行超分降噪';

  @override
  Size? get actualSize => video.rect.value?.size;

  /// 期望值，仅供诊断；安卓表面由 media_kit 或 VideoFixedSurface 维护。
  Size? get target => _target;

  @override
  Future<void> initialize() => _initialization ??= _initialize();

  Future<void> _initialize() async {
    // 等 VideoController 就绪即可，不注册任何 videoParams 监听——原先在此
    // 监听是为了在 media_kit 把表面重置回 dw×dh 后重新放大，与 media_kit
    // 自身的 videoParamsSubscription 形成乒乓，是播放中转圈与超分被拉黑的
    // 直接原因。
    await video.platform.future;
  }

  @override
  Future<void> setSurface(Size? value) async {
    await initialize();
    if (_closed) return;
    _target =
        value == null
            ? null
            : Size(value.width.roundToDouble(), value.height.roundToDouble());
  }

  @override
  Future<void> close() async {
    _closed = true;
    await _initialization;
  }
}
