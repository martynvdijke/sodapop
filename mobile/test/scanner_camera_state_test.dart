import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/scanner_screen.dart';

/// A scanner platform that can record its start options and fail on demand,
/// the way CameraX does when the camera is busy or the permission is denied.
class _FakeScannerPlatform extends MobileScannerPlatform {
  final _barcodes = StreamController<BarcodeCapture?>.broadcast();
  final _torch = StreamController<TorchState>.broadcast();
  final _zoom = StreamController<double>.broadcast();

  bool running = false;
  bool paused = false;
  int starts = 0;
  int stops = 0;
  int pauses = 0;
  StartOptions? options;

  /// Thrown once by the next [start], so a retry can succeed.
  MobileScannerException? startError;

  /// Makes every [pause] fail, like a CameraX teardown error.
  bool pauseFails = false;

  /// Holds [start] open so the starting placeholder stays on screen.
  Completer<void>? startGate;

  bool _closed = false;

  @override
  Stream<BarcodeCapture?> get barcodesStream => _barcodes.stream;

  @override
  Stream<TorchState> get torchStateStream => _torch.stream;

  @override
  Stream<double> get zoomScaleStateStream => _zoom.stream;

  @override
  Future<MobileScannerViewAttributes> start(StartOptions startOptions) async {
    starts++;
    options = startOptions;
    final gate = startGate;
    if (gate != null) await gate.future;
    final error = startError;
    if (error != null) {
      startError = null;
      throw error;
    }
    running = true;
    paused = false;
    return const MobileScannerViewAttributes(
      cameraDirection: CameraFacing.back,
      currentTorchMode: TorchState.off,
      size: Size(640, 480),
      numberOfCameras: 1,
    );
  }

  @override
  Future<void> stop() async {
    stops++;
    running = false;
    paused = false;
  }

  /// Pausing keeps the surface/texture alive, like CameraX keeping its
  /// surface producer so the preview freezes on the last frame.
  @override
  Future<void> pause() async {
    pauses++;
    if (pauseFails) {
      throw const MobileScannerException(
        errorCode: MobileScannerErrorCode.genericError,
      );
    }
    running = false;
    paused = true;
  }

  @override
  Future<void> toggleTorch() async {}

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

  @override
  Future<BarcodeResult> barcode(String gtin) async {
    calls.add(gtin);
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

Future<_ScannerApi> _pumpScanner(
  WidgetTester tester,
  _FakeScannerPlatform platform,
) async {
  final api = _ScannerApi();
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: const MaterialApp(home: ScannerScreen()),
    ),
  );
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
  return api;
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

  testWidgets('the scanner asks for the formats the app can normalize', (
    tester,
  ) async {
    await _pumpScanner(tester, platform);

    final options = platform.options;
    expect(options, isNotNull);
    final started = options!;
    expect(started.detectionSpeed, DetectionSpeed.noDuplicates);
    // ML Kit's zoom suggestion helps with small, curved cans.
    expect(started.autoZoom, isTrue);
    // The default Android resolution stays in place: no scan window is set,
    // which would fight DetectionSpeed.noDuplicates.
    expect(started.cameraResolution, isNull);

    expect(started.formats.toSet(), {
      BarcodeFormat.ean13,
      BarcodeFormat.ean8,
      BarcodeFormat.upcA,
      BarcodeFormat.code128,
    });
    // Zero padding a UPC-E into a GTIN-14 describes a different product.
    expect(started.formats, isNot(contains(BarcodeFormat.upcE)));
  });

  testWidgets('a starting camera shows a spinner instead of a black box', (
    tester,
  ) async {
    platform.startGate = Completer<void>();

    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(_ScannerApi())],
        child: const MaterialApp(home: ScannerScreen()),
      ),
    );
    await tester.pump();

    expect(find.text('Starting camera…'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    // The lime frame is still there while the camera warms up.
    expect(find.byKey(const Key('scanner-frame')), findsOneWidget);

    platform.startGate!.complete();
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('Starting camera…'), findsNothing);
    expect(platform.running, isTrue);
  });

  testWidgets('a denied camera permission explains how to allow it', (
    tester,
  ) async {
    platform.startError = const MobileScannerException(
      errorCode: MobileScannerErrorCode.permissionDenied,
    );

    await _pumpScanner(tester, platform);

    expect(
      find.text(
        'Sodapop needs camera access to read cans. Allow the camera in your '
        'system settings, then try again.',
      ),
      findsOneWidget,
    );
    expect(find.text('Starting camera…'), findsNothing);
    // Nothing to aim at, so the lime frame steps aside for the message.
    expect(find.byKey(const Key('scanner-frame')), findsNothing);
    expect(find.byKey(const Key('scanner-camera-retry')), findsOneWidget);
    // The manual path is untouched and still available.
    expect(find.byType(TextField), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('Try again restarts the camera after a failed start', (
    tester,
  ) async {
    platform.startError = const MobileScannerException(
      errorCode: MobileScannerErrorCode.genericError,
    );

    await _pumpScanner(tester, platform);
    expect(find.byKey(const Key('scanner-camera-retry')), findsOneWidget);
    expect(platform.running, isFalse);

    await tester.tap(find.byKey(const Key('scanner-camera-retry')));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(platform.starts, 2);
    expect(platform.running, isTrue);
    expect(find.byKey(const Key('scanner-camera-retry')), findsNothing);
    expect(find.byKey(const Key('scanner-frame')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a scan pauses the preview instead of releasing the camera', (
    tester,
  ) async {
    final api = await _pumpScanner(tester, platform);

    expect(platform.starts, 1);
    expect(platform.pauses, 0);
    expect(platform.stops, 0);

    platform.detect('5449000000996');
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(api.calls, ['05449000000996']);
    expect(find.text('Coca-Cola'), findsOneWidget);
    // Pause keeps CameraX's surface producer, so the preview freezes instead
    // of turning the released texture black.
    expect(platform.pauses, 1);
    expect(platform.stops, 0);
    expect(platform.paused, isTrue);

    // "Scan again" resumes the same paused session.
    await tester.tap(find.text('Scan again'));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(platform.starts, 2);
    expect(platform.paused, isFalse);
    expect(platform.running, isTrue);
    expect(find.text('Coca-Cola'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a manual lookup also pauses rather than stopping the camera', (
    tester,
  ) async {
    final api = await _pumpScanner(tester, platform);

    await tester.enterText(find.byType(TextField), '5449000000996');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(api.calls, ['05449000000996']);
    expect(find.text('Coca-Cola'), findsOneWidget);
    expect(platform.pauses, 1);
    expect(platform.stops, 0);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'a failing pause still resolves the lookup and clears the spinner',
    (tester) async {
      platform.pauseFails = true;
      final api = await _pumpScanner(tester, platform);

      platform.detect('5449000000996');
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      expect(api.calls, ['05449000000996']);
      expect(find.text('Coca-Cola'), findsOneWidget);
      // No spinner left behind by the failed pause.
      expect(find.byType(CircularProgressIndicator), findsNothing);
      // The camera error is reported without hiding the result.
      expect(
        find.textContaining('The camera stopped unexpectedly'),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);
    },
  );
}
