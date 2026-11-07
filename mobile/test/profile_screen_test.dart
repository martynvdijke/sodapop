import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/profile_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _defaultMe = Me(
  id: 'u1',
  handle: 'ada',
  displayName: 'Ada Lovelace',
  email: 'ada@example.com',
);

class _SignedInAuth extends AuthController {
  _SignedInAuth({this.me = _defaultMe});

  final Me me;

  @override
  AuthState build() => AuthState(
    loading: false,
    serverUrl: 'https://soda.example',
    token: 'sd_test_token_1234567890',
    me: me,
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
    this.pageSize,
  });

  final List<CheckinSummary> checkins;
  final MyStats stats;
  final int? pageSize;

  final List<Map<String, Object?>> calls = [];

  bool get filtered =>
      calls.isEmpty ||
      calls.last['from'] != null ||
      calls.last['visibility'] != null ||
      calls.last['sort'] != 'newest' ||
      calls.last['rating_min'] != null ||
      calls.last['min_likes'] != null ||
      calls.last['has_photo'] == true;

  @override
  Future<MyStats> myStats() async => stats;

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
  }) async {
    calls.add({
      'from': from,
      'to': to,
      'rating_min': ratingMin,
      'rating_max': ratingMax,
      'min_likes': minLikes,
      'has_photo': hasPhoto,
      'visibility': visibility,
      'sort': sort,
      'page': page,
    });
    if (filtered) {
      return CheckinPage(page: page ?? 1);
    }
    final size = pageSize;
    if (size == null || size <= 0) {
      return CheckinPage(items: checkins, page: page ?? 1);
    }
    final current = page ?? 1;
    final start = (current - 1) * size;
    final end = (start + size).clamp(0, checkins.length);
    final items = start >= checkins.length
        ? const <CheckinSummary>[]
        : checkins.sublist(start, end);
    return CheckinPage(
      items: items,
      hasNext: end < checkins.length,
      page: current,
    );
  }
}

Widget _app(ApiClient api, {Me me = _defaultMe}) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(() => _SignedInAuth(me: me)),
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
    expect(find.text('Check-ins'), findsWidgets);
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

    expect(find.text('Check-ins'), findsWidgets);
    expect(find.text('No check-ins yet'), findsOneWidget);
    expect(find.text('Scan a soda to start your log.'), findsOneWidget);
    expect(find.text('—'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the pinned favourite soda is shown on the profile', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      _app(
        _ProfileApi(),
        me: const Me(
          id: 'u1',
          handle: 'ada',
          displayName: 'Ada Lovelace',
          email: 'ada@example.com',
          favouriteDrink: FavouriteDrink(
            id: 7,
            name: 'Lemon Fizz',
            slug: 'lemon-fizz',
            brand: 'Soda Co',
            style: 'Lemonade',
            averageRating: 4.5,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('PINNED FAVOURITE'), findsOneWidget);
    expect(find.text('Lemon Fizz'), findsOneWidget);
    expect(find.text('Soda Co · Lemonade'), findsOneWidget);
    expect(find.text('4.5'), findsOneWidget);
  });

  testWidgets('check-ins paginate with Load more', (tester) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final rows = [
      for (var index = 0; index < 12; index++)
        CheckinSummary(
          id: 'c$index',
          drinkId: index,
          drinkName: 'Soda $index',
          rating: 8,
          createdAt: '2026-10-01T10:00:00Z',
        ),
    ];
    final api = _ProfileApi(checkins: rows, pageSize: 10);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('10 check-ins+'), findsOneWidget);
    expect(api.calls.single['page'], 1);

    await tester.ensureVisible(find.byKey(const Key('profile-load-more')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('profile-load-more')));
    await tester.pumpAndSettle();

    expect(api.calls.last['page'], 2);
    expect(find.byKey(const Key('profile-load-more')), findsNothing);
  });

  testWidgets('the filter sheet applies, shows chips, and resets', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _ProfileApi(
      checkins: const [
        CheckinSummary(
          id: 'c1',
          drinkId: 1,
          drinkName: 'Cherry Cola',
          rating: 13,
          createdAt: '2026-10-26T10:00:00Z',
        ),
      ],
    );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('profile-checkin-filter')));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('filter-range-30d')));
    await tester.ensureVisible(
      find.byKey(const Key('filter-visibility-friends')),
    );
    await tester.tap(find.byKey(const Key('filter-visibility-friends')));
    await tester.tap(find.byKey(const Key('filter-apply')));
    await tester.pumpAndSettle();

    expect(api.calls.last['from'], isNotNull);
    expect(api.calls.last['visibility'], 'friends');
    expect(find.widgetWithText(InputChip, 'Last 30 days'), findsOneWidget);
    expect(find.widgetWithText(InputChip, 'Friends'), findsOneWidget);
    expect(find.text('No check-ins match these filters'), findsOneWidget);

    await tester.tap(find.byKey(const Key('profile-checkin-filter')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('filter-reset')));
    await tester.tap(find.byKey(const Key('filter-apply')));
    await tester.pumpAndSettle();

    expect(api.calls.last['from'], isNull);
    expect(api.calls.last['visibility'], isNull);
    expect(api.calls.last['sort'], 'newest');
    expect(find.byKey(const Key('profile-clear-filters')), findsNothing);
    expect(find.text('Cherry Cola'), findsOneWidget);
  });
}
