import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/hongguo_account.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _AccountRepository extends FixtureRepository {
  HongguoAccountSnapshot snapshot = const HongguoAccountSnapshot(
    signedIn: true,
    favorites: [
      HongguoAccountEntry(seriesId: '7300000000000000001', title: '账号收藏'),
    ],
  );
  int calls = 0;

  @override
  bool get supportsSourceManagement => true;

  @override
  Future<HongguoAccountSnapshot> hongguoAccountSync() async {
    calls++;
    return snapshot;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('设置页给出自动同步开关与立即同步按钮，点一下会真的拉数据', (tester) async {
    tester.view.physicalSize = const Size(430, 2600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final repository = _AccountRepository();

    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(repository: repository, store: store),
      ),
    );
    await tester.pump();

    final autoSync = find.byKey(const ValueKey('hongguo-account-auto-sync'));
    final syncNow = find.byKey(const ValueKey('hongguo-account-sync-now'));
    expect(autoSync, findsOneWidget);
    expect(syncNow, findsOneWidget);

    // 默认开启。
    expect(store.hongguoAccountAutoSync, isTrue);

    await tester.tap(autoSync);
    await tester.pump();
    expect(store.hongguoAccountAutoSync, isFalse);

    await tester.tap(syncNow);
    await tester.pumpAndSettle();
    expect(repository.calls, 1);
    expect(find.textContaining('已同步'), findsOneWidget);
    expect(store.isFavorite('hongguo:7300000000000000001'), isTrue);
  });
}
