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

class VideoEnhancementPreferences {
  const VideoEnhancementPreferences({
    this.mode = VideoEnhancementMode.off,
    this.content = VideoEnhancementContent.automatic,
  });

  final VideoEnhancementMode mode;
  final VideoEnhancementContent content;

  VideoEnhancementPreferences copyWith({
    VideoEnhancementMode? mode,
    VideoEnhancementContent? content,
  }) => VideoEnhancementPreferences(
    mode: mode ?? this.mode,
    content: content ?? this.content,
  );

  Map<String, dynamic> toJson() => {'mode': mode.name, 'content': content.name};

  factory VideoEnhancementPreferences.fromJson(Object? value) {
    if (value is! Map) return const VideoEnhancementPreferences();
    return VideoEnhancementPreferences(
      mode: _modeFromJson(value['mode']),
      content:
          VideoEnhancementContent.values
              .where((content) => content.name == value['content'])
              .firstOrNull ??
          VideoEnhancementContent.automatic,
    );
  }

  /// [结构迁移·2026-10] 旧版 mode 为扁平四档（off/automatic/economy/quality），
  /// 新版把省电增强/清晰优先收进「自动」族并新增「高级」强制档：
  ///   - 旧 'automatic'（安卓=不超分的保守自动）-> economy（省电增强），零行为变化；
  ///   - 旧 'economy'/'quality' 名称不变，直接对应；
  ///   - 新 'advanced' 为高级强制档；未知值（含旧版读新档）回落 off。
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
      other.content == content;

  @override
  int get hashCode => Object.hash(mode, content);
}
