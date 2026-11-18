import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/saved_searches_screen.dart';
import 'package:sodapop/src/screens/settings_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _settings = AccountSettings(
  profile: SettingsProfile(
    handle: 'ada',
    displayName: 'Ada Lovelace',
    bio: 'Cherry cola, always.',
    email: 'ada@example.com',
    emailVerified: true,
    role: 'member',
  ),
  prefs: SettingsPrefs(
    theme: 'light',
    units: 'metric',
    emailWeekly: true,
    emailFriendReq: false,
    pushEnabled: false,
    publicProfile: true,
    analyticsOptOut: false,
    caffeineBedtime: '21:30',
    defaultVisibility: 'friends',
  ),
  tokenCount: 1,
  blockedCount: 0,
);

const _rows = [
  SavedSearch(
    id: 7,
    name: 'New colas',
    query: 'cola',
    beverageType: 'soda',
    styleSlug: 'cola-style',
    caffeineFree: true,
    notifyDrops: false,
    createdAt: '2026-11-18T09:00:00Z',
  ),
];

class _FakeSavedSearchApi extends ApiClient {
  _FakeSavedSearchApi({List<SavedSearch> rows = _rows}) : rows = List.of(rows);

  final List<SavedSearch> rows;
  final List<Map<String, Object?>> created = [];
  final List<int> deleted = [];
  int loads = 0;
  bool listFails = false;
  ApiException? createError;

  @override
  Future<List<SavedSearch>> savedSearches() async {
    loads += 1;
    if (listFails) throw ApiException('Cannot reach the server.');
    return List.of(rows);
  }

  @override
  Future<SavedSearch> createSavedSearch({
    String name = '',
    String query = '',
    String beverageType = '',
    String? styleSlug,
    String? brandSlug,
    String? flavourSlug,
    bool caffeineFree = false,
    bool sugarFree = false,
    bool notifyDrops = true,
  }) async {
    if (createError != null) throw createError!;
    created.add({
      'name': name,
      'query': query,
      'beverage_type': beverageType,
      'style_slug': styleSlug,
      'brand_slug': brandSlug,
      'flavour_slug': flavourSlug,
      'caffeine_free': caffeineFree,
      'sugar_free': sugarFree,
      'notify_drops': notifyDrops,
    });
    final row = SavedSearch(
      id: 100 + created.length,
      name: name,
      query: query,
      beverageType: beverageType,
      styleSlug: styleSlug,
      brandSlug: brandSlug,
      flavourSlug: flavourSlug,
      caffeineFree: caffeineFree,
      sugarFree: sugarFree,
      notifyDrops: notifyDrops,
    );
    rows.insert(0, row);
    return row;
  }

  @override
  Future<void> deleteSavedSearch(int id) async {
    deleted.add(id);
    rows.removeWhere((row) => row.id == id);
  }

  @override
  Future<AccountSettings> settings() async => _settings;

  @override
  Future<List<SettingsSession>> listSessions() async => const [];

  @override
  Future<List<BlockedUser>> blockedUsers() async => const [];

  @override
  Future<WishlistAlerts> wishlistAlerts() async => const WishlistAlerts();
}

Widget _app(ApiClient api, {Widget? home}) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: home ?? const SavedSearchesScreen(),
  ),
);

