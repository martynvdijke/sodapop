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
  beverageType: 'soda',
  brandSlug: 'soda-co',
  styleSlug: 'cola',
  flavourSlugs: ['cherry', 'vanilla'],
  flavours: [DrinkFlavour(name: 'Cherry', slug: 'cherry', intensity: 3)],
);

/// A payload without the slug fields, as an older cached drink can look.
const _bareDrink = Drink(
  id: 1,
  slug: 'cherry-cola',
  name: 'Cherry Cola',
  brand: 'Soda Co',
  style: 'Cola',
  beverageType: 'soda',
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

class _PrefillApi extends ApiClient {
  _PrefillApi({this.detail = _drink});

  final Drink detail;

  @override
  Future<Drink> drink(String slug) async => detail;

  @override
  Future<List<DrinkImage>> drinkImages(String slug) async => const [];

  @override
  Future<List<SavedSearch>> savedSearches() async => const [
    // One row keeps the screen on its list branch; the empty branch renders a
    // nested viewport (a pre-existing defect in SavedSearchesScreen, reported
    // separately) that is unrelated to this prefill contract.
    SavedSearch(id: 1, name: 'Cherry drops', query: 'cherry'),
  ];
}

Widget _app(ApiClient api, {bool signedIn = true}) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(
      signedIn ? _SignedInAuth.new : _SignedOutAuth.new,
    ),
  ],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const DrinkDetailScreen(slug: 'cherry-cola'),
  ),
);

String _fieldText(WidgetTester tester, String key) => tester
    .widget<TextField>(find.byKey(Key(key)))
    .controller!
    .text;

Future<void> _openPrefill(WidgetTester tester) async {
  await tester.dragUntilVisible(
    find.byKey(const Key('drink-drop-alert')),
    find.byType(ListView),
    const Offset(0, -300),
  );
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const Key('drink-drop-alert')));
  await tester.pumpAndSettle();
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('the drink page offers a drop alert row with its slugs', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_PrefillApi()));
    await tester.pumpAndSettle();

    await tester.dragUntilVisible(
      find.text('More like this'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    await tester.pumpAndSettle();

    expect(find.text('More like this'), findsOneWidget);
    expect(find.text('Alert me when similar drinks drop'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('opening it prefills style, brand and the first flavour', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_PrefillApi()));
    await tester.pumpAndSettle();
    await _openPrefill(tester);

    expect(find.text('Saved searches'), findsOneWidget);
    expect(_fieldText(tester, 'saved-search-style'), 'cola');
    expect(_fieldText(tester, 'saved-search-brand'), 'soda-co');
    expect(_fieldText(tester, 'saved-search-flavour'), 'cherry');
    // The beverage type lands on the dropdown's closed option.
    expect(find.text('Soda'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an empty flavour list leaves the flavour field unset', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_PrefillApi(detail: _bareDrink)));
    await tester.pumpAndSettle();
    await _openPrefill(tester);

    expect(_fieldText(tester, 'saved-search-style'), '');
    expect(_fieldText(tester, 'saved-search-brand'), '');
    expect(_fieldText(tester, 'saved-search-flavour'), '');
    expect(tester.takeException(), isNull);
  });

  testWidgets('signed-out visitors get no drop alert row', (tester) async {
    tester.view.physicalSize = const Size(400, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_PrefillApi(), signedIn: false));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('drink-drop-alert')), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
