import 'package:flutter/material.dart';

import 'follow_state.dart';
import 'local_store.dart';
import 'models.dart';
import 'widgets.dart';

Future<void> showDramaActions(
  BuildContext context, {
  required Drama drama,
  required LocalStore store,
  VoidCallback? onContinue,
  VoidCallback? onDownload,
  VoidCallback? onSelect,
  bool history = false,
}) async {
  final epoch = store.profileEpoch;
  final following = store.following(drama.id);
  final favoriteSource = store.isFavoriteSource(drama.source);
  final choices = <(String, String, IconData)>[
    if (onContinue != null && store.watched(drama.id) != null)
      ('continue', '继续观看', Icons.play_arrow_rounded),
    ('favorite', following == null ? '加入追剧' : '取消追剧', Icons.bookmark_outline),
    for (final status in FollowStatus.values)
      (
        'status:${status.name}',
        '标记${status.label}',
        switch (status) {
          FollowStatus.planned => Icons.bookmark_add_outlined,
          FollowStatus.watching => Icons.play_circle_outline,
          FollowStatus.watched => Icons.check_circle_outline,
        },
      ),
    if (following != null && following.hasUpdates)
      ('read', '标记 ${following.updateLabel}已读', Icons.mark_email_read_outlined),
    if (onDownload != null && store.canDownload)
      ('download', '下载选集', Icons.download_outlined),
    if (onSelect != null && store.canDownload)
      ('select', '多选下载', Icons.checklist_rounded),
    // 源级操作：收藏的是「这部剧所在的站源」，不是这部剧本身。
    // 与详情页片源行、播放页简介里的星共用 LocalStore.toggleFavoriteSource：
    // 收藏后首页把该源从所属大组拆出、独占一组（+155 起内置源同样可收藏），
    // 取消则只剩大组入口。状态用实心/空心星区分，与另外两处入口一致。
    (
      'favoriteSource',
      favoriteSource ? '取消收藏此站源' : '收藏此站源',
      favoriteSource ? Icons.star_rounded : Icons.star_border_rounded,
    ),
    if (history && store.watched(drama.id) != null)
      ('removeHistory', '删除这条观看记录', Icons.history_toggle_off),
  ];
  final choice = await showDialog<String>(
    context: context,
    builder: (context) => SimpleDialog(
      title: Text(drama.title, maxLines: 2, overflow: TextOverflow.ellipsis),
      children: [
        for (final entry in choices.indexed)
          SimpleDialogOption(
            onPressed: () => Navigator.pop(context, entry.$2.$1),
            child: Row(
              children: [
                Icon(entry.$2.$3, size: 22),
                const SizedBox(width: 14),
                Expanded(child: Text(entry.$2.$2)),
                if (entry.$2.$1 == 'status:${following?.status.name}')
                  const Icon(Icons.check_rounded, size: 20),
              ],
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(24, 10, 24, 8),
          child: Text(
            '手动标记已看会保留真实播放进度。',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
      ],
    ),
  );
  if (choice == null ||
      !context.mounted ||
      epoch != store.profileEpoch ||
      !store.allowsSource(drama.source)) {
    return;
  }
  if (choice.startsWith('status:')) {
    final status = FollowStatus.values.firstWhere(
      (value) => choice == 'status:${value.name}',
    );
    await saveUserChange(context, () => store.setFollowStatus(drama, status));
    return;
  }
  switch (choice) {
    case 'continue':
      onContinue?.call();
    case 'favorite':
      await saveUserChange(context, () => store.toggleFavorite(drama));
    case 'read':
      await saveUserChange(context, () => store.markUpdatesRead(drama.id));
    case 'download':
      if (store.canDownload) onDownload?.call();
    case 'select':
      if (store.canDownload) onSelect?.call();
    case 'favoriteSource':
      // 与详情页 / 播放页同一套提示语，三处入口的表现保持一致。
      final sourceName = SourceSite.byId(drama.source).name;
      await saveUserChange(
        context,
        () => store.toggleFavoriteSource(drama.source),
      );
      if (context.mounted) {
        final favorite = store.isFavoriteSource(drama.source);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(
              favorite ? '已收藏「$sourceName」，首页将独立显示该片源' : '已取消收藏「$sourceName」',
            ),
          ),
        );
      }
    case 'removeHistory':
      await saveUserChange(context, () => store.removeHistory(drama.id));
  }
}

Future<String?> showBatchDramaActions(
  BuildContext context, {
  required int count,
}) async {
  final choices = <(String, String, IconData)>[
    ('favoriteAdd', '批量加入追剧', Icons.bookmark_add_outlined),
    ('favoriteRemove', '批量取消追剧', Icons.bookmark_remove_outlined),
    for (final status in FollowStatus.values)
      (
        'status:${status.name}',
        '标记${status.label}',
        switch (status) {
          FollowStatus.planned => Icons.bookmark_add_outlined,
          FollowStatus.watching => Icons.play_circle_outline,
          FollowStatus.watched => Icons.check_circle_outline,
        },
      ),
    ('read', '标记更新已读', Icons.mark_email_read_outlined),
    ('favoriteSourceAdd', '收藏涉及的站源', Icons.star_border_rounded),
    ('favoriteSourceRemove', '取消收藏涉及的站源', Icons.star_rounded),
    ('removeHistory', '删除观看记录', Icons.history_toggle_off),
  ];
  return showDialog<String>(
    context: context,
    builder: (context) => SimpleDialog(
      title: Text('批量操作 · 已选 $count 部'),
      children: [
        for (final entry in choices)
          SimpleDialogOption(
            onPressed: () => Navigator.pop(context, entry.$1),
            child: Row(
              children: [
                Icon(entry.$3, size: 22),
                const SizedBox(width: 14),
                Expanded(child: Text(entry.$2)),
              ],
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(24, 10, 24, 8),
          child: Text(
            '只对已选中且当前用户可以访问的短剧生效；每条操作会跳过状态已经符合的短剧。'
            '「继续观看」和「下载选集」仍是单部动作，不在批量菜单里。',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
      ],
    ),
  );
}

class DramaActionButton extends StatelessWidget {
  const DramaActionButton({
    super.key,
    required this.drama,
    required this.onPressed,
  });
  final Drama drama;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) => IconButton.filledTonal(
    key: ValueKey('drama-actions-${drama.id}'),
    tooltip: '${drama.title} · 更多操作',
    onPressed: onPressed,
    icon: const Icon(Icons.more_horiz_rounded, size: 20),
    style: IconButton.styleFrom(
      backgroundColor: Colors.black.withValues(alpha: .64),
      foregroundColor: Colors.white,
      minimumSize: const Size(40, 40),
      padding: const EdgeInsets.all(8),
      visualDensity: VisualDensity.compact,
    ),
  );
}
