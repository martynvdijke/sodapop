import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/profile_screen.dart';
import 'package:sodapop/src/screens/recap_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

int get _now => DateTime.now().year;

/// A recap the app can draw: totals, both leaderboards, a venue, the five
/// rating bands, a palate comparison, a fingerprint and a sparse flag.
YearRecap _fullRecap(int year) => YearRecap(
  year: year,
  totals: const RecapTotals(
    checkins: 34,
    uniqueDrinks: 12,
    styles: 8,
    newStyles: 3,
    venues: 5,
    cities: 2,
    countries: 1,
    longestStreak: 6,
    avgRating: 17,
  ),
  topDrink: const RecapDrink(
    name: 'Cherry Cola',
    slug: 'cherry-cola',
    brand: 'Soda Co',
    count: 9,
  ),
  topBrand: const RecapRow(name: 'Soda Co', slug: 'soda-co', count: 14),
  topStyles: const [
    RecapRow(name: 'Cola', slug: 'cola', count: 12),
    RecapRow(name: 'Lemonade', slug: 'lemonade', count: 5),
  ],
  topVenue: const RecapVenue(id: 3, name: 'Corner Shop', count: 7),
  ratingBuckets: const [
    RecapBucket(label: '0–4', low: 0, high: 4, count: 1),
    RecapBucket(label: '5–8', low: 5, high: 8, count: 2),
    RecapBucket(label: '9–12', low: 9, high: 12, count: 4),
    RecapBucket(label: '13–16', low: 13, high: 16, count: 6),
    RecapBucket(label: '17–20', low: 17, high: 20, count: 9),
  ],
  palate: const [RecapPalate(label: 'Overall', thisYear: 17, lastYear: 14)],
  yearsWithData: [year - 1, year],
  badges: [
    RecapBadge(
      slug: 'first-pour',
      name: 'First Pour',
      awardedAt: '$year-03-02T09:00:00Z',
    ),
  ],
  fingerprint: const RecapFingerprint(
    hasData: true,
    confident: true,
    axes: [
      RecapAxis(
        key: 'fruit',
        label: 'Fruit',
        score: 100,
        samples: 22,
        confident: true,
      ),
      RecapAxis(key: 'spice', label: 'Spice', score: 40, samples: 3),
    ],
  ),
  hasData: true,
);

class _RecapApi extends ApiClient {
  _RecapApi({this.recaps = const {}, this.failing = false});

  final Map<int, YearRecap> recaps;
  final bool failing;

  /// Every year the screen asked for, in call order.
  final List<int> requested = [];

  @override
  Future<YearRecap> recap(int year) async {
    requested.add(year);
    if (failing) throw ApiException('offline', statusCode: 503);
    return recaps[year] ?? YearRecap(year: year);
  }

  @override
  Future<MyStats> myStats() async => const MyStats();

  @override
  Future<CheckinPage> myCheckins({
    int limit = 20,
    String? before,
    String? from,
    String? to,
    int? ratingMin,
    int? ratingMax,
    int? minLikes,
    bool? hasPhoto,
    String? visibility,
    String? sort,
    int? page,
  }) async => CheckinPage(page: page ?? 1);
}

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

Widget _app(Widget home, ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
);

