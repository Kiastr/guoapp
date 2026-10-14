import 'video_enhancement_preferences.dart';

const playbackSpeeds = [.5, .75, 1.0, 1.25, 1.5, 2.0, 3.0];

/// 按住画面时临时使用的倍速档位（松开即回到 [PlaybackPreferences.speed]）。
const holdSpeeds = [2.0, 2.5, 3.0, 4.0, 5.0];
const defaultHoldSpeed = 3.0;

class PlaybackPreferences {
  const PlaybackPreferences({
    this.speed = 1,
    this.holdSpeed = defaultHoldSpeed,
    this.quality = 0,
    this.autoAdvance = true,
    this.danmaku = true,
    this.preload = true,
    this.enhancement = const VideoEnhancementPreferences(),
  });

  final double speed;
  final double holdSpeed;
  final int quality;
  final bool autoAdvance;
  final bool danmaku;
  final bool preload;
  final VideoEnhancementPreferences enhancement;

  PlaybackPreferences copyWith({
    double? speed,
    double? holdSpeed,
    int? quality,
    bool? autoAdvance,
    bool? danmaku,
    bool? preload,
    VideoEnhancementPreferences? enhancement,
  }) => PlaybackPreferences(
    speed: speed ?? this.speed,
    holdSpeed: holdSpeed ?? this.holdSpeed,
    quality: quality ?? this.quality,
    autoAdvance: autoAdvance ?? this.autoAdvance,
    danmaku: danmaku ?? this.danmaku,
    preload: preload ?? this.preload,
    enhancement: enhancement ?? this.enhancement,
  );

  Map<String, dynamic> toJson() => {
    'speed': speed,
    'holdSpeed': holdSpeed,
    'quality': quality,
    'autoAdvance': autoAdvance,
    'danmaku': danmaku,
    'preload': preload,
    'enhancement': enhancement.toJson(),
  };

  factory PlaybackPreferences.fromJson(Map<String, dynamic> value) {
    final speed = (value['speed'] as num? ?? 1).toDouble();
    // 旧版本没有长按倍速键（2026-10 起可设置），缺省或异常取值都回落到
    // 原来的固定 3 倍，不因此让整份播放偏好失效。
    final savedHoldSpeed = (value['holdSpeed'] as num?)?.toDouble();
    final holdSpeed = holdSpeeds.contains(savedHoldSpeed)
        ? savedHoldSpeed!
        : defaultHoldSpeed;
    final quality = value['quality'] as int? ?? 0;
    final autoAdvance = value['autoAdvance'] as bool? ?? true;
    final danmaku = value['danmaku'] as bool? ?? true;
    final preload = value['preload'] as bool? ?? true;
    if (!playbackSpeeds.contains(speed) || quality < 0 || quality > 4320) {
      throw const FormatException('播放偏好无效');
    }
    return PlaybackPreferences(
      speed: speed,
      holdSpeed: holdSpeed,
      quality: quality,
      autoAdvance: autoAdvance,
      danmaku: danmaku,
      preload: preload,
      enhancement: VideoEnhancementPreferences.fromJson(value['enhancement']),
    );
  }
}
