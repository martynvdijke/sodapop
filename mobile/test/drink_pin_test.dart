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
  ratingShown: true,
  rating: 4.25,
  raterCount: 12,
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

class _PinApi extends ApiClient {
  FavouriteDrink? favourite;
  int pinCalls = 0;
  int clearCalls = 0;
  bool fail = false;

  @override
  Future<Drink> drink(String slug) async => _drink;

  @override
  Future<List<DrinkImage>> drinkImages(String slug) async => const [];

  @override
  Future<Me> me({String? tokenOverride}) async => Me(
    id: 'u1',
    handle: 'ada',
    displayName: 'Ada Lovelace',
    email: 'ada@example.com',
    favouriteDrink: favourite,
  );

  @override
  Future<FavouriteDrink?> setFavouriteDrink(int drinkId) async {
    if (fail) throw ApiException('Could not pin that soda.');
    pinCalls++;
    favourite = FavouriteDrink(
      id: drinkId,
      name: _drink.name,
      slug: _drink.slug,
      brand: _drink.brand,
      style: _drink.style,
    );
    return favourite;
  }

  @override
  Future<void> clearFavouriteDrink() async {
    if (fail) throw ApiException('Could not unpin that soda.');
    clearCalls++;
    favourite = null;
  }
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(_SignedInAuth.new),
  ],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const DrinkDetailScreen(slug: 'cherry-cola'),
  ),
);

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('a soda can be pinned and unpinned from its page', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _PinApi();
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.byTooltip('Pin to profile'), findsOneWidget);
    await tester.tap(find.byTooltip('Pin to profile'));
    await tester.pumpAndSettle();

    expect(api.pinCalls, 1);
    expect(api.favourite?.id, 1);
    expect(find.byTooltip('Unpin from profile'), findsOneWidget);
    expect(find.text('Pinned to your profile.'), findsOneWidget);

    await tester.tap(find.byTooltip('Unpin from profile'));
    await tester.pumpAndSettle();

    expect(api.clearCalls, 1);
    expect(api.favourite, isNull);
    expect(find.byTooltip('Pin to profile'), findsOneWidget);
    expect(find.text('Unpinned from your profile.'), findsOneWidget);
  });

  testWidgets('a pin failure is reported and the icon stays unpinned', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _PinApi()..fail = true;
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Pin to profile'));
    await tester.pumpAndSettle();

    expect(find.text('Could not pin that soda.'), findsOneWidget);
    expect(find.byTooltip('Pin to profile'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
