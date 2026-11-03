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

/// 画质增强的平台分流（表面怎么改、能不能用硬件滤镜、detail 怎么写）
/// 已经迁到 `video_enhancement_platform.dart`：
///   - Windows / macOS / Linux → DesktopVideoEnhancementPlatform（`setSize` 放大）
///   - Android → AndroidVideoEnhancementPlatform（no-op，固定表面在创建时锁定）
/// 本文件只保留与平台无关的尺寸计算，以及安卓固定表面的默认策略。
