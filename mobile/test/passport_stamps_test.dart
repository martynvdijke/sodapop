import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/passport_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';
import 'package:sodapop/src/widgets/soda_format.dart';
import 'package:sodapop/src/widgets/soda_ui.dart';

class _StampsApi extends ApiClient {
  _StampsApi(this.data);

  final Passport data;
  int loads = 0;

  @override
  Future<Passport> passport() async {
    loads++;
    return data;
  }
}

const _fullPassport = Passport(
  stats: ProfileStats(checkins: 42, drinks: 18, toasts: 7, badges: 1),
  totals: PassportTotals(checkins: 42, styles: 6, venueKinds: 3, countries: 2),
  badges: [
    ProfileBadge(
      slug: 'first-pour',
      name: 'First Pour',
      description: 'Logged your first soda.',
      awardedAt: '2025-03-02T09:00:00Z',
    ),
  ],
  styles: [
    PassportStyle(name: 'Cola', count: 12, lastAt: '2026-10-26T09:30:00Z'),
    PassportStyle(name: 'Ginger Ale', count: 5, lastAt: '2026-08-14T18:00:00Z'),
  ],
  venueKinds: [
    PassportVenueKind(kind: 'shop', count: 4),
    PassportVenueKind(kind: 'vending', count: 9),
  ],
  countries: [
    PassportCountry(country: 'GB', count: 11),
    PassportCountry(country: 'Portugal', count: 8),
  ],
  recent: [
    PassportRecent(
      name: 'Cherry Cola',
      slug: 'cherry-cola',
      style: 'Cola',
      createdAt: '2026-10-27T20:00:00Z',
      rating: 17,
    ),
    PassportRecent(
      name: 'Lemon Fizz',
      slug: '',
      style: 'Lemonade',
      createdAt: '2026-10-26T12:00:00Z',
    ),
  ],
);

