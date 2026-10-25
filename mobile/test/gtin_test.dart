import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/utils/gtin.dart';

void main() {
  group('normalizeGtin', () {
    test('left-pads UPC-A to GTIN-14', () {
      expect(normalizeGtin('5449000000996'), '05449000000996');
    });

    test('accepts EAN-8 and GTIN-12', () {
      expect(normalizeGtin('96385074'), '00000096385074');
      expect(normalizeGtin('012345678905'), '00012345678905');
    });

    test('rejects a bad check digit', () {
      expect(normalizeGtin('5449000000997'), isNull);
    });

    test('rejects wrong lengths and non-digits', () {
      expect(normalizeGtin('123'), isNull);
      expect(normalizeGtin('not-a-barcode'), isNull);
    });
  });
}
