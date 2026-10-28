import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/discover_screen.dart';
import 'package:sodapop/src/screens/drink_detail_screen.dart';
import 'package:sodapop/src/screens/scanner_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _cherry = Drink(
  id: 1,
  slug: 'cherry-cola',
  name: 'Cherry Cola',
  brand: 'Soda Co',
  style: 'Cola',
  container: 'can',
  volumeMl: 330,
);

class _DiscoverApi extends ApiClient {}

class _DetailApi extends ApiClient {
  @override
  Future<Drink> drink(String slug) async => _cherry;

  @override
  Future<List<DrinkImage>> drinkImages(String slug) async => const [];
}

class _ScannerApi extends ApiClient {
  _ScannerApi({this.unmatched = false});

  final bool unmatched;
  final List<String> calls = [];

  @override
  Future<BarcodeResult> barcode(String gtin) async {
    calls.add(gtin);
    if (unmatched) {
      return const BarcodeResult(
        status: 'not_found',
        suggestedName: 'Cherry Cola',
      );
    }
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

class _SignedOutAuth extends AuthController {
  @override
  AuthState build() => const AuthState(loading: false);
}

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

Widget _app(ApiClient api, Widget home) {
  return ProviderScope(
    overrides: [
      apiProvider.overrideWithValue(api),
      authProvider.overrideWith(_SignedOutAuth.new),
    ],
    child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
  );
}

void _sizeTo(WidgetTester tester, Size size) {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

Future<void> _scan(WidgetTester tester, String code) async {
  await tester.enterText(find.byType(TextField), code);
  await tester.tap(find.byIcon(Icons.arrow_forward_rounded));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late MobileScannerPlatform originalPlatform;
  late _FakeScannerPlatform platform;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    originalPlatform = MobileScannerPlatform.instance;
    platform = _FakeScannerPlatform();
    MobileScannerPlatform.instance = platform;
  });

  tearDown(() async {
    MobileScannerPlatform.instance = originalPlatform;
    await platform.close();
  });

  testWidgets('discover offers compare and an AI draft entry point', (
    tester,
  ) async {
    _sizeTo(tester, const Size(420, 900));
    var compares = 0;
    var drafts = 0;

    await tester.pumpWidget(
      _app(
        _DiscoverApi(),
        DiscoverScreen(
          compareOpener: () => compares++,
          draftOpener: () => drafts++,
        ),
      ),
    );
    await tester.pumpAndSettle();

    // The search and venues action are untouched.
    expect(find.byType(TextField), findsOneWidget);
    expect(find.byTooltip('Venues'), findsOneWidget);
    expect(find.byTooltip('Compare sodas'), findsOneWidget);
    expect(find.text('Draft a soda with AI'), findsOneWidget);
    expect(
      find.text('Turn a few notes into a provisional soda.'),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('discover-compare')));
    await tester.pump();
    expect(compares, 1);

    await tester.tap(find.byKey(const Key('discover-draft')));
    await tester.pump();
    expect(drafts, 1);

    expect(tester.takeException(), isNull);
  });

  testWidgets('discover offers both entry points in the empty state', (
    tester,
  ) async {
    _sizeTo(tester, const Size(360, 760));

    await tester.pumpWidget(
      _app(
        _DiscoverApi(),
        DiscoverScreen(compareOpener: () {}, draftOpener: () {}),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('Search the shared catalog.'), findsOneWidget);
    expect(find.byTooltip('Compare sodas'), findsOneWidget);
    expect(find.byKey(const Key('discover-draft')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an unknown barcode drafts the scanned code with AI', (
    tester,
  ) async {
    _sizeTo(tester, const Size(420, 900));
    final api = _ScannerApi(unmatched: true);
    final drafted = <String>[];

    await tester.pumpWidget(
      _app(api, ScannerScreen(draftOpener: drafted.add)),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    await _scan(tester, '5449000000996');

    expect(api.calls, ['05449000000996']);
    expect(find.text('New to Sodapop'), findsOneWidget);
    expect(find.text('05449000000996'), findsOneWidget);
    expect(find.textContaining('Open Food Facts suggests'), findsOneWidget);
    expect(find.textContaining('Cherry Cola'), findsOneWidget);
    expect(find.text('Draft it with AI'), findsOneWidget);
    expect(find.text('Scan again'), findsOneWidget);
    // The old web-catalog instruction is gone.
    expect(find.textContaining('web catalog'), findsNothing);
    expect(find.textContaining('web app'), findsNothing);

    await tester.tap(find.byKey(const Key('scanner-draft-ai')));
    await tester.pump();

    expect(drafted, ['05449000000996']);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a matched barcode keeps the log card unchanged', (tester) async {
    _sizeTo(tester, const Size(420, 900));
    final api = _ScannerApi();
    final drafted = <String>[];

    await tester.pumpWidget(
      _app(api, ScannerScreen(draftOpener: drafted.add)),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    await _scan(tester, '5449000000996');

    expect(find.text('Coca-Cola'), findsOneWidget);
    expect(find.text('Log this soda'), findsOneWidget);
    expect(find.text('Scan again'), findsOneWidget);
    expect(find.text('Draft it with AI'), findsNothing);
    expect(find.text('New to Sodapop'), findsNothing);
    expect(drafted, isEmpty);
  });

  testWidgets('drink detail compares with the current soda preselected', (
    tester,
  ) async {
    _sizeTo(tester, const Size(420, 1000));
    final compared = <String>[];

    await tester.pumpWidget(
      _app(
        _DetailApi(),
        DrinkDetailScreen(
          slug: 'cherry-cola',
          compareOpener: compared.add,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Log this soda'), findsOneWidget);
    expect(find.byTooltip('Compare sodas'), findsOneWidget);

    await tester.tap(find.byKey(const Key('drink-compare')));
    await tester.pump();

    expect(compared, ['cherry-cola']);
    expect(tester.takeException(), isNull);
  });

  testWidgets('drink detail hides compare when the soda is unavailable', (
    tester,
  ) async {
    _sizeTo(tester, const Size(420, 900));
    final compared = <String>[];

    await tester.pumpWidget(
      _app(
        _MissingDetailApi(),
        DrinkDetailScreen(
          slug: 'gone-soda',
          compareOpener: compared.add,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Soda unavailable'), findsOneWidget);
    expect(find.byTooltip('Compare sodas'), findsNothing);
    expect(compared, isEmpty);
  });
}

class _MissingDetailApi extends ApiClient {
  @override
  Future<Drink> drink(String slug) async {
    throw ApiException('not found', statusCode: 404);
  }

  @override
  Future<List<DrinkImage>> drinkImages(String slug) async => const [];
}
