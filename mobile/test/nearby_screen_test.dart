import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:geolocator/geolocator.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/nearby_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

Position _position({double lat = 51.5, double lng = -0.12}) => Position(
  latitude: lat,
  longitude: lng,
  timestamp: DateTime.utc(2026, 10, 28, 12),
  accuracy: 12,
  altitude: 0,
  altitudeAccuracy: 0,
  heading: 0,
  headingAccuracy: 0,
  speed: 0,
  speedAccuracy: 0,
);

NearbyItem _item(
  String id,
  String name,
  int distanceM, {
  String handle = 'ada',
  String displayName = 'Ada Lovelace',
  String venue = 'Corner Shop',
  double? rating = 17,
  String note = '',
  int toasts = 2,
  int comments = 1,
  String slug = 'cola',
}) => NearbyItem(
  checkin: FeedItem(
    id: id,
    handle: handle,
    displayName: displayName,
    drinkName: name,
    drinkSlug: slug,
    rating: rating,
    note: note,
    venueName: venue,
    createdAt: DateTime.now()
        .toUtc()
        .subtract(const Duration(minutes: 12))
        .toIso8601String(),
    toasts: toasts,
    comments: comments,
  ),
  distanceM: distanceM,
);

class _Query {
  const _Query(this.lat, this.lng, this.radiusKm, this.limit, this.before);

  final double lat;
  final double lng;
  final double radiusKm;
  final int limit;
  final String? before;
}

class _NearbyApi extends ApiClient {
  _NearbyApi(this.pages, {this.error});

  final List<NearbyPage> pages;
  final Object? error;
  final List<_Query> queries = [];
  int _index = 0;

  @override
  Future<NearbyPage> nearby({
    required double lat,
    required double lng,
    double radiusKm = 2,
    int limit = 20,
    String? before,
  }) async {
    queries.add(_Query(lat, lng, radiusKm, limit, before));
    final failure = error;
    if (failure != null) throw failure;
    if (pages.isEmpty) return const NearbyPage();
    final page = pages[_index < pages.length ? _index : pages.length - 1];
    _index++;
    return page;
  }
}

/// A page fetch that only completes when the test says so, so a radius change
/// can happen while the old page is still in flight.
class _GatedNearbyApi extends ApiClient {
  _GatedNearbyApi({
    required this.initial,
    required this.wider,
    required this.stale,
  });

  final NearbyPage initial;
  final NearbyPage wider;
  final NearbyPage stale;
  final List<_Query> queries = [];
  final Completer<NearbyPage> pending = Completer<NearbyPage>();

  @override
  Future<NearbyPage> nearby({
    required double lat,
    required double lng,
    double radiusKm = 2,
    int limit = 20,
    String? before,
  }) {
    queries.add(_Query(lat, lng, radiusKm, limit, before));
    if (radiusKm != 2) return Future.value(wider);
    if (before != null) return pending.future;
    return Future.value(initial);
  }
}

Widget _app(
  ApiClient api, {
  LocationProvider? location,
  Future<LocationPermission> Function()? permission,
  Future<bool> Function()? services,
  Future<bool> Function()? settings,
  Future<bool> Function()? serviceSettings,
}) {
  Future<LocationPermission> granted() async => LocationPermission.whileInUse;
  Future<bool> enabled() async => true;
  return ProviderScope(
    overrides: [apiProvider.overrideWithValue(api)],
    child: MaterialApp(
      theme: sodaTheme(Brightness.light),
      home: NearbyScreen(
        locationProvider: location ?? () async => _position(),
        permissionChecker: permission ?? granted,
        permissionRequester: permission ?? granted,
        serviceChecker: services ?? enabled,
        settingsOpener: settings,
        serviceSettingsOpener: serviceSettings,
      ),
    ),
  );
}

