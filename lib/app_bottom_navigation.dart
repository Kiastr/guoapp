import 'dart:ui';

import 'package:flutter/material.dart';

import 'cinema_tokens.dart';

/// 手机端底部浮岛导航。
///
/// 桌面与电视使用 NavigationRail / RemoteButton，不经过这里；因此 blur
/// 只覆盖一块小区域，不会给电视端整页合成增加负担。
class AppBottomNavigation extends StatelessWidget {
  const AppBottomNavigation({
    super.key,
    required this.selectedIndex,
    required this.onDestinationSelected,
    required this.destinations,
  });

  final int selectedIndex;
  final ValueChanged<int> onDestinationSelected;
  final List<NavigationDestination> destinations;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final dark = theme.brightness == Brightness.dark;
    final reduceMotion = MediaQuery.of(context).disableAnimations;
    final duration = reduceMotion ? Duration.zero : CinemaTokens.microDuration;

    return SafeArea(
      top: false,
      // [+193] 浮岛矩形按设计稿收缩：这里把底部外边距 12→20，配合内层
      // padding 7→3 让卡片同时变矮 8。两者增减量相等，slot 总高不变——
      // 顶边距屏底保持 86 不动、底边上提 8、高度从 74 收到 66，修掉
      // 「下限比设计稿低」的观感。数值含图标文字基线，非设计稿 px 绝对值。
      minimum: const EdgeInsets.fromLTRB(16, 0, 16, 20),
      child: DecoratedBox(
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(CinemaTokens.navigationRadius),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withValues(alpha: dark ? .38 : .14),
              blurRadius: 30,
              offset: const Offset(0, 12),
            ),
          ],
        ),
        child: ClipRRect(
          borderRadius: BorderRadius.circular(CinemaTokens.navigationRadius),
          child: BackdropFilter(
            filter: ImageFilter.blur(sigmaX: 22, sigmaY: 22),
            child: DecoratedBox(
              decoration: BoxDecoration(
                color: (dark ? CinemaTokens.surfaceDark : colors.surface)
                    .withValues(alpha: dark ? .86 : .94),
                border: Border.all(
                  color: Colors.white.withValues(alpha: dark ? .075 : .035),
                  width: .75,
                ),
                borderRadius: BorderRadius.circular(
                  CinemaTokens.navigationRadius,
                ),
              ),
              child: Padding(
                // [+193] 纵向内边距 7→3，浮岛卡片高度 74→66；配合外层
                // minimum bottom 12→20，slot 总高 86 不变：顶边不动、底边
                // 上提 8。图标区本身 minHeight 54 不变（theme_test 断言 ≥48）。
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
                child: Row(
                  children: [
                    for (final (index, destination) in destinations.indexed)
                      Expanded(
                        child: _Destination(
                          key: ValueKey('bottom-nav-$index'),
                          index: index,
                          destination: destination,
                          selected: index == selectedIndex,
                          onSelected: onDestinationSelected,
                          duration: duration,
                        ),
                      ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _Destination extends StatelessWidget {
  const _Destination({
    super.key,
    required this.index,
    required this.destination,
    required this.selected,
    required this.onSelected,
    required this.duration,
  });

  final int index;
  final NavigationDestination destination;
  final bool selected;
  final ValueChanged<int> onSelected;
  final Duration duration;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final foreground = selected ? colors.primary : colors.onSurfaceVariant;
    return Semantics(
      container: true,
      button: true,
      selected: selected,
      child: Tooltip(
        message: destination.label,
        excludeFromSemantics: true,
        child: InkWell(
          onTap: () => onSelected(index),
          borderRadius: BorderRadius.circular(16),
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 54, minWidth: 48),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 3, vertical: 3),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  SizedBox(
                    height: 4,
                    child: AnimatedScale(
                      scale: selected ? 1 : 0,
                      duration: duration,
                      curve: CinemaTokens.motionCurve,
                      child: DecoratedBox(
                        decoration: BoxDecoration(
                          color: colors.primary,
                          shape: BoxShape.circle,
                          boxShadow: [
                            BoxShadow(
                              color: colors.primary.withValues(alpha: .24),
                              blurRadius: 8,
                            ),
                          ],
                        ),
                        child: const SizedBox(width: 4, height: 4),
                      ),
                    ),
                  ),
                  const SizedBox(height: 4),
                  SizedBox(
                    height: 28,
                    child: Stack(
                      alignment: Alignment.center,
                      children: [
                        AnimatedContainer(
                          duration: duration,
                          curve: CinemaTokens.motionCurve,
                          width: 36,
                          height: 28,
                          decoration: BoxDecoration(
                            color:
                                selected
                                    ? colors.primary.withValues(alpha: .10)
                                    : Colors.transparent,
                            borderRadius: BorderRadius.circular(11),
                          ),
                        ),
                        IconTheme(
                          data: IconThemeData(size: 24, color: foreground),
                          child: SizedBox.square(
                            dimension: 24,
                            // 即使调用方提供了 selectedIcon 也不切换图形。
                            // 同源轮廓比 outline→filled 的跳变更稳定。
                            child: Center(child: destination.icon),
                          ),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    destination.label,
                    textAlign: TextAlign.center,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 11,
                      height: 1.25,
                      fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                      color: foreground,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
