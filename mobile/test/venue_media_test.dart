import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/venue_detail_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';
import 'package:sodapop/src/widgets/soda_format.dart';

const _photo = VenueMedia(
  id: 'm1',
  mediaId: 'media-photo',
  kind: 'photo',
  label: 'Fridge shelf',
  uploaderHandle: 'ada',
  createdAt: '2026-10-27T09:00:00Z',
);

class _VenueMediaApi extends ApiClient {
  _VenueMediaApi(this.rows);

  List<VenueMedia> rows;
  final List<String> deleted = [];
  int mediaCalls = 0;

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
  );

  @override
  Future<List<VenueMedia>> venueMedia(int id) async {
    mediaCalls++;
    return rows;
  }

  @override
  Future<List<SavedVenue>> savedVenues() async => const [];

  @override
  Future<void> deleteVenueMedia(String id) async {
    deleted.add(id);
    rows = rows.where((row) => row.id != id).toList();
  }
}

class _SignedInVenueApi extends _VenueMediaApi {
  _SignedInVenueApi() : super(const []);

  @override
  bool get configured => true;

  @override
  Map<String, String> get imageHeaders => const {
    'Authorization': 'Bearer sd_test_token',
  };
}

Future<void> _pumpWith(
  WidgetTester tester,
  ApiClient api, {
  Size size = const Size(420, 1000),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

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
}

Future<_VenueMediaApi> _pumpVenue(
  WidgetTester tester,
  List<VenueMedia> rows,
) async {
  final api = _VenueMediaApi(rows);
  await _pumpWith(tester, api);
  return api;
}

void main() {
  testWidgets('gallery shows kind, label, uploader and date', (tester) async {
    const menu = VenueMedia(
      id: 'm2',
      mediaId: 'media-menu',
      kind: 'menu',
      label: 'Drinks menu',
      uploaderHandle: 'grace',
      createdAt: '2026-10-26T09:00:00Z',
    );
    await _pumpVenue(tester, const [_photo, menu]);

    expect(find.text('Photos and menus'), findsOneWidget);
    expect(find.text('Photo'), findsOneWidget);
    expect(find.text('Menu'), findsOneWidget);
    expect(find.text('Fridge shelf'), findsOneWidget);
    expect(find.text('Drinks menu'), findsOneWidget);
    expect(find.text('@ada · ${sodaDate(_photo.createdAt)}'), findsOneWidget);
    expect(find.text('@grace · ${sodaDate(menu.createdAt)}'), findsOneWidget);

    // Nobody here can remove anything, and the visitor is not signed in.
    expect(find.byTooltip('Remove photo'), findsNothing);
    expect(find.byTooltip('Remove menu'), findsNothing);
    expect(find.text('Add photo'), findsNothing);
    expect(find.text('Add menu'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('remove is offered only for own media and refreshes', (
    tester,
  ) async {
    const other = VenueMedia(
      id: 'm2',
      mediaId: 'media-other',
      kind: 'photo',
      label: 'Window shelf',
      uploaderHandle: 'grace',
      createdAt: '2026-10-26T09:00:00Z',
    );
    const mine = VenueMedia(
      id: 'm1',
      mediaId: 'media-photo',
      kind: 'photo',
      label: 'Fridge shelf',
      uploaderHandle: 'ada',
      createdAt: '2026-10-27T09:00:00Z',
      canRemove: true,
    );
    final api = await _pumpVenue(tester, const [mine, other]);

    expect(find.byTooltip('Remove photo'), findsOneWidget);

    await tester.tap(find.byTooltip('Remove photo'));
    await tester.pumpAndSettle();
    expect(find.text('Remove this photo?'), findsOneWidget);

    await tester.tap(find.text('Remove'));
    await tester.pumpAndSettle();

    expect(api.deleted, ['m1']);
    expect(api.mediaCalls, 2);
    expect(find.text('Fridge shelf'), findsNothing);
    expect(find.text('Window shelf'), findsOneWidget);
    expect(find.byTooltip('Remove photo'), findsNothing);

    // Let the confirmation snackbar time out so no timer outlives the test.
    await tester.pump(const Duration(seconds: 5));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });

  testWidgets('empty gallery explains that photos and menus live here', (
    tester,
  ) async {
    await _pumpVenue(tester, const []);

    expect(find.text('No photos or menus yet'), findsOneWidget);
    expect(
      find.text('Photos and menus added by visitors show up here.'),
      findsOneWidget,
    );
    expect(find.text('Photos did not load'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('signed-in members see add actions on a narrow phone', (
    tester,
  ) async {
    await _pumpWith(tester, _SignedInVenueApi(), size: const Size(320, 1000));

    expect(find.text('Add photo'), findsOneWidget);
    expect(find.text('Add menu'), findsOneWidget);
    expect(
      find.text(
        'Add a photo of the shelf or the menu so the next visitor knows what to look for.',
      ),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });
}
