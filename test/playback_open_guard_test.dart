import 'dart:async';

import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/playback_launch_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

/// detail 永远不返回，用来模拟「点下去没有任何反应」的那几秒。
class _PendingRepository extends FixtureRepository {
  final Completer<void> gate = Completer<void>();
  int calls = 0;

  @override
  Future<DramaDetail> detail(Drama drama) async {
    calls++;
    await gate.future;
    return DramaDetail(drama, [
      Episode({'id': '1', 'title': '第1集', 'currentEpisode': 1}, 1),
    ]);
  }
}

void main() {
  testWidgets('连点同一张卡片只发起一次详情请求', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    final repository = _PendingRepository();
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => TextButton(
              onPressed: () => openPlaybackDirectly(
                context,
                drama: FixtureRepository.free,
                repository: repository,
                store: store,
              ),
              child: const Text('播放'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('播放'));
    await tester.pump();
    // 第一次点击后应立刻有反馈（detail 还没回来，界面不能是静止的）。
    expect(find.textContaining('正在打开'), findsOneWidget);

    await tester.tap(find.text('播放'));
    await tester.tap(find.text('播放'));
    await tester.pump();
    expect(repository.calls, 1, reason: '打开过程中重复点击应被忽略');

    // 让提示条自然过期，避免带着 pending timer 结束测试。
    await tester.pump(const Duration(seconds: 3));
    expect(repository.calls, 1);
    repository.gate.complete();
  });
}
