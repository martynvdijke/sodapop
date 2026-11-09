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
import 'package:sodapop/src/screens/checkin_detail_screen.dart';
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

final _photoBytes = base64Decode(_onePixelPng);

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

Future<XFile?> _pickPhoto(ImageSource source) async =>
    XFile.fromData(_photoBytes, path: 'can.png', mimeType: 'image/png');

class _Call {
  _Call({
    required this.drinkId,
    required this.rating,
    required this.note,
    required this.venueId,
    required this.serving,
    required this.visibility,
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
  final Uint8List? photo;
  final String? photoFilename;
  final String? photoMime;
  final double? lat;
  final double? lng;
  final List<String> friendIds;
}

class _Api extends ApiClient {
  final calls = <_Call>[];
  String? detailId;

  @override
  Future<FriendsDirectory> friends() async => const FriendsDirectory(
    friends: [
      FriendUser(id: 'u-ada', handle: 'ada', displayName: 'Ada Lovelace'),
      FriendUser(id: 'u-grace', handle: 'grace', displayName: 'Grace Hopper'),
    ],
  );

  @override
  Future<List<Venue>> venues(
    String query, {
    double? lat,
    double? lng,
    int? radiusM,
  }) async => const [
    Venue(id: 7, name: 'Corner Shop', kind: 'shop', city: 'Leeds'),
  ];

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
        photo: photo,
        photoFilename: photoFilename,
        photoMime: photoMime,
        lat: lat,
        lng: lng,
        friendIds: friendIds,
      ),
    );
    return 'c9';
  }

  @override
  Future<CheckinDetail> getCheckin(String id) async {
    detailId = id;
    return CheckinDetail(
      id: id,
      handle: 'ada',
      displayName: 'Ada Lovelace',
      initial: 'A',
      drinkSlug: 'cherry-cola',
      drinkName: 'Cherry Cola',
      rating: 10,
      note: 'Cold and sharp.',
      createdAt: '2026-10-30T12:00:00Z',
      canToast: true,
      canComment: true,
    );
  }

  @override
  Future<CommentPage> commentPage(
    String checkinId, {
    String? before,
    int limit = 50,
  }) async => const CommentPage();
}

class _Host extends StatelessWidget {
  const _Host();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Builder(
        builder: (context) => Center(
          child: FilledButton(
            onPressed: () async {
              final saved = await showModalBottomSheet<CheckinSave>(
                context: context,
                isScrollControlled: true,
                useSafeArea: true,
                backgroundColor: Theme.of(context).colorScheme.surface,
                builder: (_) => CheckinSheet(
                  drink: _drink,
                  pickImage: _pickPhoto,
                  locationProvider: () async => _fix(),
                  permissionChecker: () async => LocationPermission.whileInUse,
                  serviceChecker: () async => true,
                ),
              );
              if (!context.mounted || saved == null || saved.id.isEmpty) {
                return;
              }
              await CheckinDetailScreen.open(context, saved.id);
            },
            child: const Text('Open check-in'),
          ),
        ),
      ),
    );
  }
}

Future<void> _tapVisible(WidgetTester tester, String label) async {
  final finder = find.text(label);
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('one submit carries photo, place, tags and opens the detail', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1600);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _Api();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const _Host(),
        ),
      ),
    );
    await tester.tap(find.text('Open check-in'));
    await tester.pumpAndSettle();

    // Rating: tapping the track centre selects 10 quarter-stars (2.5).
    await tester.tapAt(tester.getRect(find.byType(Slider)).center);
    await tester.pumpAndSettle();
    expect(find.text('2.5'), findsOneWidget);

    await _tapVisible(tester, 'Bottle');
    await _tapVisible(tester, 'Friends');
    await tester.enterText(
      find.byKey(const Key('checkin-note')),
      'Cold and sharp.',
    );

    await _tapVisible(tester, 'Tag friends');
    await tester.tap(find.byKey(const Key('friend-pick-u-ada')));
    await tester.tap(find.byKey(const Key('friend-pick-u-grace')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('friend-picker-done')));
    await tester.pumpAndSettle();

    await _tapVisible(tester, 'Use my location');
    expect(find.byKey(const Key('checkin-location-chip')), findsOneWidget);

    await _tapVisible(tester, 'Choose a venue');
    await tester.enterText(find.byKey(const Key('venue-query')), 'Corner');
    await tester.testTextInput.receiveAction(TextInputAction.search);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Corner Shop'));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('venue-chip')), findsOneWidget);

    await _tapVisible(tester, 'Add a photo');
    await _tapVisible(tester, 'Choose from gallery');
    expect(find.text('Photo attached'), findsOneWidget);

    await _tapVisible(tester, 'Save check-in');
    await tester.pumpAndSettle();

    expect(api.calls, hasLength(1));
    final call = api.calls.single;
    expect(call.drinkId, 9);
    expect(call.rating, 10);
    expect(call.note, 'Cold and sharp.');
    expect(call.serving, 'bottle');
    expect(call.visibility, 'friends');
    expect(call.venueId, 7);
    expect(call.lat, closeTo(51.50722, 0.00001));
    expect(call.lng, closeTo(-0.1275, 0.00001));
    expect(call.friendIds, ['u-ada', 'u-grace']);
    expect(call.photo, _photoBytes);
    expect(call.photoFilename, 'can.png');
    expect(call.photoMime, 'image/png');

    // The sheet closed and the host landed on the created check-in.
    expect(find.text('Save check-in'), findsNothing);
    expect(find.byType(CheckinDetailScreen), findsOneWidget);
    expect(api.detailId, 'c9');
    expect(tester.takeException(), isNull);
  });
}
