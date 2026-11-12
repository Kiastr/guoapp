import 'package:flutter/material.dart';

import 'local_store.dart';
import 'playback_preferences.dart';
import 'video_enhancement_preferences.dart';
import 'widgets.dart';

class PlaybackSettingsScreen extends StatelessWidget {
  const PlaybackSettingsScreen({super.key, required this.store});

  final LocalStore store;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: store,
    builder: (context, _) {
      final preferences = store.playbackPreferences;
      final enhancement = preferences.enhancement;

      void save(PlaybackPreferences next) =>
          saveUserChange(context, () => store.setPlaybackPreferences(next));

      Widget chips<T>({
        required List<T> values,
        required T selected,
        required String Function(T) label,
        required ValueChanged<T> onSelected,
      }) => Wrap(
        spacing: 8,
        runSpacing: 4,
        children: [
          for (final value in values)
            ChoiceChip(
              label: Text(label(value)),
              selected: value == selected,
              onSelected: (_) => onSelected(value),
            ),
        ],
      );

      Widget heading(String text) => Padding(
        padding: const EdgeInsets.fromLTRB(0, 16, 0, 8),
        child: Text(text, style: Theme.of(context).textTheme.titleMedium),
      );

      return Scaffold(
        appBar: AppBar(title: const Text('播放偏好')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 680),
            child: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                const Text('这里改的是默认值；播放时点底部「播放设置」改的是同一份数据。'),
                heading('播放速度'),
                ListTile(
                  title: const Text('默认倍速'),
                  subtitle: const Text('播放中可随时用底部工具行切换。'),
                ),
                chips<double>(
                  values: playbackSpeeds,
                  selected: preferences.speed,
                  label: (value) => '${value}x',
                  onSelected:
                      (value) => save(preferences.copyWith(speed: value)),
                ),
                const SizedBox(height: 12),
                ListTile(
                  title: const Text('长按倍速'),
                  subtitle: const Text('按住画面临时加速，松开自动恢复。'),
                ),
                chips<double>(
                  values: holdSpeeds,
                  selected: preferences.holdSpeed,
                  label: (value) => '${value}x',
                  onSelected:
                      (value) => save(preferences.copyWith(holdSpeed: value)),
                ),
                heading('清晰度'),
                chips<int>(
                  values: const [0, 1080, 720, 480],
                  selected: preferences.quality,
                  label: (value) => value == 0 ? '自动 · 优先高清' : '${value}P',
                  onSelected:
                      (value) => save(preferences.copyWith(quality: value)),
                ),
                heading('播放行为'),
                SwitchListTile(
                  value: preferences.danmaku,
                  title: const Text('显示弹幕'),
                  subtitle: const Text('仅红果源提供弹幕；播放中可用右侧「弹」按钮切换。'),
                  onChanged:
                      (value) => save(preferences.copyWith(danmaku: value)),
                ),
                SwitchListTile(
                  value: preferences.autoAdvance,
                  title: const Text('自动连播'),
                  subtitle: const Text('关闭后，播放结束停在当前集。'),
                  onChanged:
                      (value) => save(preferences.copyWith(autoAdvance: value)),
                ),
                SwitchListTile(
                  value: preferences.preload,
                  title: const Text('下一集预加载'),
                  subtitle: const Text('提前准备下一集播放地址，切集更快但会多一次站源请求。'),
                  onChanged:
                      (value) => save(preferences.copyWith(preload: value)),
                ),
                heading('画质增强'),
                chips<VideoEnhancementMode>(
                  values: VideoEnhancementMode.values,
                  selected: enhancement.mode,
                  label: (value) => value.label,
                  onSelected: (value) => save(
                    preferences.copyWith(
                      enhancement: enhancement.copyWith(mode: value),
                    ),
                  ),
                ),
                if (enhancement.mode != VideoEnhancementMode.off) ...[
                  const SizedBox(height: 8),
                  const Text('画面类型'),
                  chips<VideoEnhancementContent>(
                    values: VideoEnhancementContent.values,
                    selected: enhancement.content,
                    label: (value) => value.label,
                    onSelected: (value) => save(
                      preferences.copyWith(
                        enhancement: enhancement.copyWith(content: value),
                      ),
                    ),
                  ),
                ],
                if (enhancement.mode == VideoEnhancementMode.advanced) ...[
                  const SizedBox(height: 8),
                  const Text('增强档位（按算力从低到高）'),
                  chips<VideoEnhancementTier>(
                    values: VideoEnhancementTier.values,
                    selected: enhancement.tier,
                    label: (value) => value.label,
                    onSelected: (value) => save(
                      preferences.copyWith(
                        enhancement: enhancement.copyWith(tier: value),
                      ),
                    ),
                  ),
                ],
                const Padding(
                  padding: EdgeInsets.fromLTRB(0, 20, 0, 8),
                  child: Text(
                    '画质增强会按设备能力自动降级；低端设备即使选了高档位也可能回落到原画。'
                    '偏好保存在当前用户中。',
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    },
  );
}
