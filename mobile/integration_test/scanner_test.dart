import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/utils/gtin.dart';

/// Android emulator flow for the barcode scanner.
///
/// The camera cannot be pointed at a real can in CI, so this test drives the
/// same native decode path the scanner uses (`analyzeImage`) with a generated
/// EAN-13 asset, then sends the normalized GTIN to a live Sodapop server.
///
/// Run with a seeded server on the host:
///   SEED_DEMO=1 go run ./cmd/server
///   flutter test integration_test/scanner_test.dart \
///     -d emulator-5554 \
///     --dart-define=E2E_BASE_URL=http://10.0.2.2:8097
void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  const asset = 'assets/test/ean13_5449000000996.png';
  const baseUrl = String.fromEnvironment(
    'E2E_BASE_URL',
    defaultValue: 'http://10.0.2.2:8097',
  );

  testWidgets('native scanner decodes the EAN-13 asset', (tester) async {
    final data = await rootBundle.load(asset);
    final file = File('${Directory.systemTemp.path}/sodapop-ean13.png');
    await file.writeAsBytes(
      data.buffer.asUint8List(data.offsetInBytes, data.lengthInBytes),
    );

    final controller = MobileScannerController(autoStart: false);
    addTearDown(controller.dispose);
    final capture = await controller.analyzeImage(
      file.path,
      formats: const [BarcodeFormat.ean13],
    );

    expect(capture, isNotNull);
    final raw = capture!.barcodes
        .map((barcode) => barcode.rawValue)
        .whereType<String>()
        .firstOrNull;
    expect(raw, '5449000000996');
    expect(normalizeGtin(raw!), '05449000000996');
  });

  testWidgets('normalized GTIN resolves against the live server', (
    tester,
  ) async {
    final api = ApiClient()..configure(baseUrl);
    final token = await api.passwordToken('e2e_admin', 'correct-horse-battery');
    api.setToken(token);

    final result = await api.barcode('05449000000996');

    expect(result.matched, isTrue);
    expect(result.drink, isNotNull);
    expect(result.drink!.name, contains('Coca-Cola'));
  });

  testWidgets('an invalid barcode never reaches the server', (tester) async {
    expect(normalizeGtin('123'), isNull);
    expect(normalizeGtin('5449000000997'), isNull);
  });
}
