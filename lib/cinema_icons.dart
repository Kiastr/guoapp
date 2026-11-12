import 'package:flutter/material.dart';

/// 底部导航的同源图标。
///
/// 四个符号共用 24×24 画布、1.75px 线宽和圆端点。选中时**保留同一路径**，
/// 只由外层改变颜色与状态圆点；不要混用 Material outline / filled 两套
/// 字形，否则会出现视觉面积忽大忽小、基线漂移的问题。
enum CinemaSymbol { discover, follow, history, download }

class CinemaIcon extends StatelessWidget {
  const CinemaIcon(this.symbol, {super.key, this.color, this.size = 24});

  final CinemaSymbol symbol;
  final Color? color;
  final double size;

  @override
  Widget build(BuildContext context) {
    final foreground =
        color ?? IconTheme.of(context).color ?? const Color(0xFFF5F3F0);
    return SizedBox.square(
      dimension: size,
      child: CustomPaint(
        painter: CinemaIconPainter(symbol: symbol, color: foreground),
      ),
    );
  }
}

@visibleForTesting
class CinemaIconPainter extends CustomPainter {
  const CinemaIconPainter({required this.symbol, required this.color});

  final CinemaSymbol symbol;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    // 所有路径以 24 为设计单位；非 24px 的调用仍按同一比例缩放。
    canvas.save();
    canvas.scale(size.width / 24, size.height / 24);
    final paint =
        Paint()
          ..color = color
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.75
          ..strokeCap = StrokeCap.round
          ..strokeJoin = StrokeJoin.round
          ..isAntiAlias = true;

    switch (symbol) {
      case CinemaSymbol.discover:
        canvas.drawCircle(const Offset(12, 12), 8.25, paint);
        final needle =
            Path()
              ..moveTo(15.55, 8.45)
              ..lineTo(13.7, 13.7)
              ..lineTo(8.45, 15.55)
              ..lineTo(10.3, 10.3)
              ..close();
        canvas.drawPath(needle, paint);
      case CinemaSymbol.follow:
        // 书签故意做到 14.5px 宽：若机械套用 16.5px 圆的同宽边界，
        // 细长的纵向轮廓在其他三个图标旁会显得明显偏小。
        final bookmark =
            Path()
              ..moveTo(5.5, 4.25)
              ..lineTo(18.5, 4.25)
              ..quadraticBezierTo(19.25, 4.25, 19.25, 5)
              ..lineTo(19.25, 19.6)
              ..quadraticBezierTo(19.25, 20.08, 18.63, 19.93)
              ..lineTo(12, 15.6)
              ..lineTo(5.37, 19.93)
              ..quadraticBezierTo(4.75, 20.08, 4.75, 19.6)
              ..lineTo(4.75, 5)
              ..quadraticBezierTo(4.75, 4.25, 5.5, 4.25)
              ..close();
        canvas.drawPath(bookmark, paint);
      case CinemaSymbol.history:
        canvas.drawCircle(const Offset(12, 12), 8.25, paint);
        final hands =
            Path()
              ..moveTo(12, 7.25)
              ..lineTo(12, 12.35)
              ..lineTo(15.4, 14.4);
        canvas.drawPath(hands, paint);
      case CinemaSymbol.download:
        final arrow =
            Path()
              ..moveTo(12, 4.25)
              ..lineTo(12, 14.35)
              ..moveTo(8, 10.5)
              ..lineTo(12, 14.5)
              ..lineTo(16, 10.5);
        final tray =
            Path()
              ..moveTo(4.75, 15.25)
              ..lineTo(4.75, 18.5)
              ..quadraticBezierTo(4.75, 19.75, 6, 19.75)
              ..lineTo(18, 19.75)
              ..quadraticBezierTo(19.25, 19.75, 19.25, 18.5)
              ..lineTo(19.25, 15.25);
        canvas
          ..drawPath(arrow, paint)
          ..drawPath(tray, paint);
    }
    canvas.restore();
  }

  @override
  bool shouldRepaint(CinemaIconPainter oldDelegate) =>
      symbol != oldDelegate.symbol || color != oldDelegate.color;
}
