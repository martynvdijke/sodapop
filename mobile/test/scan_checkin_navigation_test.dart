import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/checkin_detail_screen.dart';
import 'package:sodapop/src/screens/scanner_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

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

class _Api extends ApiClient {
  String? detailId;
  int createCalls = 0;

  @override
  Future<BarcodeResult> barcode(String gtin) async => const BarcodeResult(
    status: 'matched',
    drink: Drink(
      id: 9,
      slug: 'cherry-cola',
      name: 'Cherry Cola',
      brand: 'Soda Co',
      style: 'Cola',
    ),
  );

  @override
  Future<String> createCheckin({
    required int drinkId,
    int? rating,
    String note = '',
    int? venueId,
    String? serving,
    String prepMethod = 'none',
    String prepNote = '',
    String? visibility,
    String? idempotencyKey,
    Uint8List? photo,
    String? photoFilename,
    String? photoMime,
    double? lat,
    double? lng,
    List<String> friendIds = const [],
  }) async {
    createCalls++;
    return 'c9';
  }

  @override
  Future<CheckinDetail> getCheckin(String id) async {
    detailId = id;
    return CheckinDetail(
      id: id,
      handle: 'ada',
      displayName: 'Ada Lovelace',
      initial: 'A',
      drinkSlug: 'cherry-cola',
      drinkName: 'Cherry Cola',
      createdAt: '2026-10-30T12:00:00Z',
    );
  }

  @override
  Future<CommentPage> commentPage(
    String checkinId, {
    String? before,
    int limit = 50,
  }) async => const CommentPage();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late MobileScannerPlatform original;
  late _FakeScannerPlatform platform;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    original = MobileScannerPlatform.instance;
    platform = _FakeScannerPlatform();
    MobileScannerPlatform.instance = platform;
  });

  tearDown(() async {
    MobileScannerPlatform.instance = original;
    await platform.close();
  });

  testWidgets('a scan-led save opens the created check-in detail', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1500);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _Api();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const ScannerScreen(),
        ),
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    await tester.enterText(find.byType(TextField), '5449000000996');
    await tester.tap(find.byIcon(Icons.arrow_forward_rounded));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Log this soda'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Save check-in'));
    await tester.pumpAndSettle();

    expect(api.createCalls, 1);
    expect(find.text('Save check-in'), findsNothing);
    expect(find.byType(CheckinDetailScreen), findsOneWidget);
    expect(api.detailId, 'c9');
    expect(tester.takeException(), isNull);
  });
}
