import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/drink_detail_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _sampleDrink = Drink(
  id: 1,
  slug: 'cherry-cola',
  name: 'Cherry Cola',
  brand: 'Soda Co',
  style: 'Cola',
  volumeMl: 330,
  container: 'can',
  rating: 4.5,
  description: 'Sharp cherry and a long, dry finish.',
  caffeineMgPer100ml: 12,
  sugarGPer100ml: 10.5,
  caloriesKcalPer100ml: 44,
  status: 'limited',
  raterCount: 12,
  ratingShown: true,
  flavours: [DrinkFlavour(name: 'Cherry', slug: 'cherry', intensity: 3)],
  aliases: ['Cherry Fizz'],
);

class _FakeDrinkApi extends ApiClient {
  _FakeDrinkApi({this.detail = _sampleDrink, this.failFirstDrink = false});

  final Drink detail;
  final bool failFirstDrink;
  int drinkCalls = 0;

  @override
  Future<Drink> drink(String slug) async {
    drinkCalls += 1;
    if (failFirstDrink && drinkCalls == 1) {
      throw ApiException('Cannot reach the server.');
    }
    return detail;
  }

  @override
  Future<List<DrinkImage>> drinkImages(String slug) async => const [
    DrinkImage(
      id: 10,
      mediaId: 'media-10',
      isPrimary: true,
      attribution: 'Ada Lovelace',
    ),
    DrinkImage(id: 11, mediaId: 'media-11', attribution: 'Grace Hopper'),
  ];
}

class _SignedOutAuth extends AuthController {
  @override
  AuthState build() => const AuthState(loading: false);
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(_SignedOutAuth.new),
  ],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const DrinkDetailScreen(slug: 'cherry-cola'),
  ),
);

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('drink detail shows loading then the full record', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FakeDrinkApi()));
    expect(find.text('Loading soda…'), findsOneWidget);

    await tester.pumpAndSettle();

    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('SODA CO'), findsOneWidget);
    expect(find.text('Cola · Can · 330 ml'), findsOneWidget);
    expect(find.text('Limited run'), findsOneWidget);
    expect(find.text('4.5'), findsOneWidget);
    expect(find.text('12 ratings'), findsOneWidget);
    expect(find.text('Log this soda'), findsOneWidget);
    expect(find.text('Sharp cherry and a long, dry finish.'), findsOneWidget);
    expect(find.text('Caffeine'), findsOneWidget);
    expect(find.text('Sugar'), findsOneWidget);
    expect(find.text('Calories'), findsOneWidget);
    expect(find.text('Cherry'), findsOneWidget);
    expect(find.text('3/5'), findsOneWidget);
    expect(find.text('Cherry Fizz'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a drink without enough raters hides the average', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      _app(
        _FakeDrinkApi(
          detail: const Drink(
            id: 1,
            slug: 'cherry-cola',
            name: 'Cherry Cola',
            brand: 'Soda Co',
            style: 'Cola',
            volumeMl: 330,
            container: 'can',
            rating: 4.5,
            raterCount: 2,
            ratingShown: false,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Not enough ratings yet'), findsOneWidget);
    expect(find.text('4.5'), findsNothing);
    expect(find.text('2 ratings'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the gallery lists every photo with its credit', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FakeDrinkApi()));
    await tester.pumpAndSettle();

    await tester.dragUntilVisible(
      find.text('Photos'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    await tester.pumpAndSettle();

    final tiles = find.byWidgetPredicate(
      (widget) =>
          widget.key is ValueKey<String> &&
          (widget.key! as ValueKey<String>).value.startsWith('drink-photo-'),
    );
    expect(tiles, findsNWidgets(2));
    expect(find.text('Ada Lovelace'), findsOneWidget);
    expect(find.text('Grace Hopper'), findsOneWidget);
    expect(find.text('Primary'), findsOneWidget);
    expect(find.text('Add a photo'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a failed load offers a retry that succeeds', (tester) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeDrinkApi(failFirstDrink: true);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('Could not load this soda'), findsOneWidget);
    expect(find.text('Cannot reach the server.'), findsOneWidget);

    await tester.tap(find.text('Try again'));
    await tester.pumpAndSettle();

    expect(api.drinkCalls, 2);
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
