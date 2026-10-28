import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/compare_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _cherry = Drink(
  id: 1,
  slug: 'cherry-cola',
  name: 'Cherry Cola',
  brand: 'Fizz Co',
  style: 'Cola',
  container: 'can',
  volumeMl: 330,
  caffeineMgPer100ml: 10,
  sugarGPer100ml: 10.5,
  caloriesKcalPer100ml: 42,
  sweetener: 'none',
  carbonation: 'high',
  acid: 'phosphoric',
  status: 'active',
  rating: 4.5,
  raterCount: 12,
  ratingShown: true,
  flavours: [DrinkFlavour(name: 'Cherry', intensity: 3)],
);

const _ginger = Drink(
  id: 2,
  slug: 'ginger-beer',
  name: 'Ginger Beer',
  brand: 'Root Works',
  style: 'Ginger',
  container: 'bottle',
  volumeMl: 275,
  sugarGPer100ml: 9,
  caloriesKcalPer100ml: 36,
  sweetener: 'none',
  carbonation: 'high',
  acid: 'citric',
  status: 'active',
  rating: 4,
  raterCount: 4,
  ratingShown: true,
  flavours: [DrinkFlavour(name: 'Ginger', intensity: 4)],
);

const _quiet = Drink(
  id: 3,
  slug: 'quiet-lemon',
  name: 'Quiet Lemon',
  brand: 'Still Co',
  style: 'Lemonade',
  raterCount: 2,
  ratingShown: false,
);

const _sparse = Drink(id: 4, slug: 'plain-seltzer', name: 'Plain Seltzer');

class _CompareApi extends ApiClient {
  _CompareApi(this.catalog);

  final Map<String, Drink> catalog;
  List<String>? lastCompare;
  int compareCalls = 0;
  Object? compareError;

  @override
  Future<List<Drink>> search(String query, {String? style}) async {
    final needle = query.trim().toLowerCase();
    return catalog.values
        .where(
          (drink) =>
              drink.name.toLowerCase().contains(needle) ||
              drink.brand.toLowerCase().contains(needle),
        )
        .toList();
  }

  @override
  Future<List<Drink>> compare(List<String> slugs) async {
    compareCalls++;
    lastCompare = List.of(slugs);
    final failure = compareError;
    if (failure != null) throw failure;
    return [
      for (final slug in slugs)
        if (catalog[slug] != null) catalog[slug]!,
    ];
  }
}

Future<_CompareApi> _pump(
  WidgetTester tester, {
  List<Drink> drinks = const [],
  List<String> slugs = const [],
  Size size = const Size(430, 1200),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final api = _CompareApi({for (final drink in drinks) drink.slug: drink});
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: MaterialApp(
        theme: sodaTheme(Brightness.light),
        home: CompareScreen(initialSlugs: slugs),
      ),
    ),
  );
  await tester.pumpAndSettle();
  return api;
}

Future<void> _searchAndAdd(
  WidgetTester tester,
  String query,
  String name,
) async {
  await tester.enterText(find.byKey(const Key('compare-search')), query);
  await tester.tap(find.byTooltip('Search'));
  await tester.pumpAndSettle();
  await tester.tap(find.widgetWithText(ListTile, name));
  await tester.pumpAndSettle();
}

FilledButton _compareButton(WidgetTester tester) =>
    tester.widget<FilledButton>(find.byKey(const Key('compare-button')));