void main() {
  testWidgets('lists saved searches with pills, notify state and count', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(600, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FakeSavedSearchApi()));
    await tester.pumpAndSettle();

    expect(find.text('New colas'), findsOneWidget);
    expect(find.text('Alerts off'), findsOneWidget);
    expect(find.text('“cola”'), findsOneWidget);
    expect(find.text('Soda'), findsOneWidget);
    expect(find.text('Style · Cola style'), findsOneWidget);
    expect(find.text('Caffeine-free'), findsOneWidget);
    final savedHint = find.byWidgetPredicate(
      (widget) =>
          widget is Text &&
          (widget.data ?? '').startsWith('Saved ') &&
          widget.data != 'Saved searches',
    );
    expect(savedHint, findsOneWidget);
    expect(
      find.text(
        '1 of 25 used. Alerts are sent at most once a day per '
        'search.',
      ),
      findsOneWidget,
    );
    expect(find.byKey(const Key('saved-search-delete-7')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('empty state explains what a saved search can be', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(600, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FakeSavedSearchApi(rows: const [])));
    await tester.pumpAndSettle();

    expect(find.text('No saved searches yet'), findsOneWidget);
    expect(find.textContaining('as loose as a beverage type'), findsOneWidget);
    expect(find.byKey(const Key('saved-search-delete-7')), findsNothing);
  });

  testWidgets('creating requires at least one filter', (tester) async {
    tester.view.physicalSize = const Size(600, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeSavedSearchApi(rows: const []);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.ensureVisible(find.byKey(const Key('saved-search-submit')));
    await tester.tap(find.byKey(const Key('saved-search-submit')));
    await tester.pumpAndSettle();

    expect(find.text('Add at least one filter before saving.'), findsOneWidget);
    expect(api.created, isEmpty);
  });

  testWidgets('a valid draft is sent and lands in the list', (tester) async {
    tester.view.physicalSize = const Size(600, 2200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeSavedSearchApi(rows: const []);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(const Key('saved-search-name')),
      'Cherry drops',
    );
    await tester.enterText(
      find.byKey(const Key('saved-search-query')),
      'cherry',
    );
    await tester.enterText(
      find.byKey(const Key('saved-search-brand')),
      'soda-co',
    );
    await tester.ensureVisible(find.byKey(const Key('saved-search-submit')));
    await tester.tap(find.byKey(const Key('saved-search-submit')));
    await tester.pumpAndSettle();

    expect(api.created, hasLength(1));
    expect(api.created.single['name'], 'Cherry drops');
    expect(api.created.single['query'], 'cherry');
    expect(api.created.single['brand_slug'], 'soda-co');
    expect(api.created.single['notify_drops'], isTrue);
    // The new row appears at the top, and the form is reset.
    expect(find.text('Cherry drops'), findsOneWidget);
    expect(find.text('Brand · Soda co'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the 25-search cap surfaces delete-one-first', (tester) async {
    tester.view.physicalSize = const Size(600, 2200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeSavedSearchApi(rows: const [])
      ..createError = ApiException(
        'saved search limit reached',
        statusCode: 400,
        code: 'limit_reached',
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(find.byKey(const Key('saved-search-query')), 'cola');
    await tester.ensureVisible(find.byKey(const Key('saved-search-submit')));
    await tester.tap(find.byKey(const Key('saved-search-submit')));
    await tester.pumpAndSettle();

    expect(
      find.text('You have 25 saved searches. Delete one first.'),
      findsOneWidget,
    );
  });

  testWidgets('delete confirms, calls the API and drops the row', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(600, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeSavedSearchApi();
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('saved-search-delete-7')));
    await tester.pumpAndSettle();
    expect(find.text('Delete saved search?'), findsOneWidget);

    await tester.tap(find.byKey(const Key('saved-search-delete-confirm')));
    await tester.pumpAndSettle();

    expect(api.deleted, [7]);
    expect(find.text('New colas'), findsNothing);
    expect(find.text('No saved searches yet'), findsOneWidget);
  });

  testWidgets('settings links to the saved searches screen', (tester) async {
    tester.view.physicalSize = const Size(600, 1800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeSavedSearchApi(rows: const []);
    await tester.pumpWidget(_app(api, home: const SettingsScreen()));
    await tester.pumpAndSettle();

    final tile = find.byKey(const Key('settings-saved-searches'));
    await tester.scrollUntilVisible(
      tile,
      200,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.ensureVisible(tile);
    await tester.pumpAndSettle();
    await tester.tap(tile);
    await tester.pumpAndSettle();

    expect(find.byType(SavedSearchesScreen), findsOneWidget);
    expect(find.text('Your searches'), findsOneWidget);
    expect(find.text('New saved search'), findsOneWidget);
  });
}
