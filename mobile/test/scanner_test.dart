import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/scanner_screen.dart';

class _FakeScannerPlatform extends MobileScannerPlatform {
  final _barcodes = StreamController<BarcodeCapture?>.broadcast();
  final _torch = StreamController<TorchState>.broadcast();
  final _zoom = StreamController<double>.broadcast();

  bool running = false;
  int torchToggles = 0;
  bool _closed = false;

  @override
  Stream<BarcodeCapture?> get barcodesStream => _barcodes.stream;

  @override
  Stream<TorchState> get torchStateStream => _torch.stream;

  @override
  Stream<double> get zoomScaleStateStream => _zoom.stream;

  @override
  Future<MobileScannerViewAttributes> start(StartOptions startOptions) async {
    running = true;
    return const MobileScannerViewAttributes(
      cameraDirection: CameraFacing.back,
      currentTorchMode: TorchState.off,
      size: Size(640, 480),
      numberOfCameras: 1,
    );
  }

  @override
  Future<void> stop() async {
    running = false;
  }

  @override
  Future<void> toggleTorch() async {
    torchToggles++;
  }

  @override
  Widget buildCameraView() => const ColoredBox(color: Colors.black);

  @override
  Future<void> dispose() async {
    await close();
  }

  void detect(String raw) {
    _barcodes.add(
      BarcodeCapture(
        barcodes: [Barcode(rawValue: raw, format: BarcodeFormat.ean13)],
      ),
    );
  }

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await _barcodes.close();
    await _torch.close();
    await _zoom.close();
  }
}

class _ScannerApi extends ApiClient {
  final calls = <String>[];
  bool fail = false;

  @override
  Future<BarcodeResult> barcode(String gtin) async {
    calls.add(gtin);
    if (fail) throw ApiException('Server is offline.');
    return const BarcodeResult(
      status: 'matched',
      drink: Drink(
        id: 1,
        slug: 'coca-cola',
        name: 'Coca-Cola',
        brand: 'Coca-Cola',
        style: 'Cola',
      ),
    );
  }
}

Future<void> _pumpScanner(WidgetTester tester, _ScannerApi api) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: const MaterialApp(home: ScannerScreen()),
    ),
  );
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late MobileScannerPlatform original;
  late _FakeScannerPlatform platform;

  setUp(() {
    original = MobileScannerPlatform.instance;
    platform = _FakeScannerPlatform();
    MobileScannerPlatform.instance = platform;
  });

  tearDown(() async {
    MobileScannerPlatform.instance = original;
    await platform.close();
  });

  testWidgets('camera detection normalizes the GTIN and resolves the drink', (
    tester,
  ) async {
    final api = _ScannerApi();
    await _pumpScanner(tester, api);

    expect(platform.running, isTrue);
    platform.detect('5449000000996');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(api.calls, ['05449000000996']);
    expect(find.text('Coca-Cola'), findsOneWidget);
    expect(find.text('Log this soda'), findsOneWidget);
    expect(platform.running, isFalse);
  });

  testWidgets('manual entry uses the same normalization and lookup path', (
    tester,
  ) async {
    final api = _ScannerApi();
    await _pumpScanner(tester, api);

    await tester.enterText(find.byType(TextField), '5449000000996');
    await tester.tap(find.byIcon(Icons.arrow_forward_rounded));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(api.calls, ['05449000000996']);
    expect(find.text('Coca-Cola'), findsOneWidget);
  });

  testWidgets('invalid barcode reports an error without calling the server', (
    tester,
  ) async {
    final api = _ScannerApi();
    await _pumpScanner(tester, api);

    platform.detect('123');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(api.calls, isEmpty);
    expect(find.text('That barcode is not a valid GTIN.'), findsOneWidget);
    expect(platform.running, isTrue);
  });

  testWidgets('server failure is shown and the scanner can scan again', (
    tester,
  ) async {
    final api = _ScannerApi()..fail = true;
    await _pumpScanner(tester, api);

    platform.detect('5449000000996');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('Server is offline.'), findsOneWidget);
    expect(platform.running, isFalse);

    await tester.tap(find.text('Try again'));
    await tester.pump();
    expect(platform.running, isTrue);
  });

  testWidgets('torch action reaches the native scanner', (tester) async {
    final api = _ScannerApi();
    await _pumpScanner(tester, api);

    await tester.tap(find.byTooltip('Torch'));
    await tester.pump();

    expect(platform.torchToggles, 1);
  });
}
