import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/discover_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _FakeDiscoverApi extends ApiClient {
  final List<Map<String, Object?>> created = [];
  ApiException? createError;

  @override
  Future<List<Drink>> search(String query, {String? style}) async => const [];

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
      'notify_drops': notifyDrops,
    });
    return SavedSearch(id: 42, name: name, query: query);
  }
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const DiscoverScreen(),
  ),
);

void main() {
  testWidgets('quick-save stays hidden until the search box has words', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FakeDiscoverApi()));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('discover-save-search')), findsNothing);

    await tester.enterText(find.byType(TextField).first, 'cherry');
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('discover-save-search')), findsOneWidget);
    expect(find.text('Save as drop alert'), findsOneWidget);
  });

  testWidgets('quick-save captures the typed words as the only filter', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeDiscoverApi();
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).first, '  cherry cola  ');
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('discover-save-search')));
    await tester.pumpAndSettle();

    expect(api.created, hasLength(1));
    expect(api.created.single['query'], 'cherry cola');
    expect(api.created.single['beverage_type'], '');
    expect(api.created.single['notify_drops'], isTrue);
    expect(find.textContaining('Drop alert saved'), findsOneWidget);
  });

  testWidgets('the 25-search cap shows a delete-one-first hint', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 900);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FakeDiscoverApi()
      ..createError = ApiException(
        'saved search limit reached',
        statusCode: 400,
        code: 'limit_reached',
      );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).first, 'cola');
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('discover-save-search')));
    await tester.pumpAndSettle();

    expect(api.created, isEmpty);
    expect(
      find.textContaining('Delete one in settings first.'),
      findsOneWidget,
    );
  });
}
