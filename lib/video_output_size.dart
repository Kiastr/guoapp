import 'dart:math' as math;
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

/// 安卓固定输出表面的默认策略（实现见 media_kit_video 的 [VideoFixedSurface]）。
///
/// 取值口径：
/// - `maxScale 2.0`：与 Anime4K 链的 2× 内部渲染对齐；
/// - `maxPixels 3686400`（2560×1440）：1080p 片源可放大到 2K 级别，
///   同时挡住 4K 片源无限放大；
/// - `maxDimension 2560`：极端宽高比（如 9:32 竖屏短片）的单边兜底。
///
/// 调大 maxPixels / maxDimension 可换取更高上限的表面，代价是填充率与显存；
/// 真正的瓶颈通常在 video_enhancement_policy.dart 的安卓显存预算（128MB），
/// 源分辨率越大越容易被预算拦下（见 README「画质增强」章节）。
const androidFixedSurfacePolicy = VideoFixedSurface(
  maxScale: 2.0,
  maxPixels: 3686400,
  maxDimension: 2560,
);

/// [+224] 按屏幕物理像素收紧安卓固定表面上限（C 方案，TBDR 带宽优化）。
///
/// 【为什么需要】上面的常量把 maxPixels 固定在 1440p（3.69M）。1080p 手机屏
/// （如 2400×1080 = 2.59M）上，1080p 片源会被放大到 2560×1440 表面，播放器
/// 再把它缩到 1080p 屏——多出的表面像素只增加填充率、显存和合成带宽，屏幕上
/// 一个像素都看不到。移动 GPU 是 TBDR，跨 pass 的全屏读写直通主存，这类
/// 「渲染出来又缩回去」的像素是纯税。
///
/// 【口径】在默认上限基础上再取 `min(屏幕物理像素, 屏幕长边)`：
///   - 屏幕物理像素 = physicalSize 宽 × 高（已含 devicePixelRatio，无需再乘）；
///   - 长边兜底与 maxDimension 同义，防折叠屏/超宽屏把单边拉爆。
/// 取不到视图（无 FlutterView、已销毁）时原样返回默认策略，不影响启动。
///
/// 【已知副作用，改前必读】1080p 屏 + 1080p 源时，倍率约
/// `sqrt(2.59M / 2.07M) ≈ 1.118`，低于 Anime4K Upscale 段 `//!WHEN` 的 1.2
/// 阈值：极简超分档的 5 个 Upscale pass 会被 mpv 跳过，只剩 Clamp_Highlights
/// 的 3 个 pass，效果接近「没有超分」。这是「表面不超过屏幕」的必然结果，
/// 不是 bug——想要超分效果请用 720p 及以下片源（2× 恰好落在屏幕内），
/// 或改用同分辨率重建的极简档。档位说明文案已同步标注。
VideoFixedSurface androidFixedSurfacePolicyForScreen() {
  var maxPixels = androidFixedSurfacePolicy.maxPixels;
  var maxDimension = androidFixedSurfacePolicy.maxDimension;
  try {
    final views = PlatformDispatcher.instance.views;
    if (views.isNotEmpty) {
      final size = views.first.physicalSize;
      final pixels = size.width * size.height;
      if (pixels.isFinite && pixels > 0) {
        maxPixels = math.min(maxPixels, pixels);
        final longSide = math.max(size.width, size.height);
        if (longSide.isFinite && longSide > 0) {
          maxDimension = math.min(maxDimension, longSide);
        }
      }
    }
  } catch (_) {
    // 视图信息不可用时退回编译期默认值，宁可多花一点带宽也不阻塞播放。
  }
  return androidFixedSurfacePolicy.copyWith(
    maxPixels: maxPixels,
    maxDimension: maxDimension,
  );
}

/// 画质增强的平台分流（表面怎么改、能不能用硬件滤镜、detail 怎么写）
/// 已经迁到 `video_enhancement_platform.dart`：
///   - Windows / macOS / Linux → DesktopVideoEnhancementPlatform（`setSize` 放大）
///   - Android → AndroidVideoEnhancementPlatform（no-op，固定表面在创建时锁定）
/// 本文件只保留与平台无关的尺寸计算，以及安卓固定表面的默认策略。
