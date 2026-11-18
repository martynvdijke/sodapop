import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/drink_detail_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _drink = Drink(
  id: 1,
  slug: 'cherry-cola',
  name: 'Cherry Cola',
  brand: 'Soda Co',
  style: 'Cola',
  volumeMl: 330,
  container: 'can',
  ratingShown: true,
  rating: 4.25,
  raterCount: 12,
  barcodeCount: 2,
);

const _emptyDrink = Drink(
  id: 1,
  slug: 'cherry-cola',
  name: 'Cherry Cola',
  brand: 'Soda Co',
  style: 'Cola',
);

class _SignedInAuth extends AuthController {
  @override
  AuthState build() => const AuthState(
    loading: false,
    serverUrl: 'https://soda.example',
    token: 'sd_test_token_1234567890',
    me: Me(
      id: 'u1',
      handle: 'ada',
      displayName: 'Ada Lovelace',
      email: 'ada@example.com',
    ),
  );
}

class _SignedOutAuth extends AuthController {
  @override
  AuthState build() => const AuthState(loading: false);
}

class _BarcodeApi extends ApiClient {
  _BarcodeApi({this.detail = _drink, this.created = true, this.error});

  final Drink detail;
  bool created;
  Object? error;
  int calls = 0;
  String? lastSlug;
  String? lastGtin;
  String? lastSymbology;

  @override
  Future<Drink> drink(String slug) async => detail;

  @override
  Future<List<DrinkImage>> drinkImages(String slug) async => const [];

  @override
  Future<bool> contributeBarcode(
    String drinkSlug, {
    required String gtin,
    String symbology = '',
  }) async {
    calls += 1;
    lastSlug = drinkSlug;
    lastGtin = gtin;
    lastSymbology = symbology;
    final failure = error;
    if (failure != null) throw failure;
    return created;
  }
}

Widget _app(ApiClient api, {bool signedIn = true}) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(signedIn ? _SignedInAuth.new : _SignedOutAuth.new),
  ],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const DrinkDetailScreen(slug: 'cherry-cola'),
  ),
);

Future<void> _openSheet(WidgetTester tester) async {
  await tester.dragUntilVisible(
    find.byKey(const Key('drink-add-barcode')),
    find.byType(ListView),
    const Offset(0, -300),
  );
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const Key('drink-add-barcode')));
  await tester.pumpAndSettle();
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('the barcodes section sits above the gallery with its count', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_BarcodeApi()));
    await tester.pumpAndSettle();

    await tester.dragUntilVisible(
      find.text('Barcodes'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    await tester.pumpAndSettle();

    expect(find.text('Barcodes'), findsOneWidget);
    expect(find.text('2 barcodes'), findsOneWidget);
    expect(
      find.textContaining('The digits under the barcode on the package.'),
      findsOneWidget,
    );
    expect(find.text('Add a barcode'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an empty catalog count reads as no barcodes yet', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_BarcodeApi(detail: _emptyDrink)));
    await tester.pumpAndSettle();

    await tester.dragUntilVisible(
      find.text('No barcodes yet.'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    await tester.pumpAndSettle();

    expect(find.text('No barcodes yet.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('signed-out visitors see the count but no add action', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_BarcodeApi(), signedIn: false));
    await tester.pumpAndSettle();

    await tester.dragUntilVisible(
      find.text('Barcodes'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('drink-add-barcode')), findsNothing);
    expect(
      find.text('Barcodes are added by members who are signed in.'),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('a bad check digit is rejected before any request', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi();
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();
    await _openSheet(tester);

    expect(find.text('8 to 14 digits'), findsOneWidget);

    await tester.enterText(
      find.byKey(const Key('barcode-input')),
      '5449000000997',
    );
    await tester.pump();

    // The derived type follows the digit count.
    expect(find.text('EAN-13'), findsOneWidget);

    await tester.tap(find.byKey(const Key('barcode-submit')));
    await tester.pumpAndSettle();

    expect(find.textContaining('not a valid barcode'), findsOneWidget);
    expect(api.calls, 0);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a new code closes the sheet, confirms and bumps the count', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi(detail: _emptyDrink);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();
    await _openSheet(tester);

    await tester.enterText(
      find.byKey(const Key('barcode-input')),
      '5449000000996',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('barcode-submit')));
    await tester.pumpAndSettle();

    expect(api.calls, 1);
    expect(api.lastSlug, 'cherry-cola');
    expect(api.lastGtin, '5449000000996');
    expect(api.lastSymbology, 'ean13');
    expect(find.byKey(const Key('barcode-input')), findsNothing);
    expect(find.text('Barcode added'), findsOneWidget);

    await tester.dragUntilVisible(
      find.text('1 barcode'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    expect(find.text('1 barcode'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a code already on this drink says so and keeps the count', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi(detail: _emptyDrink, created: false);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();
    await _openSheet(tester);

    await tester.enterText(
      find.byKey(const Key('barcode-input')),
      '036000291452',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('barcode-submit')));
    await tester.pumpAndSettle();

    expect(api.lastSymbology, 'upca');
    expect(find.text('Already on file'), findsOneWidget);

    await tester.dragUntilVisible(
      find.text('No barcodes yet.'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    expect(find.text('No barcodes yet.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a taken code names the drink that holds it', (tester) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi(
      error: ApiException(
        'that barcode already belongs to Pepsi',
        statusCode: 409,
        code: 'barcode_taken',
      ),
    );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();
    await _openSheet(tester);

    await tester.enterText(
      find.byKey(const Key('barcode-input')),
      '5449000000996',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('barcode-submit')));
    await tester.pumpAndSettle();

    expect(find.text('Barcode already taken'), findsOneWidget);
    expect(find.text('that barcode already belongs to Pepsi'), findsOneWidget);

    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();

    // The sheet stays open so the digits can be corrected.
    expect(find.byKey(const Key('barcode-input')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the rate limit asks for a minute and keeps the sheet open', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi(
      error: ApiException(
        'slow down',
        statusCode: 429,
        code: 'rate_limited',
      ),
    );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();
    await _openSheet(tester);

    await tester.enterText(
      find.byKey(const Key('barcode-input')),
      '5449000000996',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('barcode-submit')));
    await tester.pumpAndSettle();

    expect(find.textContaining('Try again in a minute'), findsOneWidget);
    expect(find.byKey(const Key('barcode-input')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a server-side invalid_barcode shows a field error', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _BarcodeApi(
      error: ApiException('bad code', statusCode: 400, code: 'invalid_barcode'),
    );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();
    await _openSheet(tester);

    await tester.enterText(
      find.byKey(const Key('barcode-input')),
      '5449000000996',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('barcode-submit')));
    await tester.pumpAndSettle();

    expect(find.textContaining('not a valid barcode'), findsOneWidget);
    expect(find.byKey(const Key('barcode-input')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
