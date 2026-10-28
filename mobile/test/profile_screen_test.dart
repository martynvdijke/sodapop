import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/profile_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

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

class _ProfileApi extends ApiClient {
  _ProfileApi({
    this.checkins = const [],
    this.stats = const MyStats(
      checkins: 12,
      uniqueDrinks: 5,
      avgRating: 14,
      toastsReceived: 3,
      badges: 2,
    ),
  });

  final List<CheckinSummary> checkins;
  final MyStats stats;

  @override
  Future<MyStats> myStats() async => stats;

  @override
  Future<List<CheckinSummary>> myCheckins({
    int limit = 20,
    String? before,
  }) async => checkins;
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(_SignedInAuth.new),
  ],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const ProfileScreen(),
  ),
);

void main() {
  testWidgets('profile shows stats and recent check-ins', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      _app(
        _ProfileApi(
          checkins: const [
            CheckinSummary(
              id: 'c1',
              drinkId: 1,
              drinkName: 'Cherry Cola',
              rating: 13,
              venueName: 'Corner Shop',
              createdAt: '2026-10-26T10:00:00Z',
            ),
            CheckinSummary(
              id: 'c2',
              drinkId: 2,
              drinkName: 'Root Beer',
              createdAt: '2026-10-25T10:00:00Z',
            ),
          ],
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Ada Lovelace'), findsOneWidget);
    expect(find.text('@ada'), findsOneWidget);
    expect(find.text('12'), findsOneWidget);
    expect(find.text('5'), findsOneWidget);
    expect(find.text('3.5'), findsOneWidget);
    expect(find.text('Recent check-ins'), findsOneWidget);
    expect(find.text('2 check-ins'), findsOneWidget);
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('3.25 · Corner Shop'), findsOneWidget);
    expect(find.text('Root Beer'), findsOneWidget);
    expect(find.text('Not rated'), findsOneWidget);

    await tester.scrollUntilVisible(find.text('Saved venues'), 300);
    await tester.pumpAndSettle();

    expect(find.text('Friends'), findsOneWidget);
    expect(find.text('Sign out'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('profile shows an empty recent check-ins state', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      _app(
        _ProfileApi(
          stats: const MyStats(checkins: 0, uniqueDrinks: 0, badges: 0),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Recent check-ins'), findsOneWidget);
    expect(find.text('No check-ins yet'), findsOneWidget);
    expect(find.text('Scan a soda to start your log.'), findsOneWidget);
    expect(find.text('—'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
