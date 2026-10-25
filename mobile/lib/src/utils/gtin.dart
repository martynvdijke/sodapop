/// Normalizes an EAN-8, UPC-A, EAN-13 or GTIN-14 value to GTIN-14 and
/// validates its GS1 check digit. Mirrors `internal/catalog.NormalizeGTIN`.
String? normalizeGtin(String raw) {
  final digits = raw.replaceAll(RegExp(r'[^0-9]'), '');
  if (digits.length != 8 && digits.length != 12 && digits.length != 13 && digits.length != 14) {
    return null;
  }
  final padded = digits.padLeft(14, '0');
  var sum = 0;
  for (var i = 0; i < 13; i++) {
    final digit = int.parse(padded[i]);
    sum += (i.isEven) ? digit * 3 : digit;
  }
  final expected = (10 - (sum % 10)) % 10;
  if (expected != int.parse(padded[13])) return null;
  return padded;
}
