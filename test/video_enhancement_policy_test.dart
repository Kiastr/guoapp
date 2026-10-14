import 'dart:ui';

import 'package:duanju_app/video_enhancement_policy.dart';
import 'package:duanju_app/video_enhancement_preferences.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';

/// 画质增强策略的回归测试。
///
/// 重点固定 2026-10「安卓端放弃放大表面」修复后的语义：
///   - 安卓：决策里的 output 恒等于视频显示尺寸（源尺寸），表面不被触碰；
///   - 安卓：是否上神经超分只看 targetScale（视口/源），与表面放大倍率解耦；
///   - 安卓：显存预算只跟源分辨率相关，半屏/全屏决策一致（进全屏不重新 apply）；
///   - 桌面：行为与上游一致，output 仍为放大后的尺寸。
VideoParams params({required int width, required int height, int rotate = 0}) =>
    VideoParams(
      pixelformat: 'yuv420p',
      w: width,
      h: height,
      dw: width,
      dh: height,
      par: 1.0,
      primaries: 'bt.709',
      gamma: 'bt.1886',
      rotate: rotate,
    );

const _qualityAnime = VideoEnhancementPreferences(
  mode: VideoEnhancementMode.quality,
  content: VideoEnhancementContent.animation,
);

const _power = VideoEnhancementPower(gles: 0x30001);

VideoEnhancementDecision decide({
  required VideoParams parameters,
  required Size viewport,
  required bool android,
  VideoEnhancementPreferences preferences = _qualityAnime,
  VideoEnhancementPower power = _power,
  double rate = 1.0,
  Set<VideoEnhancementBackend> rejected = const {},
  int costLimit = 4,
}) => chooseVideoEnhancement(
  preferences: preferences,
  parameters: parameters,
  viewport: viewport,
  ready: true,
  android: android,
  television: false,
  gpu: true,
  luma: true,
  hardware: false,
  animation: true,
  power: power,
  rate: rate,
  costLimit: costLimit,
  rejected: rejected,
  opaque8Bit: false,
);

void main() {
  group('安卓端表面语义（2026-10 修复）', () {
    final source = params(width: 544, height: 960);

    test('半屏：动漫档命中 anime，但 output 为源尺寸（不放大表面）', () {
      final decision = decide(
        parameters: source,
        viewport: const Size(1080, 1344),
        android: true,
      );
      expect(decision.backend, VideoEnhancementBackend.anime);
      expect(decision.output, const Size(544, 960));
    });

    test('全屏与半屏决策一致：进全屏不会重新 apply，无表面 churn', () {
      final half = decide(
        parameters: source,
        viewport: const Size(1080, 1344),
        android: true,
      );
      final full = decide(
        parameters: source,
        viewport: const Size(1080, 2400),
        android: true,
      );
      expect(full, half);
      expect(full.backend, VideoEnhancementBackend.anime);
      expect(full.output, const Size(544, 960));
    });

    test('显示端不需要放大（targetScale<=1）时走高质量缩放', () {
      final decision = decide(
        parameters: source,
        viewport: const Size(544, 960),
        android: true,
      );
      expect(decision.backend, VideoEnhancementBackend.scaling);
      expect(decision.output, const Size(544, 960));
      expect(decision.reason, '当前尺寸无需超分，使用高质量缩放');
    });

    test('1080p 竖剧全屏：无需放大，走高质量缩放', () {
      final decision = decide(
        parameters: params(width: 1080, height: 1920),
        viewport: const Size(1080, 2400),
        android: true,
      );
      expect(decision.backend, VideoEnhancementBackend.scaling);
      expect(decision.output, const Size(1080, 1920));
      expect(decision.reason, '当前尺寸无需超分，使用高质量缩放');
    });

    test('源分辨率超出安卓显存预算时不选 anime（预算只看源，与视口无关）', () {
      final big = params(width: 890, height: 1600);
      for (final viewport in const [Size(1080, 1344), Size(1080, 2400)]) {
        final decision = decide(
          parameters: big,
          viewport: viewport,
          android: true,
        );
        expect(
          decision.backend,
          VideoEnhancementBackend.scaling,
          reason: '视口 $viewport 下大源不应命中 anime',
        );
        expect(decision.output, const Size(890, 1600));
      }
    });

    test('anime 被拉黑后本集回不去（rejected 持久化语义）', () {
      final decision = decide(
        parameters: source,
        viewport: const Size(1080, 1344),
        android: true,
        rejected: const {VideoEnhancementBackend.anime},
      );
      expect(decision.backend, VideoEnhancementBackend.scaling);
    });

    test('倍速超过 kNeuralMaxRate 后失去 anime 资格', () {
      final decision = decide(
        parameters: source,
        viewport: const Size(1080, 1344),
        android: true,
        rate: 2.5,
      );
      expect(decision.backend, VideoEnhancementBackend.scaling);

      final fast = decide(
        parameters: source,
        viewport: const Size(1080, 1344),
        android: true,
        rate: 3.0,
      );
      expect(fast.backend, VideoEnhancementBackend.scaling);
      expect(fast.reason, '倍速播放，已降低增强');
    });

    test('常用倍速（1.75x）仍保留 anime', () {
      final decision = decide(
        parameters: source,
        viewport: const Size(1080, 1344),
        android: true,
        rate: 1.75,
      );
      expect(decision.backend, VideoEnhancementBackend.anime);
    });
  });

  group('桌面端行为不变', () {
    final source = params(width: 544, height: 960);

    test('output 仍为放大后的尺寸', () {
      final decision = decide(
        parameters: source,
        viewport: const Size(1080, 1344),
        android: false,
      );
      expect(decision.backend, VideoEnhancementBackend.anime);
      expect(decision.output, const Size(762, 1344));
    });

    test('桌面显存预算更宽（512MB），大源仍可选 anime', () {
      final decision = decide(
        parameters: params(width: 890, height: 1600),
        viewport: const Size(1080, 2400),
        android: false,
      );
      expect(decision.backend, VideoEnhancementBackend.anime);
      expect(decision.output, const Size(1080, 1942));
    });
  });
}
