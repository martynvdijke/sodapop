import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/scanner_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

/// The camera plugin has no platform implementation in widget tests.
class _FakeScannerPlatform extends MobileScannerPlatform {
  final _barcodes = StreamController<BarcodeCapture?>.broadcast();
  final _torch = StreamController<TorchState>.broadcast();
  final _zoom = StreamController<double>.broadcast();
  bool _closed = false;

  @override
  Stream<BarcodeCapture?> get barcodesStream => _barcodes.stream;

  @override
  Stream<TorchState> get torchStateStream => _torch.stream;

  @override
  Stream<double> get zoomScaleStateStream => _zoom.stream;

  @override
  Future<MobileScannerViewAttributes> start(StartOptions startOptions) async {
    return const MobileScannerViewAttributes(
      cameraDirection: CameraFacing.back,
      currentTorchMode: TorchState.off,
      size: Size(640, 480),
      numberOfCameras: 1,
    );
  }

  @override
  Future<void> stop() async {}

  @override
  Future<void> toggleTorch() async {}

  @override
  Widget buildCameraView() => const ColoredBox(color: Colors.black);

  @override
  Future<void> dispose() async {
    await close();
  }

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await _barcodes.close();
    await _torch.close();
    await _zoom.close();
  }
}

class _BarcodeApi extends ApiClient {
  _BarcodeApi(this.result);

  final BarcodeResult result;
  final calls = <String>[];

  @override
  Future<BarcodeResult> barcode(String gtin) async {
    calls.add(gtin);
    return result;
  }
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const ScannerScreen(),
  ),
);

Future<void> _lookup(WidgetTester tester) async {
  await tester.enterText(find.byType(TextField), '5449000000996');
  await tester.tap(find.byIcon(Icons.arrow_forward_rounded));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

String _fieldText(WidgetTester tester, Key key) =>
    tester.widget<TextFormField>(find.byKey(key)).controller!.text;

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

  testWidgets('an OFF suggestion feeds the manual form without losing AI', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi(
      const BarcodeResult(
        status: 'off',
        suggested: BarcodeSuggestion(
          name: 'Cherry Cola',
          brand: 'Soda Co',
          quantity: '330 ml',
          barcode: '05449000000996',
        ),
      ),
    );
    await tester.pumpWidget(_app(api));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    await _lookup(tester);

    expect(find.text('New to Sodapop'), findsOneWidget);
    expect(find.textContaining('Cherry Cola'), findsOneWidget);
    expect(find.textContaining('Soda Co'), findsOneWidget);
    expect(find.textContaining('330 ml'), findsOneWidget);
    expect(find.byKey(const Key('scanner-draft-ai')), findsOneWidget);
    expect(find.byKey(const Key('scanner-add-manual')), findsOneWidget);

    await tester.tap(find.byKey(const Key('scanner-add-manual')));
    await tester.pumpAndSettle();

    expect(find.text('Add a soda'), findsOneWidget);
    expect(_fieldText(tester, const Key('add-barcode')), '05449000000996');
    expect(_fieldText(tester, const Key('add-name')), 'Cherry Cola');
    expect(_fieldText(tester, const Key('add-brand')), 'Soda Co');
    expect(_fieldText(tester, const Key('add-volume')), '330');
    expect(tester.takeException(), isNull);
  });

  testWidgets('an unknown code without a suggestion has no dead end', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi(const BarcodeResult(status: 'off'));
    await tester.pumpWidget(_app(api));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));
    await _lookup(tester);

    expect(find.text('New to Sodapop'), findsOneWidget);
    expect(find.textContaining('Open Food Facts suggests'), findsNothing);
    expect(find.byKey(const Key('scanner-add-manual')), findsOneWidget);

    await tester.tap(find.byKey(const Key('scanner-add-manual')));
    await tester.pumpAndSettle();

    expect(find.text('Add a soda'), findsOneWidget);
    expect(_fieldText(tester, const Key('add-barcode')), '05449000000996');
    expect(_fieldText(tester, const Key('add-name')), '');
    expect(_fieldText(tester, const Key('add-brand')), '');
    expect(_fieldText(tester, const Key('add-volume')), '355');
  });
}