void main() {
  testWidgets('two drinks enable Compare and send their slugs', (tester) async {
    final api = await _pump(tester, drinks: [_cherry, _ginger]);

    expect(_compareButton(tester).onPressed, isNull);

    await _searchAndAdd(tester, 'cherry', 'Cherry Cola');
    expect(_compareButton(tester).onPressed, isNull);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('compare-search')))
          .controller
          ?.text,
      isEmpty,
    );

    await _searchAndAdd(tester, 'ginger', 'Ginger Beer');
    expect(_compareButton(tester).onPressed, isNotNull);
    expect(find.text('2 of 6'), findsOneWidget);

    await tester.tap(find.byKey(const Key('compare-button')));
    await tester.pumpAndSettle();

    expect(api.lastCompare, ['cherry-cola', 'ginger-beer']);
    expect(find.text('330 ml'), findsOneWidget);
    expect(find.text('10 mg'), findsOneWidget);
    expect(find.text('4.5 · 12 ratings'), findsOneWidget);
    expect(find.text('Cherry'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('six is the cap and a seventh add is explained', (tester) async {
    final drinks = [
      for (var index = 0; index < 7; index++)
        Drink(
          id: index + 1,
          slug: 'soda-$index',
          name: 'Soda $index',
          brand: 'Brand $index',
        ),
    ];
    await _pump(tester, drinks: drinks);

    for (var index = 0; index < 6; index++) {
      await _searchAndAdd(tester, 'soda', 'Soda $index');
    }

    expect(find.byType(InputChip), findsNWidgets(6));
    expect(
      find.text('Six selected. Remove one to add a different drink.'),
      findsOneWidget,
    );

    await _searchAndAdd(tester, 'soda', 'Soda 6');

    expect(find.byType(InputChip), findsNWidgets(6));
    expect(
      find.text(
        'You can compare six drinks at a time. Remove one to add another.',
      ),
      findsOneWidget,
    );
  });

  testWidgets('removing a chip updates the line-up and the button', (
    tester,
  ) async {
    await _pump(tester, drinks: [_cherry, _ginger]);
    await _searchAndAdd(tester, 'cherry', 'Cherry Cola');
    await _searchAndAdd(tester, 'ginger', 'Ginger Beer');

    await tester.tap(find.byTooltip('Remove Cherry Cola'));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const ValueKey('compare-chip-cherry-cola')),
      findsNothing,
    );
    expect(find.text('1 of 6'), findsOneWidget);
    expect(_compareButton(tester).onPressed, isNull);

    await tester.tap(find.byTooltip('Remove Ginger Beer'));
    await tester.pumpAndSettle();

    expect(
      find.byKey(const ValueKey('compare-chip-ginger-beer')),
      findsNothing,
    );
    expect(find.text('0 of 6'), findsOneWidget);
    expect(
      find.text('Add two to six drinks to see them side by side.'),
      findsOneWidget,
    );
  });

  testWidgets('missing values render as a dash', (tester) async {
    final api = await _pump(
      tester,
      drinks: [_cherry, _sparse],
      slugs: ['cherry-cola', 'plain-seltzer'],
    );

    expect(api.lastCompare, ['cherry-cola', 'plain-seltzer']);
    expect(find.text('—'), findsWidgets);
    expect(find.text('0 ml'), findsNothing);
    expect(find.text('0 g'), findsNothing);
    expect(find.text('0 kcal'), findsNothing);
  });

  testWidgets('a hidden rating shows the threshold copy', (tester) async {
    await _pump(
      tester,
      drinks: [_cherry, _quiet],
      slugs: ['cherry-cola', 'quiet-lemon'],
    );

    expect(find.text('Not enough ratings'), findsOneWidget);
    expect(find.text('4.5 · 12 ratings'), findsOneWidget);
    expect(find.text('2 ratings'), findsNothing);
  });

  testWidgets('a bad range maps to a plain message and keeps the line-up', (
    tester,
  ) async {
    final api = await _pump(tester, drinks: [_cherry, _ginger]);
    await _searchAndAdd(tester, 'cherry', 'Cherry Cola');
    await _searchAndAdd(tester, 'ginger', 'Ginger Beer');
    api.compareError = ApiException('bad request', statusCode: 400);

    await tester.tap(find.byKey(const Key('compare-button')));
    await tester.pumpAndSettle();

    expect(
      find.text('Choose between two and six drinks to compare.'),
      findsOneWidget,
    );
    expect(find.byType(InputChip), findsNWidgets(2));
    expect(find.text('Nothing to compare yet'), findsNothing);
    expect(_compareButton(tester).onPressed, isNotNull);
  });

  testWidgets('a failed comparison keeps the line-up and retries', (
    tester,
  ) async {
    final api = await _pump(tester, drinks: [_cherry, _ginger]);
    await _searchAndAdd(tester, 'cherry', 'Cherry Cola');
    await _searchAndAdd(tester, 'ginger', 'Ginger Beer');
    api.compareError = ApiException('The server did not answer');

    await tester.tap(find.byKey(const Key('compare-button')));
    await tester.pumpAndSettle();

    expect(find.text('The server did not answer'), findsOneWidget);
    expect(find.byType(InputChip), findsNWidgets(2));
    expect(find.text('Retry'), findsOneWidget);

    api.compareError = null;
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();

    expect(api.compareCalls, 2);
    expect(find.text('330 ml'), findsOneWidget);
  });

  testWidgets('a drink header opens its detail page', (tester) async {
    String? openedSlug;
    String? openedName;
    CompareScreen.drinkOpener = (context, slug, {name}) async {
      openedSlug = slug;
      openedName = name;
    };
    addTearDown(() => CompareScreen.drinkOpener = null);

    await _pump(
      tester,
      drinks: [_cherry, _ginger],
      slugs: ['cherry-cola', 'ginger-beer'],
    );

    await tester.tap(find.byKey(const ValueKey('compare-header-cherry-cola')));
    await tester.pumpAndSettle();

    expect(openedSlug, 'cherry-cola');
    expect(openedName, 'Cherry Cola');
  });

  testWidgets('the label column stays put while the table scrolls', (
    tester,
  ) async {
    const third = Drink(
      id: 5,
      slug: 'lime-soda',
      name: 'Lime Soda',
      brand: 'Fizz Co',
      style: 'Lime',
      volumeMl: 330,
    );
    await _pump(
      tester,
      drinks: [_cherry, _ginger, third],
      slugs: ['cherry-cola', 'ginger-beer', 'lime-soda'],
      size: const Size(430, 1200),
    );

    final horizontal = find.byWidgetPredicate(
      (widget) =>
          widget is SingleChildScrollView &&
          widget.scrollDirection == Axis.horizontal,
    );
    expect(horizontal, findsOneWidget);
    final before = tester.getTopLeft(find.text('Brand')).dx;

    await tester.drag(horizontal, const Offset(-100, 0));
    await tester.pumpAndSettle();

    expect(
      tester.getTopLeft(find.text('Brand')).dx,
      moreOrLessEquals(before, epsilon: 0.1),
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('wide layouts fit every column without sideways scrolling', (
    tester,
  ) async {
    const third = Drink(
      id: 5,
      slug: 'lime-soda',
      name: 'Lime Soda',
      brand: 'Fizz Co',
    );
    const fourth = Drink(
      id: 6,
      slug: 'root-beer',
      name: 'Root Beer',
      brand: 'Root Works',
    );
    await _pump(
      tester,
      drinks: [_cherry, _ginger, third, fourth],
      slugs: ['cherry-cola', 'ginger-beer', 'lime-soda', 'root-beer'],
      size: const Size(1000, 900),
    );

    final horizontal = find.byWidgetPredicate(
      (widget) =>
          widget is SingleChildScrollView &&
          widget.scrollDirection == Axis.horizontal,
    );
    expect(horizontal, findsNothing);
    expect(find.text('Swipe the table to see every drink.'), findsNothing);
    expect(find.text('Root Beer'), findsWidgets);
  });
}
