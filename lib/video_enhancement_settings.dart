import 'package:flutter/material.dart';

import 'remote_widgets.dart';
import 'video_enhancement.dart';
import 'video_enhancement_preferences.dart';

class VideoEnhancementSettings extends StatelessWidget {
  const VideoEnhancementSettings({
    super.key,
    required this.controller,
    required this.onChanged,
    required this.onCompare,
    this.television = false,
    this.busy = false,
  });

  final VideoEnhancementController controller;
  final ValueChanged<VideoEnhancementPreferences> onChanged;
  final VoidCallback onCompare;
  final bool television;
  final bool busy;

  Widget _choice({
    required String id,
    required String label,
    required bool selected,
    required VoidCallback action,
    bool enabled = true,
  }) => television
      ? RemoteButton(
          key: ValueKey(id),
          label: label,
          selected: selected,
          onPressed: busy || !enabled ? null : action,
        )
      : ChoiceChip(
          key: ValueKey(id),
          label: Text(label),
          selected: selected,
          onSelected: busy || !enabled ? null : (_) => action(),
        );

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller,
    builder: (context, _) {
      final preferences = controller.preferences;
      final colors = Theme.of(context).colorScheme;
      final helperStyle = TextStyle(
        fontSize: 13,
        color: colors.onSurfaceVariant,
        height: 1.5,
      );
      final mode = preferences.mode;
      final enabled = mode != VideoEnhancementMode.off;
      // [结构重构·2026-10] 顶层三档：关闭 / 自动（族：省电增强+清晰优先）/ 高级。
      // 「自动」是族标记，本身不对应枚举值；子档由下方「画质增强模式」chips 选择。
      final autoFamily =
          mode == VideoEnhancementMode.economy ||
          mode == VideoEnhancementMode.quality;
      final supported = controller.supported;
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '画质增强',
            style: television ? const TextStyle(fontSize: 18) : null,
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _choice(
                id: 'enhancement-mode-off',
                label: '关闭',
                selected: mode == VideoEnhancementMode.off,
                action: () => onChanged(
                  preferences.copyWith(mode: VideoEnhancementMode.off),
                ),
              ),
              _choice(
                id: 'enhancement-mode-auto',
                label: '自动',
                selected: autoFamily,
                enabled: supported,
                // 已在自动族内则保持当前子档；从关闭/高级切入时默认落省电增强
                // （与旧 automatic 用户的迁移默认一致，零行为意外）。
                action: () => onChanged(
                  preferences.copyWith(
                    mode: autoFamily ? mode : VideoEnhancementMode.economy,
                  ),
                ),
              ),
              _choice(
                id: 'enhancement-mode-advanced',
                label: '高级',
                selected: mode == VideoEnhancementMode.advanced,
                enabled: supported,
                action: () => onChanged(
                  preferences.copyWith(mode: VideoEnhancementMode.advanced),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(_modeHelper(mode), style: helperStyle),
          if (autoFamily) ...[
            const SizedBox(height: 16),
            const Text('画质增强模式'),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final level in const [
                  VideoEnhancementMode.economy,
                  VideoEnhancementMode.quality,
                ])
                  _choice(
                    id: 'enhancement-level-${level.name}',
                    label: level.label,
                    selected: mode == level,
                    enabled: supported,
                    action: () => onChanged(preferences.copyWith(mode: level)),
                  ),
              ],
            ),
            const SizedBox(height: 16),
            const Text('画面类型'),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final content in VideoEnhancementContent.values)
                  _choice(
                    id: 'enhancement-content-${content.name}',
                    label: content.label,
                    selected: preferences.content == content,
                    enabled: supported,
                    action: () =>
                        onChanged(preferences.copyWith(content: content)),
                  ),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              preferences.content == VideoEnhancementContent.automatic
                  ? '${controller.contentLabel}；分类不准确时可手动切换。'
                  : '动漫档适用于动画和 AI 动漫；写实 AI 画面建议使用通用档。',
              style: helperStyle,
            ),
          ],
          if (enabled) ...[
            const SizedBox(height: 12),
            Text(controller.status, key: const ValueKey('enhancement-status')),
            if (controller.detail.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(controller.detail, style: helperStyle),
            ],
            const SizedBox(height: 8),
            if (television)
              RemoteButton(
                key: const ValueKey('enhancement-compare'),
                label: controller.comparing ? '恢复增强' : '原画对比',
                icon: Icons.compare_rounded,
                onPressed: controller.canCompare && !busy ? onCompare : null,
              )
            else
              OutlinedButton.icon(
                key: const ValueKey('enhancement-compare'),
                onPressed: controller.canCompare && !busy ? onCompare : null,
                icon: const Icon(Icons.compare_rounded),
                label: Text(controller.comparing ? '恢复增强' : '原画对比'),
              ),
          ] else if (!supported) ...[
            const SizedBox(height: 12),
            Text(controller.status, key: const ValueKey('enhancement-status')),
          ],
          const SizedBox(height: 20),
        ],
      );
    },
  );

  /// 顶层模式 chips 下方的说明文案。
  String _modeHelper(VideoEnhancementMode mode) => switch (mode) {
    VideoEnhancementMode.off => '默认关闭，不改变视频输出；开启后再配置画面类型和原画对比。',
    VideoEnhancementMode.economy || VideoEnhancementMode.quality =>
      '自动按画面和设备选择增强；省电使用轻量缩放；清晰优先允许更高负载。建议先选择源站高清画质。',
    VideoEnhancementMode.advanced =>
      '高级模式直接强制 Anime4K 神经增强链（Kazumi 同款：安卓端为同分辨率重建/降噪，桌面端为 2× 超分），绕过全部自动判断（动画标签、温控、倍速、显存预算均不拦截）；渲染异常时仍会自动回退原画。',
  };
}
