import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:geolocator/geolocator.dart';
import 'package:image_picker/image_picker.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/offline/checkin_outbox.dart';
import 'package:sodapop/src/screens/checkin_sheet.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _drink = Drink(
  id: 9,
  slug: 'cherry-cola',
  name: 'Cherry Cola',
  brand: 'Soda Co',
  style: 'Cola',
  volumeMl: 330,
);

const _onePixelPng =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAE'
    'hQGAhKmMIQAAAABJRU5ErkJggg==';

class _Call {
  _Call({
    required this.drinkId,
    required this.rating,
    required this.note,
    required this.venueId,
    required this.serving,
    required this.visibility,
    required this.idempotencyKey,
    required this.photo,
    required this.photoFilename,
    required this.photoMime,
    required this.lat,
    required this.lng,
    required this.friendIds,
  });

  final int drinkId;
  final int? rating;
  final String note;
  final int? venueId;
  final String? serving;
  final String? visibility;
  final String idempotencyKey;
  final Uint8List? photo;
  final String? photoFilename;
  final String? photoMime;
  final double? lat;
  final double? lng;
  final List<String> friendIds;
}

class _VenueCall {
  _VenueCall({
    required this.name,
    required this.address,
    required this.city,
    required this.country,
    required this.lat,
    required this.lng,
  });

  final String name;
  final String address;
  final String city;
  final String country;
  final double? lat;
  final double? lng;
}

class _FakeApi extends ApiClient {
  _FakeApi({
    this.venueResults = const [],
    this.friendResults = const [],
    this.failure,
  });

  final List<Venue> venueResults;
  final List<FriendUser> friendResults;
  final Object? failure;
  final calls = <_Call>[];
  final venueCalls = <_VenueCall>[];
  String? venueQuery;
  final venueQueries = <String>[];
  double? venueLat;
  double? venueLng;
  int? venueRadiusM;

  @override
  Future<List<Venue>> venues(
    String query, {
    double? lat,
    double? lng,
    int? radiusM,
  }) async {
    venueQuery = query;
    venueQueries.add(query);
    venueLat = lat;
    venueLng = lng;
    venueRadiusM = radiusM;
    return venueResults;
  }

  @override
  Future<FriendsDirectory> friends() async =>
      FriendsDirectory(friends: friendResults);

  @override
  Future<Venue> createVenue({
    required String name,
    String address = '',
    String city = '',
    String country = '',
    double? lat,
    double? lng,
  }) async {
    venueCalls.add(
      _VenueCall(
        name: name,
        address: address,
        city: city,
        country: country,
        lat: lat,
        lng: lng,
      ),
    );
    return Venue(
      id: 99,
      name: name,
      kind: 'shop',
      city: city,
      address: address,
      country: country,
      lat: lat ?? 0,
      lng: lng ?? 0,
    );
  }

  @override
  Future<String> createCheckin({
    required int drinkId,
    int? rating,
    String note = '',
    int? venueId,
    String? serving,
    String prepMethod = 'none',
    String prepNote = '',
    String? visibility,
    String? idempotencyKey,
    Uint8List? photo,
    String? photoFilename,
    String? photoMime,
    double? lat,
    double? lng,
    List<String> friendIds = const [],
  }) async {
    calls.add(
      _Call(
        drinkId: drinkId,
        rating: rating,
        note: note,
        venueId: venueId,
        serving: serving,
        visibility: visibility,
        idempotencyKey: idempotencyKey ?? '',
        photo: photo,
        photoFilename: photoFilename,
        photoMime: photoMime,
        lat: lat,
        lng: lng,
        friendIds: friendIds,
      ),
    );
    final error = failure;
    if (error != null) throw error;
    return 'c-9';
  }
}

