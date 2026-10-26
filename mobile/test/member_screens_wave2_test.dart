import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/checkin_detail_screen.dart';
import 'package:sodapop/src/screens/insights_screen.dart';
import 'package:sodapop/src/screens/mix_create_screen.dart';
import 'package:sodapop/src/screens/passport_screen.dart';
import 'package:sodapop/src/screens/taste_screen.dart';
import 'package:sodapop/src/screens/venue_detail_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _InsightsApi extends ApiClient {
  int? requestedDays;

  @override
  Future<Insights> insights({int days = 30}) async {
    requestedDays = days;
    return const Insights(
      rangeDays: 30,
      from: '2026-10-01',
      to: '2026-10-26',
      pours: 12,
      sugarG: 120.5,
      caffeineMg: 90,
      caloriesKcal: 1500,
      avgSugarPerDayG: 4,
      avgCaffeinePerDayMg: 3,
      bedtime: '23:00',
      lastCaffeineAt: '14:00',
      caffeineClearBy: '19:00',
      lateCaffeine: true,
      series: [
        InsightPoint(
          date: '2026-10-24',
          sugarG: 3,
          caffeineMg: 10,
          caloriesKcal: 40,
          pours: 1,
        ),
        InsightPoint(
          date: '2026-10-25',
          sugarG: 0,
          caffeineMg: 0,
          caloriesKcal: 0,
        ),
        InsightPoint(
          date: '2026-10-26',
          sugarG: 6,
          caffeineMg: 20,
          caloriesKcal: 80,
          pours: 2,
        ),
      ],
      topDrinks: [
        InsightDrink(
          slug: 'cola',
          name: 'Cherry Cola',
          brand: 'Soda Co',
          pours: 4,
          sugarG: 40,
          caffeineMg: 30,
        ),
      ],
    );
  }
}

class _VenueApi extends ApiClient {
  bool saved = false;

  @override
  Future<VenueDetail> venue(int id) async => VenueDetail(
    venue: Venue(
      id: id,
      slug: 'corner-shop',
      name: 'Corner Shop',
      kind: 'shop',
      city: 'Leeds',
      address: '1 High Street',
    ),
    drinks: const [
      VenueDrink(
        slug: 'cola',
        name: 'Cherry Cola',
        brand: 'Soda Co',
        lastSeenAt: '2026-10-20T10:00:00Z',
        onOffer: true,
      ),
    ],
  );

  @override
  Future<List<VenueMedia>> venueMedia(int id) async => const [];

  @override
  Future<List<SavedVenue>> savedVenues() async => saved
      ? [
          const SavedVenue(
            id: 4,
            name: 'Corner Shop',
            kind: 'shop',
            city: 'Leeds',
          ),
        ]
      : const [];

  @override
  Future<void> saveVenue(int id) async => saved = true;

  @override
  Future<void> unsaveVenue(int id) async => saved = false;
}

class _TasteApi extends ApiClient {
  @override
  Future<List<Twin>> tasteTwins({int limit = 10}) async => const [
    Twin(
      handle: 'grace',
      displayName: 'Grace Hopper',
      score: 87,
      commonDrinks: 6,
    ),
  ];

  @override
  Future<List<Recommendation>> tasteRecommendations({int limit = 10}) async =>
      const [
        Recommendation(
          drinkId: 1,
          slug: 'cherry-cola',
          name: 'Cherry Cola',
          brand: 'Soda Co',
          style: 'Cola',
          score: 16.2,
          reason: 'Three taste twins rate this highly.',
        ),
      ];
}

class _PassportApi extends ApiClient {
  @override
  Future<Passport> passport() async => const Passport(
    stats: ProfileStats(checkins: 42, drinks: 18, toasts: 7, badges: 1),
    badges: [
      ProfileBadge(
        slug: 'first-pour',
        name: 'First Pour',
        description: 'Logged your first soda.',
        awardedAt: '2025-03-02T09:00:00Z',
      ),
    ],
  );

  @override
  Future<List<BadgeStatus>> badges() async => const [
    BadgeStatus(
      slug: 'first-pour',
      name: 'First Pour',
      description: 'Logged your first soda.',
      earned: true,
      awardedAt: '2025-03-02T09:00:00Z',
      progress: 1,
      target: 1,
      percent: 100,
    ),
    BadgeStatus(
      slug: 'ten-pours',
      name: 'Ten Pours',
      description: 'Log ten sodas.',
      earned: false,
      progress: 3,
      target: 10,
      percent: 30,
    ),
  ];
}

