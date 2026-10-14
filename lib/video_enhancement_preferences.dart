enum VideoEnhancementMode {
  off('关闭'),
  economy('省电增强'),
  quality('清晰优先'),
  advanced('高级');

  const VideoEnhancementMode(this.label);
  final String label;
}

enum VideoEnhancementContent {
  automatic('跟随分类'),
  general('通用／真人'),
  animation('动漫／AI 动漫');

  const VideoEnhancementContent(this.label);
  final String label;
}

/// 高级模式（advanced）下的 Anime4K 链档位。
/// [lite] = Kazumi 轻量档平权（Restore M+S，流畅优先，+87 默认）；
/// [quality] = Kazumi 质量档平权（Restore VL + Upscale VL，效果优先，
/// 负载显著更高，可能触发性能看门狗自动降级）。
enum VideoEnhancementTier {
  lite('轻量档'),
  quality('质量档');

  const VideoEnhancementTier(this.label);
  final String label;
}

class VideoEnhancementPreferences {
  const VideoEnhancementPreferences({
    this.mode = VideoEnhancementMode.off,
    this.content = VideoEnhancementContent.automatic,
    this.tier = VideoEnhancementTier.lite,
  });

  final VideoEnhancementMode mode;
  final VideoEnhancementContent content;
  final VideoEnhancementTier tier;

  VideoEnhancementPreferences copyWith({
    VideoEnhancementMode? mode,
    VideoEnhancementContent? content,
    VideoEnhancementTier? tier,
  }) => VideoEnhancementPreferences(
    mode: mode ?? this.mode,
    content: content ?? this.content,
    tier: tier ?? this.tier,
  );

  Map<String, dynamic> toJson() => {
    'mode': mode.name,
    'content': content.name,
    'tier': tier.name,
  };

  factory VideoEnhancementPreferences.fromJson(Object? value) {
    if (value is! Map) return const VideoEnhancementPreferences();
    return VideoEnhancementPreferences(
      mode: _modeFromJson(value['mode']),
      content:
          VideoEnhancementContent.values
              .where((content) => content.name == value['content'])
              .firstOrNull ??
          VideoEnhancementContent.automatic,
      tier:
          VideoEnhancementTier.values
              .where((tier) => tier.name == value['tier'])
              .firstOrNull ??
          VideoEnhancementTier.lite,
    );
  }

  /// [结构迁移·2026-10] 旧版 mode 为扁平四档（off/automatic/economy/quality），
  /// 新版把省电增强/清晰优先收进「自动」族并新增「高级」强制档：
  ///   - 旧 'automatic'（安卓=不超分的保守自动）-> economy（省电增强），零行为变化；
  ///   - 旧 'economy'/'quality' 名称不变，直接对应；
  ///   - 新 'advanced' 为高级强制档；未知值（含旧版读新档）回落 off。
  /// [档位迁移·2026-10] tier 为 +89 新增字段：旧 JSON 无此键时回落 lite
  /// （与 +87 起的默认链一致，零行为意外）；非法值同样回落 lite。
  static VideoEnhancementMode _modeFromJson(Object? name) => switch (name) {
    'economy' => VideoEnhancementMode.economy,
    'quality' => VideoEnhancementMode.quality,
    'advanced' => VideoEnhancementMode.advanced,
    'automatic' => VideoEnhancementMode.economy,
    _ => VideoEnhancementMode.off,
  };

  @override
  bool operator ==(Object other) =>
      other is VideoEnhancementPreferences &&
      other.mode == mode &&
      other.content == content &&
      other.tier == tier;

  @override
  int get hashCode => Object.hash(mode, content, tier);
}
