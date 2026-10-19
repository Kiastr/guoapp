import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:duanju_app/models.dart';
import 'package:duanju_app/playback_preferences.dart';
import 'package:duanju_app/player_menu.dart';

// 回归背景：LibreTV 采集源的详情里，同一集来自多个播放源（「第N集」与
// 「第N集·源名」并列），但播放器「清晰度」只有一个「自动」选项，看起来是空的。
// 现在：没有真实画质档位时隐藏「清晰度」区块，改为展示「线路」选择。

List<Episode> episodesWithLines() => [
  Episode({'id': 'a1', 'title': '第01集', 'currentEpisode': 1}, 1),
  Episode({'id': 'a2', 'title': '第02集', 'currentEpisode': 2}, 2),
  Episode({'id': 'b1', 'title': '第01集·mihua', 'currentEpisode': 1}, 3),
  Episode({'id': 'b2', 'title': '第02集·mihua', 'currentEpisode': 2}, 4),
];

Widget mountMenu({
  required PlayerMenuSection section,
  required List<int> qualities,
  required List<PlayerLine> lines,
  required int currentIndex,
  required ValueChanged<int> onEpisode,
}) => MaterialApp(
  home: Scaffold(
    body: PlayerMenu(
      section: section,
      episodes: episodesWithLines(),
      currentIndex: currentIndex,
      preferences: const PlaybackPreferences(),
      qualities: qualities,
      actualQuality: 0,
      lines: lines,
      local: false,
      favorite: false,
      mobile: true,
      onEpisode: onEpisode,
      onFavorite: () async {},
      onPreferences: (_) async {},
    ),
  ),
);

void main() {
  testWidgets('没有真实清晰度时隐藏清晰度区块，展示线路选择', (tester) async {
    final lines = [
      const PlayerLine(index: 0, label: '线路 1'),
      const PlayerLine(index: 2, label: 'mihua'),
    ];
    int? selected;
    await tester.pumpWidget(
      mountMenu(
        section: PlayerMenuSection.quality,
        qualities: const [],
        lines: lines,
        currentIndex: 0,
        onEpisode: (index) => selected = index,
      ),
    );
    await tester.pumpAndSettle();

    // 标题改为「线路」，不再叫「清晰度」。
    expect(find.text('线路'), findsWidgets);
    expect(find.text('清晰度'), findsNothing);
    // 没有 quality chips。
    expect(find.byKey(const ValueKey('menu-quality-0')), findsNothing);
    // 两个线路 chip 都在，点击切换章节。
    expect(find.byKey(const ValueKey('menu-line-0')), findsOneWidget);
    expect(find.byKey(const ValueKey('menu-line-2')), findsOneWidget);
    await tester.tap(find.byKey(const ValueKey('menu-line-2')));
    expect(selected, 2);
  });

  testWidgets('有真实清晰度时同时展示清晰度与线路', (tester) async {
    await tester.pumpWidget(
      mountMenu(
        section: PlayerMenuSection.quality,
        qualities: const [1080, 720],
        lines: const [
          PlayerLine(index: 0, label: '线路 1'),
          PlayerLine(index: 2, label: 'mihua'),
        ],
        currentIndex: 0,
        onEpisode: (_) {},
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('清晰度 / 线路'), findsOneWidget);
    expect(find.byKey(const ValueKey('menu-quality-1080')), findsOneWidget);
    expect(find.byKey(const ValueKey('menu-line-2')), findsOneWidget);
  });

  testWidgets('单线路时不展示线路区块', (tester) async {
    await tester.pumpWidget(
      mountMenu(
        section: PlayerMenuSection.quality,
        qualities: const [],
        lines: const [PlayerLine(index: 0, label: '线路 1')],
        currentIndex: 0,
        onEpisode: (_) {},
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('线路'), findsNothing);
    expect(find.byKey(const ValueKey('menu-line-0')), findsNothing);
    // 单线路且无画质：给出解释而不是空白面板。
    expect(find.textContaining('没有可选的清晰度或线路'), findsOneWidget);
  });
}
