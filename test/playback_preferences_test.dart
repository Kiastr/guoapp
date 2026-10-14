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
  });
}
