import 'package:flutter/material.dart';

import 'cinema_tokens.dart';
import 'models.dart';

// A · 影院首页的「区块标题行 / 文字链接 / 继续观看横卡」。
//
// 【为什么单独一个文件】+190 把首页的信息架构对齐设计稿：品牌头、常驻搜索、
// 单行分类、继续观看横卡、「为你推荐 / 换一批」标题行。前两块分别在
// home_screen（目录页）和 recommendations_screen（红果推荐页）里渲染，但
// 标题行与横卡是同一套视觉，抽成共享组件可以避免两处各写一份数值。
//
// 【设计系统边界】这里只复用 CinemaTokens 的间距 / 圆角 / 强调色；文字颜色
// 一律走 ColorScheme 语义色（onSurfaceVariant / primary），浅色主题下也能读。
// 业务语义（错误色、VIP 色等）不在这里定义。
//
// 【测试约束】继续观看横卡刻意**不使用 DramaCover / DramaTile**：设计稿的
// wide-art 本来就是渐变色块，且 interface_test 会用 find.byType(DramaCover)
// 跨 tab 数封面，若横卡里塞真实封面会把计数从 3 变成 5，断言会挂。
// 同理横卡不发起任何网络请求，纯本地绘制。

/// 区块标题行：左侧标题，右侧一组文字超链接按钮。
///
/// 对齐设计稿 `.section`（标题 15px/700，右侧动作 10px 次要色）。
/// [actions] 用 Wrap 包住，窄屏 / 大字体下按钮会自动换行而不是溢出 Row。
class CinemaSectionHeader extends StatelessWidget {
  const CinemaSectionHeader({
    super.key,
    required this.title,
    this.actions = const <Widget>[],
  });

  final String title;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      // [+192] 对齐定稿 `.section { margin:24px 0 12px }`：标题行上下留白
      // 逐段累积是最明显的顶部错位来源，这里按定稿值收敛。
      padding: const EdgeInsets.fromLTRB(
        CinemaTokens.pageGutter,
        24,
        CinemaTokens.pageGutter,
        12,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Expanded(
            child: Text(
              title,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.titleMedium?.copyWith(
                fontSize: 15,
                fontWeight: FontWeight.w700,
                letterSpacing: -.3,
              ),
            ),
          ),
          if (actions.isNotEmpty)
            Wrap(
              spacing: 12,
              runSpacing: 4,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: actions,
            ),
        ],
      ),
    );
  }
}

/// 文字超链接形式的轻量按钮（设计稿 `.section span` / 「换一批 ↻」）。
///
/// 刻意不用 TextButton：Material 3 的 TextButton 有默认 minWidth 64 / 内边距，
/// 三五个挤在标题行右侧会显得像一排实心按钮。这里无背景、无边框，只有文字 +
/// 可选尾图标，InkWell 提供极轻的点击反馈。
///
/// [onTap] 为 null 时表示禁用（例如正在刷新），文字会跟着变灰。
/// [tooltip] 为空字符串时不包 Tooltip——档位按钮外层已经是 PopupMenuButton，
/// 它自带 tooltip，嵌套两层 Tooltip 会抢手势。
///
/// [passthrough] 为真时不包 InkWell，只保留文字外观，让外层手势（如
/// PopupMenuButton）接管点击。档位按钮与「换一批」共用同一套视觉，但前者
/// 由菜单触发、后者是普通点击，故需要这个开关。
class CinemaTextLink extends StatelessWidget {
  const CinemaTextLink({
    super.key,
    required this.label,
    this.icon,
    this.tooltip,
    this.onTap,
    this.emphasis = false,
    this.passthrough = false,
  });

  final String label;
  final IconData? icon;
  final String? tooltip;
  final VoidCallback? onTap;
  final bool emphasis;
  final bool passthrough;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    // passthrough 时点击由外层（如 PopupMenuButton）接管，这里只负责外观，
    // 不能因为没传 onTap 就把档位按钮显示成禁用灰。
    final enabled = passthrough || onTap != null;
    final color = !enabled
        ? scheme.onSurfaceVariant.withValues(alpha: .45)
        : emphasis
        ? scheme.primary
        : scheme.onSurfaceVariant;
    final text = Text(
      label,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      style: TextStyle(
        fontSize: 11,
        height: 1.2,
        color: color,
        fontWeight: emphasis ? FontWeight.w600 : FontWeight.w500,
      ),
    );
    final body = Padding(
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 4),
      // 26px 的最小高度保证标题行有基本触控区，又不会把首屏撑高。
      child: ConstrainedBox(
        constraints: const BoxConstraints(minHeight: 26),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Flexible(child: text),
            if (icon != null) ...[
              const SizedBox(width: 2),
              Icon(icon, size: 13, color: color),
            ],
          ],
        ),
      ),
    );
    // passthrough 时不要 InkWell：它会抢走 PopupMenuButton 的点击手势。
    final link = passthrough
        ? body
        : InkWell(
            onTap: onTap,
            borderRadius: BorderRadius.circular(8),
            child: body,
          );
    final message = tooltip ?? label;
    return message.isEmpty ? link : Tooltip(message: message, child: link);
  }
}

