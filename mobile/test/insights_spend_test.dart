import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/insights_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

/// Serves a canned insights payload per requested range and remembers what the
/// screen asked for, so a range switch can be checked end to end.
class _InsightsApi extends ApiClient {
  _InsightsApi({required this.byRange});

  final Map<int, Insights> byRange;

  /// Every range the screen fetched, in call order.
  final List<int> requested = [];

  @override
  Future<Insights> insights({int days = 30}) async {
    requested.add(days);
    return byRange[days] ?? Insights(rangeDays: days);
  }
}

Insights _insights({
  int days = 30,
  List<InsightSpend> spend = const [],
}) => Insights(
  rangeDays: days,
  pours: 6,
  sugarG: 120,
  caffeineMg: 90,
  caloriesKcal: 900,
  avgSugarPerDayG: 4,
  avgCaffeinePerDayMg: 3,
  series: const [
    InsightPoint(date: '2026-11-16', pours: 2, sugarG: 40, caffeineMg: 30),
    InsightPoint(date: '2026-11-17', pours: 4, sugarG: 80, caffeineMg: 60),
  ],
  spend: spend,
);

Widget _app(ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const InsightsScreen(),
  ),
);

/// A viewport tall enough that the whole list builds at once: the screen is a
/// ListView, which otherwise only builds what it can reach.
void _tallViewport(WidgetTester tester) {
  tester.view.physicalSize = const Size(420, 3200);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

void main() {
  testWidgets('spend lists one row per currency with amounts and counts', (
    tester,
  ) async {
    _tallViewport(tester);
    final api = _InsightsApi(
      byRange: {
        30: _insights(
          spend: const [
            InsightSpend(currency: 'EUR', totalCents: 1250, count: 3),
            InsightSpend(currency: 'USD', totalCents: 99, count: 1),
          ],
        ),
      },
    );

    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('Spend'), findsOneWidget);

    // Each currency row: code, amount in major units, priced-check-in count.
    expect(find.text('EUR'), findsOneWidget);
    expect(find.text('12.50'), findsOneWidget);
    expect(find.text('3 priced check-ins'), findsOneWidget);

    expect(find.text('USD'), findsOneWidget);
    expect(find.text('0.99'), findsOneWidget);
    expect(find.text('1 priced check-in'), findsOneWidget);

    // Server order is preserved: EUR (larger total) above USD.
    final euro = tester.getTopLeft(find.text('EUR')).dy;
    final dollar = tester.getTopLeft(find.text('USD')).dy;
    expect(euro, lessThan(dollar));

    expect(find.textContaining('No prices in this range'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an unpriced range shows a quiet empty state, not rows', (
    tester,
  ) async {
    _tallViewport(tester);
    final api = _InsightsApi(byRange: {30: _insights()});

    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('Spend'), findsOneWidget);
    expect(
      find.text('No prices in this range. A price on a check-in is totalled here.'),
      findsOneWidget,
    );
    expect(find.textContaining('priced check-in'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a priced check-in without a currency keeps its amount', (
    tester,
  ) async {
    _tallViewport(tester);
    final api = _InsightsApi(
      byRange: {
        30: _insights(
          spend: const [
            InsightSpend(currency: '', totalCents: 500, count: 2),
          ],
        ),
      },
    );

    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.text('No currency'), findsOneWidget);
    expect(find.text('5'), findsOneWidget);
    expect(find.text('2 priced check-ins'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('switching range refetches and redraws spend from the response', (
    tester,
  ) async {
    _tallViewport(tester);
    final api = _InsightsApi(
      byRange: {
        7: _insights(
          days: 7,
          spend: const [
            InsightSpend(currency: 'GBP', totalCents: 725, count: 2),
          ],
        ),
        30: _insights(
          spend: const [
            InsightSpend(currency: 'EUR', totalCents: 1250, count: 3),
          ],
        ),
      },
    );

    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(api.requested, [30]);
    expect(find.text('EUR'), findsOneWidget);
    expect(find.text('GBP'), findsNothing);

    await tester.tap(find.text('7 days'));
    await tester.pumpAndSettle();

    // The same fetch that refreshes the totals refreshes spend: no new request.
    expect(api.requested, [30, 7]);
    expect(find.text('GBP'), findsOneWidget);
    expect(find.text('7.25'), findsOneWidget);
    expect(find.text('2 priced check-ins'), findsOneWidget);
    expect(find.text('EUR'), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
