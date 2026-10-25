import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/utils/url.dart';

void main() {
  group('normalizeServerUrl', () {
    test('keeps scheme, host and port', () {
      expect(normalizeServerUrl('https://soda.example.com'), 'https://soda.example.com');
      expect(normalizeServerUrl('http://192.168.1.10:8097/'), 'http://192.168.1.10:8097');
    });

    test('rejects paths, queries and credentials', () {
      expect(normalizeServerUrl('https://example.com/app'), isNull);
      expect(normalizeServerUrl('https://example.com?x=1'), isNull);
      expect(normalizeServerUrl('https://user:pass@example.com'), isNull);
    });

    test('rejects other schemes and empty values', () {
      expect(normalizeServerUrl('ftp://example.com'), isNull);
      expect(normalizeServerUrl('   '), isNull);
    });
  });

  test('ratingLabel uses quarter steps and never shows 0.0', () {
    expect(ratingLabel(0), 'Not rated');
    expect(ratingLabel(4), '1');
    expect(ratingLabel(17), '4.25');
    expect(ratingLabel(18), '4.5');
  });
}
