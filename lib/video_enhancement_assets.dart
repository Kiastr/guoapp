import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

class VideoEnhancementAssets {
  static const root = 'assets/video_enhancement';
  static const ravuRgb = 'ravu-r2-rgb.hook';
  static const ravuLuma = 'ravu-lite-ar-r2.hook';
  static const fsrcnnx = 'FSRCNNX_x2_8-0-4-1.glsl';
  /// Anime4K 极简档链（+95：轻量档的瘦身版，只留 S 档重建网络）。
  /// Clamp_Highlights 3 个 pass + Restore_CNN_S 4 个 pass，去掉轻量档里的
  /// Restore_CNN_M（8 pass）与 Upscale 链；安卓端 Upscale 本就被 WHEN 跳过，
  /// 所以实际每帧从 15 个 pass 降到 7 个、卷积量约为原来的 30%，
  /// 骁龙 865（Adreno 650）一类 GPU 能跑，仍保留同分辨率重建带来的清晰感。
  static const animeChainMinimal = <String>[
    'Anime4K_Clamp_Highlights.glsl',
    'Anime4K_Restore_CNN_S.glsl',
  ];

  /// Anime4K 轻量档链（+87：Kazumi 轻量档平权，顺序即注入顺序）。
  /// Clamp_Highlights + Restore_CNN_M + Restore_CNN_S + Upscale_CNN_x2_M
  /// + AutoDownscalePre_x2/x4 + Upscale_CNN_x2_S。
  /// WHEN 全部保留：安卓端表面恒源尺寸，mpv 自动跳过 Upscale/AutoDownscalePre，
  /// 仅 Clamp + Restore 生效（同分辨率重建/降噪）；桌面端表面放大 2× 时全链生效。
  /// 与 Kazumi lib/utils/constants.dart 的 mpvAnime4KShadersLite 一致。
  static const animeChainLite = <String>[
    'Anime4K_Clamp_Highlights.glsl',
    'Anime4K_Restore_CNN_M.glsl',
    'Anime4K_Restore_CNN_S.glsl',
    'Anime4K_Upscale_CNN_x2_M.glsl',
    'Anime4K_AutoDownscalePre_x2.glsl',
    'Anime4K_AutoDownscalePre_x4.glsl',
    'Anime4K_Upscale_CNN_x2_S.glsl',
  ];

  /// Anime4K 质量档链（+89：Kazumi 质量档平权，顺序即注入顺序）。
  /// Clamp_Highlights + Restore_CNN_VL + Upscale_CNN_x2_VL
  /// + AutoDownscalePre_x2/x4 + Upscale_CNN_x2_M。
  /// 与 Kazumi lib/utils/constants.dart 的 mpvAnime4KShaders 一致。
  /// VL 变体层数多、权重文件大（各 ~145KB），负载显著高于轻量档，
  /// 低端设备可能触发 video_enhancement.dart 的 _watchPerformance 自动降级。
  static const animeChainQuality = <String>[
    'Anime4K_Clamp_Highlights.glsl',
    'Anime4K_Restore_CNN_VL.glsl',
    'Anime4K_Upscale_CNN_x2_VL.glsl',
    'Anime4K_AutoDownscalePre_x2.glsl',
    'Anime4K_AutoDownscalePre_x4.glsl',
    'Anime4K_Upscale_CNN_x2_M.glsl',
  ];
  static final _pending = <String, Future<String>>{};

  static Future<String> prepare(String name) async {
    final existing = _pending[name];
    if (existing != null) return existing;
    final future = _prepare(name);
    _pending[name] = future;
    try {
      return await future;
    } finally {
      if (identical(_pending[name], future)) _pending.remove(name);
    }
  }

  static Future<String> _prepare(String name) async {
    if (!RegExp(r'^[A-Za-z0-9_.-]+$').hasMatch(name)) {
      throw const FormatException('增强资源名称无效');
    }
    final manifest =
        jsonDecode(await rootBundle.loadString('$root/manifest.json')) as Map;
    final entry = (manifest['shaders'] as Map)[name] as Map?;
    final digest = entry?['sha256'] as String? ?? '';
    final length = entry?['bytes'] as int? ?? 0;
    if (!RegExp(r'^[a-f0-9]{64}$').hasMatch(digest) ||
        length <= 0 ||
        length > 4 * 1024 * 1024) {
      throw const FormatException('增强资源清单无效');
    }
    final directory = Directory(
      path.join(
        (await getApplicationSupportDirectory()).path,
        'video_enhancement',
        'v1',
      ),
    );
    await directory.create(recursive: true);
    final destination = File(path.join(directory.path, '$digest-$name'));
    if (await destination.exists() && await destination.length() == length) {
      if (sha256.convert(await destination.readAsBytes()).toString() ==
          digest) {
        return destination.path;
      }
    }
    final data = await rootBundle.load('$root/$name');
    final bytes = data.buffer.asUint8List(
      data.offsetInBytes,
      data.lengthInBytes,
    );
    if (bytes.length != length || sha256.convert(bytes).toString() != digest) {
      throw const FormatException('增强资源校验失败');
    }
    final temporary = File(
      '${destination.path}.${DateTime.now().microsecondsSinceEpoch}.tmp',
    );
    File? previous;
    try {
      await temporary.writeAsBytes(bytes, flush: true);
      if (Platform.isWindows && await destination.exists()) {
        previous = await destination.rename('${temporary.path}.previous');
      }
      await temporary.rename(destination.path);
      if (previous != null && await previous.exists()) await previous.delete();
    } catch (_) {
      if (previous != null &&
          await previous.exists() &&
          !await destination.exists()) {
        await previous.rename(destination.path);
      }
      rethrow;
    } finally {
      if (await temporary.exists()) await temporary.delete();
    }
    return destination.path;
  }

  static void registerLicenses() {
    LicenseRegistry.addLicense(() async* {
      yield LicenseEntryWithLineBreaks(
        ['RAVU', 'FSRCNNX'],
        '${await rootBundle.loadString('$root/LGPL-3.0.txt')}\n\n'
        '${await rootBundle.loadString('$root/GPL-3.0.txt')}',
      );
      yield LicenseEntryWithLineBreaks([
        'Anime4K',
      ], await rootBundle.loadString('$root/Anime4K-LICENSE.txt'));
    });
  }
}