class _DeleteCheckinApi extends ApiClient {
  bool deleted = false;

  @override
  Future<CheckinDetail> getCheckin(String id) async => CheckinDetail(
    id: id,
    handle: 'ada',
    displayName: 'Ada Lovelace',
    initial: 'A',
    drinkName: 'Cherry Cola',
    rating: 8,
    visibility: 'private',
    canDelete: true,
  );

  @override
  Future<List<Comment>> listComments(String checkinId) async => const [];

  @override
  Future<void> deleteCheckin(String id) async => deleted = true;
}

void main() {
  testWidgets('insights renders totals and switches range', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _InsightsApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const InsightsScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Pours'), findsOneWidget);
    expect(find.text('Daily totals'), findsOneWidget);
    expect(api.requestedDays, 30);

    await tester.tap(find.text('7 days'));
    await tester.pumpAndSettle();

    expect(api.requestedDays, 7);
    expect(find.text('7 days'), findsOneWidget);

    await tester.scrollUntilVisible(find.text('Caffeine clock'), 300);
    await tester.pumpAndSettle();
    expect(find.text('Caffeine clock'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('venue detail saves and unsaves', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _VenueApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const VenueDetailScreen(venueId: 4),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Corner Shop'), findsOneWidget);
    expect(find.text('Seen here'), findsOneWidget);
    expect(find.text('Save venue'), findsOneWidget);

    await tester.tap(find.text('Save venue'));
    await tester.pumpAndSettle();

    expect(api.saved, isTrue);
    expect(find.text('Saved'), findsOneWidget);

    await tester.tap(find.text('Saved'));
    await tester.pumpAndSettle();

    expect(api.saved, isFalse);
    expect(find.text('Save venue'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('mix form validates name and component count', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      ProviderScope(
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const MixCreateScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.dragUntilVisible(
      find.text('Create mix'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.text('Create mix'));
    await tester.pumpAndSettle();

    expect(
      find.text('Give the mix a name of 2–80 characters.'),
      findsOneWidget,
    );

    await tester.dragUntilVisible(
      find.byType(TextField).first,
      find.byType(ListView),
      const Offset(0, 300),
    );
    await tester.enterText(find.byType(TextField).first, 'Cherry sunset');
    FocusManager.instance.primaryFocus?.unfocus();
    await tester.pumpAndSettle();
    await tester.dragUntilVisible(
      find.text('Create mix'),
      find.byType(ListView),
      const Offset(0, -300),
    );
    await tester.ensureVisible(find.text('Create mix'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Create mix'));
    await tester.pumpAndSettle();

    expect(find.text('Choose between two and six sodas.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('taste switches between twins and recommendations', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(_TasteApi())],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const TasteScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Grace Hopper'), findsOneWidget);
    expect(find.text('87% match'), findsOneWidget);
    expect(find.text('6 drinks in common'), findsOneWidget);

    await tester.tap(find.text('For you'));
    await tester.pumpAndSettle();

    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Three taste twins rate this highly.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('passport shows earned badges and opens all badges', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(_PassportApi())],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const PassportScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Every check-in is a stamp.'), findsOneWidget);
    expect(find.text('First Pour'), findsOneWidget);

    await tester.ensureVisible(find.text('See all badges'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('See all badges'));
    await tester.pumpAndSettle();

    expect(find.text('Badges'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('Ten Pours'), 300);
    await tester.pumpAndSettle();

    expect(find.text('Ten Pours'), findsOneWidget);
    expect(find.text('3 / 10'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('check-in menu deletes after confirmation', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _DeleteCheckinApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: TextButton(
                  onPressed: () => CheckinDetailScreen.open(context, 'c1'),
                  child: const Text('open check-in'),
                ),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open check-in'));
    await tester.pumpAndSettle();
    expect(find.text('Cherry Cola'), findsOneWidget);

    await tester.tap(find.byTooltip('Check-in options'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete check-in'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();

    expect(api.deleted, isTrue);
    expect(find.text('open check-in'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
