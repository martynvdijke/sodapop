import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/list_detail_screen.dart';
import 'package:sodapop/src/screens/lists_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _wishlist = ListSummary(
  id: 'wish',
  name: 'Wishlist',
  description: 'Bottles to try next.',
  visibility: 'private',
  itemCount: 2,
  isSystem: true,
);

ListDetail _detail({
  String name = 'Summer bottles',
  List<ListItem> items = const [],
}) => ListDetail(
  id: 'l1',
  name: name,
  description: 'Cold ones for the balcony.',
  visibility: 'private',
  itemCount: items.length,
  items: items,
);

class _FakeListsApi extends ApiClient {
  _FakeListsApi({
    List<ListSummary>? lists,
    ListDetail? detail,
    this.searchResults = const [],
  }) : _lists = [...?lists],
       _detail = detail;

  final List<ListSummary> _lists;
  ListDetail? _detail;
  final List<Drink> searchResults;

  int listLoads = 0;
  int detailLoads = 0;
  String? createdName;
  String? createdDescription;
  String? createdVisibility;
  String? updatedId;
  String? updatedName;
  String? updatedDescription;
  String? updatedVisibility;
  String? deletedId;
  String? addedListId;
  int? addedDrinkId;
  String? removedListId;
  int? removedDrinkId;

  @override
  Future<List<ListSummary>> myLists() async {
    listLoads++;
    return List<ListSummary>.of(_lists);
  }

  @override
  Future<ListDetail> getList(String id) async {
    detailLoads++;
    final detail = _detail;
    if (detail == null) {
      throw ApiException('not found', statusCode: 404);
    }
    return detail;
  }

  @override
  Future<ListSummary> createList({
    required String name,
    String description = '',
    String visibility = 'private',
  }) async {
    createdName = name;
    createdDescription = description;
    createdVisibility = visibility;
    final created = ListSummary(
      id: 'l${_lists.length + 1}',
      name: name,
      description: description,
      visibility: visibility,
    );
    _lists.add(created);
    return created;
  }

  @override
  Future<ListSummary> updateList(
    String id, {
    String? name,
    String? description,
    String? visibility,
  }) async {
    updatedId = id;
    updatedName = name;
    updatedDescription = description;
    updatedVisibility = visibility;
    final updated = ListSummary(
      id: id,
      name: name ?? '',
      description: description ?? '',
      visibility: visibility ?? 'private',
    );
    final index = _lists.indexWhere((list) => list.id == id);
    if (index >= 0) _lists[index] = updated;
    final detail = _detail;
    if (detail != null && detail.id == id) {
      _detail = ListDetail(
        id: detail.id,
        name: updated.name,
        description: updated.description,
        visibility: updated.visibility,
        itemCount: detail.itemCount,
        isSystem: detail.isSystem,
        items: detail.items,
      );
    }
    return updated;
  }

  @override
  Future<void> deleteList(String id) async {
    deletedId = id;
    _lists.removeWhere((list) => list.id == id);
  }

  @override
  Future<List<Drink>> search(String query, {String? style}) async =>
      searchResults;

  @override
  Future<ListItem> addListItem(
    String listId, {
    int drinkId = 0,
    String slug = '',
    String note = '',
  }) async {
    addedListId = listId;
    addedDrinkId = drinkId;
    return ListItem(drinkId: drinkId, slug: slug, name: 'Cherry Cola');
  }

  @override
  Future<void> removeListItem(String listId, int drinkId) async {
    removedListId = listId;
    removedDrinkId = drinkId;
    final detail = _detail;
    if (detail == null) return;
    final items = detail.items
        .where((item) => item.drinkId != drinkId)
        .toList();
    _detail = ListDetail(
      id: detail.id,
      name: detail.name,
      description: detail.description,
      visibility: detail.visibility,
      itemCount: items.length,
      isSystem: detail.isSystem,
      items: items,
    );
  }
}