void _useTallScreen(WidgetTester tester) {
  tester.view.physicalSize = const Size(420, 1500);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

Future<void> _openSheet(
  WidgetTester tester, {
  required ApiClient api,
  CheckinImagePicker? pickImage,
  CheckinVideoPicker? pickVideo,
  Future<Position> Function()? locationProvider,
  Future<LocationPermission> Function()? permissionChecker,
  Future<bool> Function()? serviceChecker,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: MaterialApp(
        theme: sodaTheme(Brightness.light),
        home: Scaffold(
          body: Builder(
            builder: (context) => Center(
              child: FilledButton(
                onPressed: () => showModalBottomSheet<CheckinSave>(
                  context: context,
                  isScrollControlled: true,
                  backgroundColor: Theme.of(context).colorScheme.surface,
                  builder: (_) => CheckinSheet(
                    drink: _drink,
                    pickImage: pickImage,
                    pickVideo: pickVideo,
                    locationProvider: locationProvider,
                    permissionChecker: permissionChecker,
                    serviceChecker: serviceChecker,
                  ),
                ),
                child: const Text('Open check-in'),
              ),
            ),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('Open check-in'));
  await tester.pumpAndSettle();
}

Position _fix({double lat = 51.50722, double lng = -0.1275}) => Position(
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

Future<void> _tapVisible(WidgetTester tester, String label) async {
  final finder = find.text(label);
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

Future<void> _save(WidgetTester tester) => _tapVisible(tester, 'Save check-in');

Future<void> _chooseVenue(
  WidgetTester tester,
  String query,
  String name,
) async {
  await _tapVisible(tester, 'Choose a venue');
  await tester.enterText(find.byKey(const Key('venue-query')), query);
  await tester.testTextInput.receiveAction(TextInputAction.search);
  await tester.pumpAndSettle();
  await tester.tap(find.text(name));
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('serving and visibility reach createCheckin', (tester) async {
    _useTallScreen(tester);
    final api = _FakeApi();
    await _openSheet(tester, api: api);

    await _tapVisible(tester, 'Bottle');
    await _tapVisible(tester, 'Friends');
    await tester.enterText(
      find.byKey(const Key('checkin-note')),
      'Cold and sharp.',
    );
    await _save(tester);

    expect(api.calls, hasLength(1));
    expect(api.calls.single.drinkId, 9);
    expect(api.calls.single.serving, 'bottle');
    expect(api.calls.single.visibility, 'friends');
    expect(api.calls.single.note, 'Cold and sharp.');
    expect(find.text('Cherry Cola'), findsNothing);
  });

  testWidgets('venue search sets the venue on the check-in', (tester) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      venueResults: const [
        Venue(id: 7, name: 'Corner Shop', kind: 'shop', city: 'Leeds'),
      ],
    );
    await _openSheet(tester, api: api);

    await _chooseVenue(tester, 'Corner', 'Corner Shop');

    expect(api.venueQuery, 'Corner');
    expect(find.byKey(const Key('venue-chip')), findsOneWidget);
    expect(find.text('Corner Shop'), findsOneWidget);

    await _save(tester);
    expect(api.calls.single.venueId, 7);
  });

  testWidgets('a venue chip can be removed', (tester) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      venueResults: const [
        Venue(id: 7, name: 'Corner Shop', kind: 'shop', city: 'Leeds'),
      ],
    );
    await _openSheet(tester, api: api);

    await _chooseVenue(tester, 'Corner', 'Corner Shop');
    await tester.tap(find.byTooltip('Remove venue'));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('venue-chip')), findsNothing);
    expect(find.text('Choose a venue'), findsOneWidget);

    await _save(tester);
    expect(api.calls.single.venueId, isNull);
  });

  testWidgets('the note accepts 1000 characters', (tester) async {
    _useTallScreen(tester);
    final api = _FakeApi();
    await _openSheet(tester, api: api);

    final field = tester.widget<TextField>(
      find.byKey(const Key('checkin-note')),
    );
    expect(field.maxLength, 1000);

    final note = 'a' * 1000;
    await tester.enterText(find.byKey(const Key('checkin-note')), note);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('checkin-note')))
          .controller!
          .text
          .length,
      1000,
    );

    await _save(tester);
    expect(api.calls.single.note.length, 1000);
  });

  testWidgets('a gallery photo previews and uploads with its filename', (
    tester,
  ) async {
    _useTallScreen(tester);
    final api = _FakeApi();
    final photo = base64Decode(_onePixelPng);
    ImageSource? sourceUsed;
    await _openSheet(
      tester,
      api: api,
      pickImage: (source) async {
        sourceUsed = source;
        return XFile.fromData(photo, path: 'can.png', mimeType: 'image/png');
      },
    );

    await _tapVisible(tester, 'Add a photo');
    await _tapVisible(tester, 'Choose from gallery');

    expect(sourceUsed, ImageSource.gallery);
    expect(find.text('Photo attached'), findsOneWidget);

    await _save(tester);
    expect(api.calls.single.photo, photo);
    expect(api.calls.single.photoFilename, 'can.png');
    expect(api.calls.single.photoMime, 'image/png');
  });

  testWidgets('a network failure queues serving, venue and visibility', (
    tester,
  ) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      venueResults: const [
        Venue(id: 7, name: 'Corner Shop', kind: 'shop', city: 'Leeds'),
      ],
      failure: ApiException(
        'Cannot reach the server. Check the address and try again.',
      ),
    );
    await _openSheet(tester, api: api);

    await _chooseVenue(tester, 'Corner', 'Corner Shop');
    await _tapVisible(tester, 'Bottle');
    await _tapVisible(tester, 'Anyone');
    await tester.enterText(
      find.byKey(const Key('checkin-note')),
      'Cellar cold.',
    );
    await _save(tester);

    expect(find.text('Cherry Cola'), findsNothing);
    expect(find.text('Saved offline'), findsOneWidget);

    final row = (await CheckinOutbox().pending()).single;
    expect(row['drink_id'], 9);
    expect(row['serving'], 'bottle');
    expect(row['venue_id'], 7);
    expect(row['visibility'], 'public');
    expect(row['note'], 'Cellar cold.');
    expect('${row['idempotency_key']}', isNotEmpty);

    final replay = _FakeApi();
    expect(await CheckinOutbox().flush(replay), 1);
    expect(replay.calls.single.serving, 'bottle');
    expect(replay.calls.single.venueId, 7);
    expect(replay.calls.single.visibility, 'public');
    expect(replay.calls.single.idempotencyKey, row['idempotency_key']);
  });

  testWidgets('a 4xx keeps the sheet open with the API message', (
    tester,
  ) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      failure: ApiException('Rating must be between 0 and 5', statusCode: 422),
    );
    await _openSheet(tester, api: api);

    await _save(tester);

    expect(find.text('Rating must be between 0 and 5'), findsOneWidget);
    expect(find.text('Save check-in'), findsOneWidget);
    expect(await CheckinOutbox().pending(), isEmpty);
  });

  testWidgets('video failures stay online and never enter the outbox', (
    tester,
  ) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      failure: ApiException('Cannot reach the server. Try again.'),
    );
    final video = Uint8List.fromList([
      0x00,
      0x00,
      0x00,
      0x18,
      0x66,
      0x74,
      0x79,
      0x70,
      0x69,
      0x73,
      0x6F,
      0x6D,
      0x00,
      0x00,
      0x02,
      0x00,
    ]);
    await _openSheet(
      tester,
      api: api,
      pickVideo: () async =>
          XFile.fromData(video, path: 'clip.mp4', mimeType: 'video/mp4'),
    );

    await _tapVisible(tester, 'Add a video');
    expect(find.text('Video attached'), findsOneWidget);

    await _save(tester);

    expect(
      find.text('Video check-ins need a connection. Try again when online.'),
      findsOneWidget,
    );
    expect(find.text('Save check-in'), findsOneWidget);
    expect(api.calls.single.photo, video);
    expect(api.calls.single.photoMime, 'video/mp4');
    expect(await CheckinOutbox().pending(), isEmpty);
  });

  testWidgets('tags friends and submits their ids', (tester) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      friendResults: const [
        FriendUser(id: 'u-ada', handle: 'ada', displayName: 'Ada Lovelace'),
        FriendUser(id: 'u-grace', handle: 'grace', displayName: 'Grace Hopper'),
      ],
    );
    await _openSheet(tester, api: api);

    await _tapVisible(tester, 'Tag friends');
    await tester.tap(find.byKey(const Key('friend-pick-u-ada')));
    await tester.tap(find.byKey(const Key('friend-pick-u-grace')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('friend-picker-done')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('checkin-friend-u-ada')), findsOneWidget);
    expect(find.byKey(const Key('checkin-friend-u-grace')), findsOneWidget);

    // A chip removes one friend without reopening the picker.
    await tester.tap(
      find.descendant(
        of: find.byKey(const Key('checkin-friend-u-grace')),
        matching: find.byTooltip('Remove tagged friend'),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('checkin-friend-u-grace')), findsNothing);

    await _save(tester);
    expect(api.calls.single.friendIds, ['u-ada']);
  });

  testWidgets('a long friend list can be filtered', (tester) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      friendResults: [
        for (var i = 0; i < 8; i++)
          FriendUser(
            id: 'u-$i',
            handle: 'friend$i',
            displayName: i == 7 ? 'Grace Hopper' : 'Friend $i',
          ),
      ],
    );
    await _openSheet(tester, api: api);

    await _tapVisible(tester, 'Tag friends');
    expect(find.byKey(const Key('friend-search')), findsOneWidget);

    await tester.enterText(find.byKey(const Key('friend-search')), 'grace');
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('friend-pick-u-7')), findsOneWidget);
    expect(find.byKey(const Key('friend-pick-u-0')), findsNothing);
  });

  testWidgets('creates a venue from the check-in and submits the fix', (
    tester,
  ) async {
    _useTallScreen(tester);
    final api = _FakeApi();
    await _openSheet(
      tester,
      api: api,
      locationProvider: () async => _fix(),
      permissionChecker: () async => LocationPermission.whileInUse,
      serviceChecker: () async => true,
    );

    await _tapVisible(tester, 'Use my location');
    expect(find.byKey(const Key('checkin-location-chip')), findsOneWidget);

    await _tapVisible(tester, 'Choose a venue');
    await tester.enterText(find.byKey(const Key('venue-query')), 'Corner');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pumpAndSettle();
    expect(api.venueLat, closeTo(51.50722, 0.00001));
    expect(api.venueLng, closeTo(-0.1275, 0.00001));
    expect(api.venueRadiusM, 25000);

    await tester.tap(find.byKey(const Key('venue-create')));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(const Key('venue-create-name')),
      'Corner Shop',
    );
    await tester.enterText(find.byKey(const Key('venue-create-city')), 'Leeds');
    await tester.tap(find.byKey(const Key('venue-create-submit')));
    await tester.pumpAndSettle();

    expect(api.venueCalls.single.name, 'Corner Shop');
    expect(api.venueCalls.single.city, 'Leeds');
    expect(api.venueCalls.single.lat, closeTo(51.50722, 0.00001));
    expect(find.byKey(const Key('venue-chip')), findsOneWidget);

    await _save(tester);
    expect(api.calls.single.venueId, 99);
    expect(api.calls.single.lat, closeTo(51.50722, 0.00001));
    expect(api.calls.single.lng, closeTo(-0.1275, 0.00001));
  });

  testWidgets('opens on venues near you and returns after clearing', (
    tester,
  ) async {
    _useTallScreen(tester);
    final api = _FakeApi(
      venueResults: const [
        Venue(
          id: 7,
          name: 'Corner Shop',
          kind: 'shop',
          city: 'Leeds',
          distanceM: 180,
        ),
      ],
    );
    await _openSheet(
      tester,
      api: api,
      locationProvider: () async => _fix(),
      permissionChecker: () async => LocationPermission.whileInUse,
      serviceChecker: () async => true,
    );

    await _tapVisible(tester, 'Use my location');
    await _tapVisible(tester, 'Choose a venue');

    // Opening with a fix browses nearby venues before any typing.
    expect(api.venueQueries, ['']);
    expect(api.venueLat, closeTo(51.50722, 0.00001));
    expect(api.venueRadiusM, 25000);
    expect(find.text('Venues near you'), findsOneWidget);
    expect(find.text('Corner Shop'), findsOneWidget);
    expect(find.textContaining('180 m'), findsOneWidget);

    // Typing switches to the distance-ordered search.
    await tester.enterText(find.byKey(const Key('venue-query')), 'Corner');
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pumpAndSettle();
    expect(api.venueQueries.last, 'Corner');
    expect(find.text('Search results'), findsOneWidget);

    // Clearing brings the browse list back.
    await tester.enterText(find.byKey(const Key('venue-query')), '');
    await tester.pumpAndSettle();
    expect(api.venueQueries, ['', 'Corner', '']);
    expect(find.text('Venues near you'), findsOneWidget);
    expect(find.text('Corner Shop'), findsOneWidget);
  });

  testWidgets('an empty nearby browse offers the create path', (tester) async {
    _useTallScreen(tester);
    final api = _FakeApi();
    await _openSheet(
      tester,
      api: api,
      locationProvider: () async => _fix(),
      permissionChecker: () async => LocationPermission.whileInUse,
      serviceChecker: () async => true,
    );

    await _tapVisible(tester, 'Use my location');
    await _tapVisible(tester, 'Choose a venue');

    expect(find.text('No venues within 25 km'), findsOneWidget);
    expect(find.text('Create a venue'), findsOneWidget);

    await tester.tap(find.text('Create a venue'));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('venue-create-name')), findsOneWidget);
  });
}
