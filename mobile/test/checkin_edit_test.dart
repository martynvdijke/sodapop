import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/utils/checkin_edit.dart';

void main() {
  group('checkinEditWindowOpen', () {
    final createdAt = '2026-11-18T10:00:00Z';

    test('is open just inside the 24-hour boundary', () {
      final now = DateTime.utc(2026, 11, 19, 9, 59, 59);
      expect(checkinEditWindowOpen(createdAt, now: now), isTrue);
    });

    test('is closed at and past the boundary', () {
      expect(
        checkinEditWindowOpen(
          createdAt,
          now: DateTime.utc(2026, 11, 19, 10),
        ),
        isFalse,
      );
      expect(
        checkinEditWindowOpen(
          createdAt,
          now: DateTime.utc(2026, 11, 19, 10, 0, 1),
        ),
        isFalse,
      );
    });

    test('treats a missing or unparsable timestamp as closed', () {
      expect(checkinEditWindowOpen(null), isFalse);
      expect(checkinEditWindowOpen(''), isFalse);
      expect(checkinEditWindowOpen('not-a-date'), isFalse);
    });

    test('accepts a local-time now against a UTC timestamp', () {
      final now = DateTime.utc(2026, 11, 19, 9).toLocal();
      expect(checkinEditWindowOpen(createdAt, now: now), isTrue);
    });
  });

  group('parsePriceCents', () {
    test('reads major units with two decimals', () {
      final result = parsePriceCents('1.50');
      expect(result.isOk, isTrue);
      expect(result.cents, 150);
    });

    test('reads whole major units', () {
      expect(parsePriceCents('2').cents, 200);
    });

    test('pads a single decimal place', () {
      expect(parsePriceCents('1.5').cents, 150);
    });

    test('treats blank input as no price', () {
      expect(parsePriceCents('').isBlank, isTrue);
      expect(parsePriceCents('   ').isBlank, isTrue);
      expect(parsePriceCents('').cents, isNull);
    });

    test('accepts the cap and rejects anything above it', () {
      expect(parsePriceCents('1000').cents, maxPriceCents);
      expect(parsePriceCents('1000.00').cents, maxPriceCents);
      expect(parsePriceCents('1000.01').isInvalid, isTrue);
      expect(parsePriceCents('5000').isInvalid, isTrue);
    });

    test('rejects malformed input instead of clamping it', () {
      for (final input in [
        '1.234',
        '1,50',
        '-1',
        'abc',
        '1.2.3',
        r'$1.50',
        '1 000',
      ]) {
        expect(
          parsePriceCents(input).isInvalid,
          isTrue,
          reason: '"$input" should be invalid',
        );
      }
    });
  });

  group('formatMajorUnits', () {
    test('renders cents as major-unit text', () {
      expect(formatMajorUnits(150), '1.50');
      expect(formatMajorUnits(200), '2');
      expect(formatMajorUnits(199), '1.99');
      expect(formatMajorUnits(0), '0');
    });
  });
}