Future<void> _pump(WidgetTester tester, Widget home, _FakeListsApi api) async {
  tester.view.physicalSize = const Size(420, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('creating a list sends the form and reloads', (tester) async {
    final api = _FakeListsApi(lists: [_wishlist]);
    await _pump(tester, const ListsScreen(), api);
    final loadsBefore = api.listLoads;

    await tester.tap(find.text('New list'));
    await tester.pumpAndSettle();
    expect(find.text('Create list'), findsOneWidget);

    await tester.enterText(
      find.byKey(const Key('list-name-field')),
      'Summer bottles',
    );
    await tester.enterText(
      find.byKey(const Key('list-description-field')),
      'Cold ones for the balcony.',
    );
    await tester.tap(find.text('Friends'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('list-save-button')));
    await tester.pumpAndSettle();

    expect(api.createdName, 'Summer bottles');
    expect(api.createdDescription, 'Cold ones for the balcony.');
    expect(api.createdVisibility, 'friends');
    expect(api.listLoads, loadsBefore + 1);
    expect(find.text('Summer bottles'), findsOneWidget);
    expect(find.text('List created.'), findsOneWidget);
  });

  testWidgets('a blank name keeps the sheet open with a hint', (tester) async {
    final api = _FakeListsApi(lists: [_wishlist]);
    await _pump(tester, const ListsScreen(), api);

    await tester.tap(find.text('New list'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('list-save-button')));
    await tester.pumpAndSettle();

    expect(api.createdName, isNull);
    expect(find.text('Give the list a name.'), findsOneWidget);
    expect(find.byKey(const Key('list-name-field')), findsOneWidget);
  });

  testWidgets('editing a list sends the new name and visibility', (
    tester,
  ) async {
    final api = _FakeListsApi(
      lists: [
        _wishlist,
        const ListSummary(
          id: 'l1',
          name: 'Summer bottles',
          description: 'Cold ones for the balcony.',
          visibility: 'private',
          itemCount: 3,
        ),
      ],
    );
    await _pump(tester, const ListsScreen(), api);

    await tester.tap(find.byTooltip('Options for Summer bottles'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Edit'));
    await tester.pumpAndSettle();

    expect(find.text('Edit list'), findsOneWidget);
    final prefilled = tester.widget<TextField>(
      find.byKey(const Key('list-name-field')),
    );
    expect(prefilled.controller?.text, 'Summer bottles');

    await tester.enterText(
      find.byKey(const Key('list-name-field')),
      'Rainy day bottles',
    );
    await tester.tap(find.text('Public'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('list-save-button')));
    await tester.pumpAndSettle();

    expect(api.updatedId, 'l1');
    expect(api.updatedName, 'Rainy day bottles');
    expect(api.updatedVisibility, 'public');
    expect(api.updatedDescription, 'Cold ones for the balcony.');
    expect(find.text('Rainy day bottles'), findsOneWidget);
    expect(find.text('List updated.'), findsOneWidget);
  });

  testWidgets('deleting a list confirms, then calls deleteList', (
    tester,
  ) async {
    final api = _FakeListsApi(
      lists: [
        _wishlist,
        const ListSummary(
          id: 'l1',
          name: 'Summer bottles',
          visibility: 'private',
          itemCount: 3,
        ),
      ],
    );
    await _pump(tester, const ListsScreen(), api);

    await tester.tap(find.byTooltip('Options for Summer bottles'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();

    expect(find.text('Delete list?'), findsOneWidget);
    expect(find.textContaining('Summer bottles'), findsWidgets);

    await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
    await tester.pumpAndSettle();

    expect(api.deletedId, 'l1');
    expect(find.text('Summer bottles'), findsNothing);
    expect(find.text('List deleted.'), findsOneWidget);
  });

  testWidgets('a built-in list hides edit and delete', (tester) async {
    final api = _FakeListsApi(lists: [_wishlist]);
    await _pump(tester, const ListsScreen(), api);

    expect(find.text('Wishlist'), findsOneWidget);
    expect(find.text('Built in'), findsOneWidget);
    expect(find.byTooltip('Options for Wishlist'), findsNothing);
    expect(find.text('Edit'), findsNothing);
    expect(find.text('Delete'), findsNothing);
  });

  testWidgets('adding a soda searches the catalog and sends its id', (
    tester,
  ) async {
    final api = _FakeListsApi(
      detail: _detail(),
      searchResults: const [
        Drink(
          id: 7,
          slug: 'cherry-cola',
          name: 'Cherry Cola',
          brand: 'Soda Co',
        ),
      ],
    );
    await _pump(tester, const ListDetailScreen(listId: 'l1'), api);

    await tester.tap(find.byKey(const Key('add-soda-button')));
    await tester.pumpAndSettle();

    await tester.enterText(find.byKey(const Key('list-add-search')), 'cherry');
    await tester.tap(find.byTooltip('Search'));
    await tester.pumpAndSettle();

    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Soda Co'), findsOneWidget);

    await tester.tap(find.text('Cherry Cola'));
    await tester.pumpAndSettle();

    expect(api.addedListId, 'l1');
    expect(api.addedDrinkId, 7);
    expect(find.text('Added'), findsOneWidget);
  });

  testWidgets('an empty search shows a hint and no results copy', (
    tester,
  ) async {
    final api = _FakeListsApi(detail: _detail());
    await _pump(tester, const ListDetailScreen(listId: 'l1'), api);

    await tester.tap(find.byKey(const Key('add-soda-button')));
    await tester.pumpAndSettle();

    expect(
      find.text('Search the shared catalog by name or brand.'),
      findsOneWidget,
    );

    await tester.enterText(find.byKey(const Key('list-add-search')), 'zzz');
    await tester.tap(find.byTooltip('Search'));
    await tester.pumpAndSettle();

    expect(find.text('No sodas found'), findsOneWidget);
  });

  testWidgets('removing a soda confirms, then calls removeListItem', (
    tester,
  ) async {
    final api = _FakeListsApi(
      detail: _detail(
        items: const [
          ListItem(
            drinkId: 7,
            slug: 'cherry-cola',
            name: 'Cherry Cola',
            brand: 'Soda Co',
            note: 'Cold and sharp.',
          ),
        ],
      ),
    );
    await _pump(tester, const ListDetailScreen(listId: 'l1'), api);

    expect(find.text('Cold and sharp.'), findsOneWidget);

    await tester.tap(find.byTooltip('Options for Cherry Cola'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Remove'));
    await tester.pumpAndSettle();

    expect(find.text('Remove soda?'), findsOneWidget);

    await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
    await tester.pumpAndSettle();

    expect(api.removedListId, 'l1');
    expect(api.removedDrinkId, 7);
    expect(find.text('Cherry Cola'), findsNothing);
    expect(find.text('Removed from the list.'), findsOneWidget);
  });
}