void _sizeTo(WidgetTester tester, Size size) {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

void main() {
  testWidgets('does not ask for location or data on load', (tester) async {
    _sizeTo(tester, const Size(360, 900));
    final api = _NearbyApi(const []);
    var locationCalls = 0;

    await tester.pumpWidget(
      _app(
        api,
        location: () async {
          locationCalls++;
          return _position();
        },
      ),
    );
    await tester.pumpAndSettle();

    expect(locationCalls, 0);
    expect(api.queries, isEmpty);
    expect(find.byKey(const Key('nearby-use-location')), findsOneWidget);
    expect(find.textContaining('never stored'), findsOneWidget);
    expect(find.textContaining('people you can already see'), findsOneWidget);
    expect(find.text('No location yet'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a location tap renders pours with bucketed distance', (
    tester,
  ) async {
    _sizeTo(tester, const Size(320, 1600));
    final api = _NearbyApi([
      NearbyPage(
        items: [
          _item(
            'c1',
            'Cherry Cola',
            80,
            rating: 17,
            note: 'Sharp and cold.',
            toasts: 3,
            comments: 2,
          ),
          _item(
            'c2',
            'Lime Soda',
            240,
            handle: 'grace',
            displayName: 'Grace Hopper',
            venue: 'Park Kiosk',
            rating: null,
          ),
          _item(
            'c3',
            'Ginger Beer',
            1150,
            handle: 'bob',
            displayName: 'Bob',
            rating: 12,
            venue: 'Market Hall',
          ),
        ],
      ),
    ]);

    await tester.pumpWidget(_app(api));
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();

    expect(api.queries, hasLength(1));
    expect(api.queries.single.lat, 51.5);
    expect(api.queries.single.lng, -0.12);
    expect(api.queries.single.radiusKm, 2);
    expect(api.queries.single.limit, 20);
    expect(api.queries.single.before, isNull);

    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('@ada'), findsOneWidget);
    expect(find.text('Corner Shop'), findsOneWidget);
    expect(find.text('Within 100 m'), findsOneWidget);
    expect(find.text('Within 300 m'), findsOneWidget);
    expect(find.text('Within 1.2 km'), findsOneWidget);
    expect(find.text('Sharp and cold.'), findsOneWidget);
    expect(find.text('4.25'), findsOneWidget);
    expect(find.text('Not rated'), findsOneWidget);
    expect(find.text('3 toasts'), findsOneWidget);
    expect(find.text('2 comments'), findsOneWidget);
    expect(find.textContaining('51.5'), findsNothing);
    expect(find.textContaining('-0.12'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('coordinates are rounded to three decimals before the request', (
    tester,
  ) async {
    _sizeTo(tester, const Size(360, 1000));
    final api = _NearbyApi([
      NearbyPage(items: [_item('c1', 'Cherry Cola', 80)]),
    ]);

    await tester.pumpWidget(
      _app(
        api,
        location: () async =>
            _position(lat: 51.5073518, lng: -0.1277583),
      ),
    );
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();

    expect(api.queries, hasLength(1));
    expect(api.queries.single.lat, 51.507);
    expect(api.queries.single.lng, -0.128);
    expect(api.queries.single.lat.toStringAsFixed(3), '51.507');
    expect(api.queries.single.lng.toStringAsFixed(3), '-0.128');
    expect(find.textContaining('51.507'), findsNothing);
    expect(find.textContaining('0.127'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an empty page suggests widening the radius', (tester) async {
    _sizeTo(tester, const Size(360, 1000));
    final api = _NearbyApi([
      const NearbyPage(),
      NearbyPage(items: [_item('c9', 'Root Beer', 400)]),
    ]);

    await tester.pumpWidget(_app(api));
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();

    expect(find.text('No nearby pours'), findsOneWidget);
    expect(find.textContaining('Widen the radius'), findsOneWidget);

    await tester.tap(find.byKey(const Key('nearby-widen')));
    await tester.pumpAndSettle();

    expect(api.queries, hasLength(2));
    expect(api.queries.last.radiusKm, 5);
    expect(find.text('Root Beer'), findsOneWidget);
  });

  testWidgets('loading more appends the next page without duplicates', (
    tester,
  ) async {
    _sizeTo(tester, const Size(400, 1400));
    final first = _item('c1', 'Cherry Cola', 80);
    final second = _item('c2', 'Lime Soda', 240, handle: 'grace');
    final third = _item('c3', 'Ginger Beer', 1500, handle: 'bob');
    final api = _NearbyApi([
      NearbyPage(items: [first, second], hasMore: true, nextBefore: 'cursor-1'),
      NearbyPage(items: [second, third]),
    ]);

    await tester.pumpWidget(_app(api));
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();
    expect(find.text('3 shown'), findsNothing);
    expect(find.text('2 shown'), findsOneWidget);

    await tester.tap(find.byKey(const Key('nearby-load-more')));
    await tester.pumpAndSettle();

    expect(api.queries, hasLength(2));
    expect(api.queries.last.before, 'cursor-1');
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Lime Soda'), findsOneWidget);
    expect(find.text('Ginger Beer'), findsOneWidget);
    expect(find.text('3 shown'), findsOneWidget);
    expect(find.byKey(const Key('nearby-load-more')), findsNothing);
  });

  testWidgets('a stale page from an old radius is discarded', (tester) async {
    _sizeTo(tester, const Size(400, 1400));
    final api = _GatedNearbyApi(
      initial: NearbyPage(
        items: [_item('c1', 'Cherry Cola', 80)],
        hasMore: true,
        nextBefore: 'cursor-1',
      ),
      wider: NearbyPage(items: [_item('c9', 'Fresh Pour', 400)]),
      stale: NearbyPage(items: [_item('c2', 'Stale Pour', 2400)]),
    );

    await tester.pumpWidget(_app(api));
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('1 shown'), findsOneWidget);

    // Start the next page, but leave its response hanging.
    await tester.tap(find.byKey(const Key('nearby-load-more')));
    await tester.pump();
    expect(api.queries, hasLength(2));
    expect(api.queries.last.before, 'cursor-1');

    // Widen the radius while the old page is still in flight.
    await tester.tap(find.byKey(const Key('nearby-radius-5')));
    await tester.pumpAndSettle();

    expect(api.queries, hasLength(3));
    expect(api.queries.last.radiusKm, 5);
    expect(find.text('Fresh Pour'), findsOneWidget);
    expect(find.text('Cherry Cola'), findsNothing);

    // The old page finally lands; it must not be appended to the new list.
    api.pending.complete(api.stale);
    await tester.pumpAndSettle();

    expect(find.text('Stale Pour'), findsNothing);
    expect(find.text('Fresh Pour'), findsOneWidget);
    expect(find.text('1 shown'), findsOneWidget);
    expect(api.queries, hasLength(3));
    expect(tester.takeException(), isNull);
  });

  testWidgets('changing the radius reloads once a location is set', (
    tester,
  ) async {
    _sizeTo(tester, const Size(360, 1000));
    final api = _NearbyApi([
      NearbyPage(items: [_item('c1', 'Cherry Cola', 80)]),
      NearbyPage(items: [_item('c2', 'Ginger Beer', 2400)]),
    ]);

    await tester.pumpWidget(_app(api));
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();
    expect(find.text('Cherry Cola'), findsOneWidget);

    await tester.tap(find.byKey(const Key('nearby-radius-5')));
    await tester.pumpAndSettle();

    expect(api.queries, hasLength(2));
    expect(api.queries.last.radiusKm, 5);
    expect(find.text('Ginger Beer'), findsOneWidget);
    expect(find.text('Cherry Cola'), findsNothing);
  });

  testWidgets('a failed page shows an error with retry', (tester) async {
    _sizeTo(tester, const Size(360, 1200));
    final api = _NearbyApi(const [], error: ApiException('The server is busy'));

    await tester.pumpWidget(_app(api));
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();

    expect(find.text('Could not load nearby pours'), findsOneWidget);
    expect(find.text('The server is busy'), findsOneWidget);
    expect(find.byKey(const Key('nearby-retry')), findsOneWidget);

    await tester.tap(find.byKey(const Key('nearby-retry')));
    await tester.pumpAndSettle();
    expect(api.queries, hasLength(2));
  });

  testWidgets('a location failure keeps the screen usable', (tester) async {
    _sizeTo(tester, const Size(360, 900));
    final api = _NearbyApi(const []);

    await tester.pumpWidget(
      _app(api, location: () async => throw Exception('no fix')),
    );
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();

    expect(
      find.text('Could not read your location. Try again.'),
      findsOneWidget,
    );
    expect(api.queries, isEmpty);
    expect(find.byKey(const Key('nearby-use-location')), findsOneWidget);
    expect(find.text('Use my location'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a permanent denial offers app settings', (tester) async {
    _sizeTo(tester, const Size(360, 900));
    final api = _NearbyApi(const []);
    var settingsCalls = 0;

    await tester.pumpWidget(
      _app(
        api,
        permission: () async => LocationPermission.deniedForever,
        settings: () async {
          settingsCalls++;
          return true;
        },
      ),
    );
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();

    expect(
      find.textContaining('Location is blocked for Sodapop'),
      findsOneWidget,
    );
    expect(api.queries, isEmpty);

    await tester.tap(find.byKey(const Key('nearby-open-settings')));
    await tester.pumpAndSettle();
    expect(settingsCalls, 1);
  });

  testWidgets('disabled location services point at the system toggle', (
    tester,
  ) async {
    _sizeTo(tester, const Size(360, 900));
    final api = _NearbyApi(const []);
    var settingsCalls = 0;

    await tester.pumpWidget(
      _app(
        api,
        services: () async => false,
        serviceSettings: () async {
          settingsCalls++;
          return true;
        },
      ),
    );
    await tester.tap(find.byKey(const Key('nearby-use-location')));
    await tester.pumpAndSettle();

    expect(find.textContaining('Location services are off'), findsOneWidget);

    await tester.tap(find.byKey(const Key('nearby-enable-services')));
    await tester.pumpAndSettle();
    expect(settingsCalls, 1);
    expect(api.queries, isEmpty);
  });
}