void main() {
  testWidgets('the recap shows the year, totals and leaderboards', (
    tester,
  ) async {
    // Tall enough for the whole recap to be laid out at once: a ListView only
    // builds what it can reach, and this test reads from top to bottom.
    tester.view.physicalSize = const Size(400, 2400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _RecapApi(recaps: {_now: _fullRecap(_now)});

    await tester.pumpWidget(_app(const RecapScreen(), api));
    await tester.pumpAndSettle();

    // The hero: the year, the count and the still-running marker.
    expect(find.text('YEAR IN SODA'), findsOneWidget);
    expect(find.text('So far'), findsOneWidget);
    expect(find.text('34 check-ins'), findsOneWidget);

    // The counters the hero does not carry.
    expect(find.text('new styles'), findsOneWidget);
    expect(find.text('day streak'), findsOneWidget);

    // What you drank, and where.
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Soda Co'), findsOneWidget);
    expect(find.text('Cola · 12'), findsOneWidget);
    expect(find.text('Corner Shop'), findsOneWidget);

    // How you rated, and how that moved.
    expect(find.text('22 check-ins rated, averaging 4.25.'), findsOneWidget);
    expect(find.text('Higher than ${_now - 1}.'), findsOneWidget);

    // The flavour fingerprint, with its scope read from the payload and the
    // thin axis called out in words.
    expect(find.text('Fruit'), findsOneWidget);
    expect(find.text('Spice'), findsOneWidget);
    expect(find.text('few check-ins so far'), findsOneWidget);
    expect(
      find.textContaining('Built from all your check-ins, not just $_now.'),
      findsOneWidget,
    );

    // The badge list arrives inside the recap, already filtered to the year.
    expect(find.text('First Pour'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a year with nothing logged reads as so-far, not as a bug', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(const RecapScreen(), _RecapApi()));
    await tester.pumpAndSettle();

    expect(find.text('Nothing logged in $_now'), findsOneWidget);
    expect(find.textContaining('The year is still running'), findsOneWidget);
    expect(find.text('YEAR IN SODA'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a finished year with no data says so plainly', (tester) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      _app(RecapScreen(initialYear: _now - 3), _RecapApi()),
    );
    await tester.pumpAndSettle();

    expect(find.text('Nothing logged in ${_now - 3}'), findsOneWidget);
    expect(
      find.textContaining('No check-ins are recorded for this year'),
      findsOneWidget,
    );
  });

  testWidgets('a failed load offers a retry', (tester) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      _app(const RecapScreen(), _RecapApi(failing: true)),
    );
    await tester.pumpAndSettle();

    expect(find.text('Could not load your recap'), findsOneWidget);
    expect(find.text('Try again'), findsOneWidget);
    expect(find.text('offline'), findsOneWidget);
  });

  testWidgets('the year picker offers exactly the years with check-ins', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    // The current year is empty but its payload names the previous year as the
    // only other year with data, so that chip appears without any probe.
    final api = _RecapApi(
      recaps: {
        _now: YearRecap(year: _now, yearsWithData: [_now - 1, _now]),
        _now - 1: _fullRecap(_now - 1),
      },
    );
    await tester.pumpWidget(_app(const RecapScreen(), api));
    await tester.pumpAndSettle();

    expect(find.text('$_now'), findsOneWidget);
    expect(find.text('${_now - 1}'), findsOneWidget);
    expect(api.requested, [_now]);

    await tester.tap(find.text('${_now - 1}'));
    await tester.pumpAndSettle();

    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('34 check-ins'), findsOneWidget);
    // Year to date no longer applies to a finished year.
    expect(find.text('So far'), findsNothing);
    // A cached year is not fetched twice.
    expect(api.requested.where((year) => year == _now - 1).length, 1);
  });

  testWidgets('a year-scoped fingerprint says so in the footnote', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 2400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final scoped = YearRecap(
      year: _now,
      totals: const RecapTotals(checkins: 3),
      hasData: true,
      fingerprint: const RecapFingerprint(
        hasData: true,
        wholeHistory: false,
        axes: [
          RecapAxis(key: 'fruit', label: 'Fruit', score: 100, samples: 9),
        ],
      ),
    );
    await tester.pumpWidget(
      _app(const RecapScreen(), _RecapApi(recaps: {_now: scoped})),
    );
    await tester.pumpAndSettle();

    expect(
      find.textContaining('Built from your $_now check-ins.'),
      findsOneWidget,
    );
    expect(
      find.textContaining('Built from all your check-ins'),
      findsNothing,
    );
  });

  testWidgets('the profile shows a Year in Soda row that opens the recap', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _RecapApi(recaps: {_now: _fullRecap(_now)});
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          apiProvider.overrideWithValue(api),
          authProvider.overrideWith(_SignedInAuth.new),
        ],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const ProfileScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Year in Soda'), findsOneWidget);

    await tester.tap(find.text('Year in Soda'));
    await tester.pumpAndSettle();

    expect(find.text('YEAR IN SODA'), findsOneWidget);
    expect(find.text('34 check-ins'), findsOneWidget);
  });
}