/// 「继续观看」横卡区：标题行 + 横向滚动的 16:10 卡片。
///
/// [entries] 由调用方筛好（未看完、按更新时间倒序），组件本身不碰 LocalStore，
/// 保持纯展示；这样 widget test 里可以直接喂假数据。列数由可用宽度决定
/// （见下方 LayoutBuilder），条目本身不截断，多出的可横向滑动。
/// 无记录时返回零尺寸，不占首屏一行空白。
class ContinueWatchingStrip extends StatelessWidget {
  const ContinueWatchingStrip({
    super.key,
    required this.entries,
    required this.onOpen,
    required this.onViewAll,
  });

  final List<WatchEntry> entries;
  final ValueChanged<Drama> onOpen;
  final VoidCallback onViewAll;

  @override
  Widget build(BuildContext context) {
    if (entries.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        CinemaSectionHeader(
          title: '继续观看',
          actions: [
            CinemaTextLink(
              label: '全部',
              icon: Icons.chevron_right_rounded,
              tooltip: '查看全部观看记录',
              onTap: onViewAll,
            ),
          ],
        ),
        // [+195] 横卡数量与宽度按可用宽度算，不再固定 2 条 / 174 宽。
        // 之前竖屏 2 条还能凑满一行，横屏平板上右侧会空出一大片；现在竖屏
        // 仍是 2 条但铺满，平板横屏约 5 条、超宽屏封顶 8 条，卡片等宽均分。
        // [+196] 卡片**等比缩放**：宽度变了高度按 174×106 的比例跟着变，
        // 不做「宽度拉长、高度不动」的拉伸（那样横屏会变成又扁又长的一条）。
        LayoutBuilder(
          builder: (context, constraints) {
            final available =
                constraints.maxWidth - 2 * CinemaTokens.pageGutter;
            // 目标卡片宽 174 + 间距 12（设计稿 `wide-art` 尺寸）。
            final target = ((available + 12) / (174 + 12)).floor().clamp(2, 8);
            // 宽度按**目标列数**均分而不是实际条数：只有 2 条历史时也保持
            // 正常卡片宽度左对齐，不会把两张卡拉成超宽色块。上限 200 是
            // 防止电视/超宽屏把卡片放得比设计稿大太多。
            final cardWidth = ((available - 12 * (target - 1)) / target).clamp(
              140.0,
              200.0,
            );
            // [+197] 不截断到目标列数：多于一屏的记录全部渲染，第一屏末尾会
            // 自然露出下一张的边缘，作为「还能往右滑」的提示；少于一屏时按
            // 实际条数左对齐，不拉伸、不补位。
            return SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(
                horizontal: CinemaTokens.pageGutter,
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  for (final entry in entries)
                    Padding(
                      padding: const EdgeInsets.only(right: 12),
                      child: _ContinueCard(
                        entry: entry,
                        width: cardWidth,
                        onTap: () => onOpen(entry.drama),
                      ),
                    ),
                ],
              ),
            );
          },
        ),
      ],
    );
  }
}

class _ContinueCard extends StatelessWidget {
  const _ContinueCard({
    required this.entry,
    required this.width,
    required this.onTap,
  });

  final WatchEntry entry;

  /// [+195] 由 [ContinueWatchingStrip] 按可用宽度均分传入（原来固定 174）。
  final double width;

  final VoidCallback onTap;