Future<void> _pumpPassport(
  WidgetTester tester,
  Passport passport, {
  PassportDrinkOpener? onOpenDrink,
  Size size = const Size(400, 1600),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(_StampsApi(passport))],
      child: MaterialApp(
        theme: sodaTheme(Brightness.light),
        home: PassportScreen(onOpenDrink: onOpenDrink),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('passport renders stamp totals, styles, places and countries', (
    tester,
  ) async {
    await _pumpPassport(tester, _fullPassport);

    expect(find.text('Collected'), findsOneWidget);
    expect(
      find.text('The styles, places and countries you have collected so far.'),
      findsOneWidget,
    );
    expect(
      tester.widget<Text>(find.byKey(const Key('stamp-total-styles'))).data,
      '6',
    );
    expect(
      tester
          .widget<Text>(find.byKey(const Key('stamp-total-venue-kinds')))
          .data,
      '3',
    );
    expect(
      tester.widget<Text>(find.byKey(const Key('stamp-total-countries'))).data,
      '2',
    );

    expect(
      find.widgetWithText(SodaSectionTitle, 'Styles collected'),
      findsOneWidget,
    );
    expect(find.text('Cola'), findsOneWidget);
    expect(find.text('Ginger Ale'), findsOneWidget);
    expect(find.text('12 pours'), findsOneWidget);
    expect(find.text('5 pours'), findsOneWidget);
    expect(find.text(sodaDate('2026-10-26T09:30:00Z')), findsOneWidget);
    expect(find.text(sodaDate('2026-08-14T18:00:00Z')), findsOneWidget);

    expect(
      find.widgetWithText(SodaSectionTitle, 'Venue kinds'),
      findsOneWidget,
    );
    expect(find.text('Shop'), findsOneWidget);
    expect(find.text('Vending machine'), findsOneWidget);
    expect(find.text('4'), findsOneWidget);
    expect(find.text('9'), findsOneWidget);

    expect(find.widgetWithText(SodaSectionTitle, 'Countries'), findsOneWidget);
    expect(find.text('GB'), findsOneWidget);
    expect(find.text('Portugal'), findsOneWidget);
    expect(find.text('11'), findsOneWidget);
    expect(find.text('8'), findsOneWidget);

    expect(
      find.widgetWithText(SodaSectionTitle, 'Recent pours'),
      findsOneWidget,
    );
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Lemon Fizz'), findsOneWidget);
    expect(
      find.text('Cola · ${sodaDate('2026-10-27T20:00:00Z')}'),
      findsOneWidget,
    );

    // Existing sections survive the new stamp area.
    expect(find.text('Check-ins'), findsOneWidget);
    expect(
      find.widgetWithText(SodaSectionTitle, 'Earned badges'),
      findsOneWidget,
    );
    expect(find.text('First Pour'), findsOneWidget);
    expect(find.text('See all badges'), findsOneWidget);
    expect(find.byType(RefreshIndicator), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('recent pours show quarter stars and the unrated label', (
    tester,
  ) async {
    await _pumpPassport(
      tester,
      const Passport(
        recent: [
          PassportRecent(name: 'Cherry Cola', slug: 'cherry-cola', rating: 17),
          PassportRecent(name: 'Half Star', slug: 'half-star', rating: 10),
          PassportRecent(name: 'Lemon Fizz', slug: 'lemon-fizz'),
        ],
      ),
    );

    expect(find.text('4.25'), findsOneWidget);
    expect(find.text('2.5'), findsOneWidget);
    expect(find.text('Not rated'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an empty passport shows a state for every stamp list', (
    tester,
  ) async {
    await _pumpPassport(tester, const Passport());

    expect(find.text('Collected'), findsOneWidget);
    expect(
      tester.widget<Text>(find.byKey(const Key('stamp-total-styles'))).data,
      '0',
    );
    expect(
      tester
          .widget<Text>(find.byKey(const Key('stamp-total-venue-kinds')))
          .data,
      '0',
    );
    expect(
      tester.widget<Text>(find.byKey(const Key('stamp-total-countries'))).data,
      '0',
    );
    expect(
      find.text('No style stamps yet. Your first check-in starts this list.'),
      findsOneWidget,
    );
    expect(
      find.text(
        'No venue stamps yet. Kinds of place appear when you check in somewhere.',
      ),
      findsOneWidget,
    );
    expect(
      find.text(
        'No country stamps yet. Add a venue with a country when you check in.',
      ),
      findsOneWidget,
    );
    expect(
      find.text('No pours yet. Your last check-ins will be listed here.'),
      findsOneWidget,
    );
    expect(find.text('No badges yet'), findsOneWidget);
    expect(find.text('See all badges'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('tapping a recent pour with a slug asks to open the drink', (
    tester,
  ) async {
    final opened = <(String, String)>[];
    await _pumpPassport(
      tester,
      const Passport(
        recent: [
          PassportRecent(
            name: 'Cherry Cola',
            slug: 'cherry-cola',
            style: 'Cola',
            rating: 17,
          ),
          PassportRecent(name: 'Lemon Fizz', slug: ''),
        ],
      ),
      onOpenDrink: (context, slug, name) => opened.add((slug, name)),
    );

    await tester.tap(find.text('Cherry Cola'));
    await tester.pumpAndSettle();

    expect(opened, [('cherry-cola', 'Cherry Cola')]);

    // A row without a slug stays inert rather than pushing an empty route.
    await tester.tap(find.text('Lemon Fizz'));
    await tester.pumpAndSettle();

    expect(opened, [('cherry-cola', 'Cherry Cola')]);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the stamp area holds together on a narrow screen', (
    tester,
  ) async {
    await _pumpPassport(tester, _fullPassport, size: const Size(320, 1600));

    expect(find.text('Collected'), findsOneWidget);
    expect(
      tester.widget<Text>(find.byKey(const Key('stamp-total-styles'))).data,
      '6',
    );
    expect(find.text('Vending machine'), findsOneWidget);
    expect(find.text('Portugal'), findsOneWidget);

    await tester.scrollUntilVisible(find.text('Lemon Fizz'), 300);
    await tester.pumpAndSettle();

    expect(find.text('Lemon Fizz'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
