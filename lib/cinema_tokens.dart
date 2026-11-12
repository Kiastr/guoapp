import 'package:flutter/material.dart';

/// A · 影院方案的共享视觉参数。
///
/// 这里只放**跨组件复用**的值；单个页面独有的尺寸留在该页面，避免把
/// 设计系统变成无所不包的魔法常量表。业务颜色仍应使用 ColorScheme 的
/// error / tertiary 等语义色，不要为了「统一」而把错误也染成品牌色。
abstract final class CinemaTokens {
  static const canvasDark = Color(0xFF0B0B0D);
  static const surfaceDark = Color(0xFF17171B);
  static const accentDark = Color(0xFFFF6B57);
  static const onAccentDark = Color(0xFF2A0B06);
  static const textDark = Color(0xFFF5F3F0);
  static const mutedDark = Color(0xFF93909C);

  static const pageGutter = 18.0;
  static const gridSpacing = 12.0;
  static const posterRadius = 14.0;
  static const panelRadius = 18.0;
  static const navigationRadius = 24.0;

  static const microDuration = Duration(milliseconds: 160);
  static const controlsDuration = Duration(milliseconds: 180);
  static const motionCurve = Curves.easeOutCubic;

  /// 播放器控制层遮罩。中间完全透明，顶部和底部使用不同停点，
  /// 避免一整层黑色把画面压灰。控件自身仍用白色前景，不跟随浅色主题。
  static const playerScrim = LinearGradient(
    begin: Alignment.topCenter,
    end: Alignment.bottomCenter,
    colors: [
      Color(0xB3000000),
      Color(0x00000000),
      Color(0x00000000),
      Color(0xE6000000),
    ],
    stops: [0, .25, .53, 1],
  );

  /// 封面文字遮罩：只在下半部渐深，保留封面主体色彩。
  static const coverScrim = LinearGradient(
    begin: Alignment(0, .05),
    end: Alignment.bottomCenter,
    colors: [Colors.transparent, Color(0xC7000000)],
  );
}
