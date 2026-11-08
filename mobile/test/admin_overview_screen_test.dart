import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/admin_overview_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _counts = AdminOverviewCounts(
  checkins24h: 12,
  drinks24h: 3,
  users7d: 5,
  openReports: 0,
  pendingDrinks: 2,
);

const _rows = [
  AdminOverviewEvent(
    id: 'e1',
    kind: 'checkin',
    createdAt: '2026-11-08T10:00:00Z',
    actorId: 'u1',
    actorHandle: 'ada',
    actorName: 'Ada Lovelace',
    title: "Cherry Cola at Ruby's",
    detail: 'Rated 4.25',
    visibility: 'public',
    href: '/checkins/e1',
  ),
  AdminOverviewEvent(
    id: 'e2',
    kind: 'report',
    createdAt: '2026-11-08T09:00:00Z',
    actorHandle: '',
    title: 'Report on a comment',
    status: 'open',
    href: '/reports/e2',
  ),
];

class _FakeOverviewApi extends ApiClient {
  _FakeOverviewApi({
    this.page = const AdminOverviewPage(),
    this.forbidden = false,
  });

  AdminOverviewPage page;
  bool forbidden;
  AdminOverviewCounts counts = _counts;

  final List<String?> beforeCalls = [];
  final List<List<String>> kindCalls = [];
  int countCalls = 0;

  @override
  Future<AdminOverviewPage> adminOverview({
    String? before,
    int limit = 50,
    List<String> kinds = const [],
  }) async {
    beforeCalls.add(before);
    kindCalls.add(kinds);
    if (forbidden) throw ApiException('Not found', statusCode: 404);
    return page;
  }

  @override
  Future<AdminOverviewCounts> adminOverviewCounts() async {
    countCalls++;
    if (forbidden) throw ApiException('Not found', statusCode: 404);
    return counts;
  }
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const AdminOverviewScreen(),
  ),
);

Future<void> _pump(WidgetTester tester, _FakeOverviewApi api) async {
  tester.view.physicalSize = const Size(440, 1400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(_app(api));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('an empty feed shows the counts and the empty state', (
    tester,
  ) async {
    final api = _FakeOverviewApi();
    await _pump(tester, api);

    expect(find.text('At a glance'), findsOneWidget);
    expect(find.text('New sodas'), findsOneWidget);
    expect(find.text('New members'), findsOneWidget);
    expect(find.text('Pending sodas'), findsOneWidget);
    expect(find.text('Open reports'), findsOneWidget);

    expect(find.text('No activity yet'), findsOneWidget);
    expect(find.text('Could not load activity'), findsNothing);
    expect(api.beforeCalls, [null]);
    expect(api.kindCalls.single, isEmpty);
  });

  testWidgets('a 404 renders as admin access required', (tester) async {
    final api = _FakeOverviewApi(forbidden: true);
    await _pump(tester, api);

    expect(find.text('Admin access required'), findsOneWidget);
    expect(find.text('Try again'), findsNothing);
    expect(find.text('No activity yet'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('rows show the actor, title and a status pill', (tester) async {
    final api = _FakeOverviewApi(page: const AdminOverviewPage(items: _rows));
    await _pump(tester, api);

    expect(find.text('Check-in'), findsOneWidget);
    expect(find.text('@ada'), findsOneWidget);
    expect(find.text('Cherry Cola at Ruby\'s'), findsOneWidget);
    expect(find.text('Anyone'), findsOneWidget);

    expect(find.text('Report'), findsOneWidget);
    expect(find.text('system'), findsOneWidget);
    expect(find.text('Open'), findsOneWidget);

    // The cursor is empty, so there is nothing older to ask for.
    expect(find.byKey(const Key('admin-load-older')), findsNothing);

    await tester.tap(find.text('Cherry Cola at Ruby\'s'));
    await tester.pumpAndSettle();
    expect(find.text('Web link'), findsOneWidget);
    expect(find.text('Copy link'), findsOneWidget);
  });

  testWidgets('a filter reloads from the first page', (tester) async {
    final api = _FakeOverviewApi(page: const AdminOverviewPage(items: _rows));
    await _pump(tester, api);

    await tester.ensureVisible(find.byKey(const Key('admin-filter-report')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('admin-filter-report')));
    await tester.pumpAndSettle();

    expect(api.kindCalls.last, ['report']);
    expect(api.beforeCalls.last, isNull);
  });
}