  // [+193] 对齐设计稿 wide-art 的 arts 六组三色渐变（145deg / 中段 55%）。
  // 原来用的是「深棕→近黑」双色暗渐变，叠上底部 70% 黑遮罩后整块几乎
  // 发黑，实机看起来像没有图案的纯色板；换成设计稿的亮色三色渐变后，
  // 上半部能明显看出色彩层次。按剧 id 稳定取色，同一部剧每次颜色一致；
  // 不使用真实封面，理由见文件头注释（interface_test 跨 tab 数封面）。
  static const _arts = <List<Color>>[
    [Color(0xFF2D4059), Color(0xFF708090), Color(0xFFB5835A)],
    [Color(0xFF361522), Color(0xFF914F53), Color(0xFFD4A373)],
    [Color(0xFF243B35), Color(0xFF52685B), Color(0xFFB08968)],
    [Color(0xFF22223B), Color(0xFF4A4E69), Color(0xFF9A8C98)],
    [Color(0xFF432818), Color(0xFF99582A), Color(0xFFBB9457)],
    [Color(0xFF293241), Color(0xFF3D5A80), Color(0xFF98C1D9)],
  ];

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = _arts[entry.drama.id.hashCode.abs() % _arts.length];
    // duration 为 0 / 异常值时退回 0 进度，不显示 NaN 宽度。
    final progress = entry.duration > 1
        ? (entry.position / entry.duration).clamp(0.0, 1.0)
        : 0.0;
    final rounded = BorderRadius.circular(CinemaTokens.posterRadius);
    // [+196] 海报高度随宽度等比（设计稿 174×106）。宽度变化时比例不变，
    // 不会出现「宽度拉长、高度固定 106」的横向拉伸。
    final artHeight = width * 106 / 174;
    return SizedBox(
      width: width,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Material(
            color: Colors.transparent,
            borderRadius: rounded,
            clipBehavior: Clip.antiAlias,
            child: InkWell(
              onTap: onTap,
              child: SizedBox(
                height: artHeight,
                child: Stack(
                  children: [
                    Positioned.fill(
                      child: DecoratedBox(
                        decoration: BoxDecoration(
                          gradient: LinearGradient(
                            begin: Alignment.topLeft,
                            end: Alignment.bottomRight,
                            colors: colors,
                            // 中段色落在 55%，和设计稿 145deg 渐变一致。
                            stops: const [0, .55, 1],
                          ),
                        ),
                      ),
                    ),
                    // 右上角柔光 + 底部渐深，保证白色文字在任何一组底色上可读。
                    Positioned.fill(
                      child: DecoratedBox(
                        decoration: BoxDecoration(
                          gradient: RadialGradient(
                            center: const Alignment(.65, -.5),
                            radius: 1.1,
                            colors: [
                              Colors.white.withValues(alpha: .16),
                              Colors.transparent,
                            ],
                          ),
                        ),
                      ),
                    ),
                    Positioned.fill(
                      child: DecoratedBox(
                        decoration: const BoxDecoration(
                          gradient: LinearGradient(
                            begin: Alignment.topCenter,
                            end: Alignment.bottomCenter,
                            colors: [Colors.transparent, Color(0xB3000000)],
                            stops: [.35, 1],
                          ),
                        ),
                      ),
                    ),
                    Positioned(
                      left: 12,
                      bottom: 12,
                      child: Container(
                        width: 29,
                        height: 29,
                        decoration: const BoxDecoration(
                          color: CinemaTokens.accentDark,
                          shape: BoxShape.circle,
                        ),
                        child: const Icon(
                          Icons.play_arrow_rounded,
                          size: 17,
                          color: CinemaTokens.onAccentDark,
                        ),
                      ),
                    ),
                    Positioned(
                      right: 12,
                      bottom: 17,
                      child: Text(
                        '第 ${entry.episode} 集',
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 11,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    // 3px 进度条贴底，不用 LinearProgressIndicator（它最小高度
                    // 4px 且带自己的轨道色，塞进 3px 会被裁）。
                    Positioned(
                      left: 0,
                      right: 0,
                      bottom: 0,
                      child: Container(
                        height: 3,
                        color: Colors.white.withValues(alpha: .22),
                        alignment: Alignment.centerLeft,
                        child: FractionallySizedBox(
                          widthFactor: progress,
                          heightFactor: 1,
                          child: const ColoredBox(
                            color: CinemaTokens.accentDark,
                          ),
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(height: 8),
          // [+192] 对齐定稿 `.card-title`（11px/500）与 `.card-meta`（9px）；
          // 之前走 bodySmall 的 12px/600 让卡片标题和区块标题抢视觉重量，
          // 也是拼图里实机字号整体偏大的来源之一。
          Text(
            entry.drama.title,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: theme.textTheme.bodySmall?.copyWith(
              fontSize: 11,
              fontWeight: FontWeight.w500,
            ),
          ),
          const SizedBox(height: 2),
          Text(
            '看到 ${(progress * 100).round()}%',
            style: theme.textTheme.bodySmall?.copyWith(
              fontSize: 9,
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}
