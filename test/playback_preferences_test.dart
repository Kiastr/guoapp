import 'package:duanju_app/playback_preferences.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('hold speed defaults to 3x and round trips through json', () {
    const value = PlaybackPreferences();
    expect(value.holdSpeed, 3);
    final next = value.copyWith(holdSpeed: 2.5);
    expect(next.holdSpeed, 2.5);
    expect(next.speed, 1);
    final decoded = PlaybackPreferences.fromJson(next.toJson());
    expect(decoded.holdSpeed, 2.5);
    expect(decoded.speed, 1);
    expect(decoded.quality, 0);
  });

  test('legacy and invalid hold speed fall back to 3x', () {
    final legacy = PlaybackPreferences.fromJson({
      'speed': 1.5,
      'quality': 1080,
    });
    expect(legacy.holdSpeed, 3);
    expect(legacy.speed, 1.5);
    expect(legacy.quality, 1080);

    final invalid = PlaybackPreferences.fromJson({
      'speed': 1,
      'quality': 720,
      'holdSpeed': 3.7,
    });
    expect(invalid.holdSpeed, 3);
    expect(invalid.quality, 720);
  });

  test('hold speed options stay faster than normal playback', () {
    expect(holdSpeeds.every((speed) => speed > 1), isTrue);
    expect(holdSpeeds.contains(defaultHoldSpeed), isTrue);
    expect(holdSpeeds, [1.5, 1.75, 2.0, 2.5, 3.0, 4.0, 5.0]);
  });

  test('hold speed survives a copyWith round trip used by the player menu', () {
    // 播放菜单用当前字段重新组装偏好，漏传 holdSpeed 会让选中态跳回默认 3x。
    var value = const PlaybackPreferences().copyWith(holdSpeed: 1.75);
    value = PlaybackPreferences(
      speed: value.speed,
      holdSpeed: value.holdSpeed,
      quality: value.quality,
    );
    expect(value.holdSpeed, 1.75);
  });
}
