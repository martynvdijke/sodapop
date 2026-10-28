import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
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
}

class _FakeApi extends ApiClient {
  _FakeApi({this.venueResults = const [], this.failure});

  final List<Venue> venueResults;
  final Object? failure;
  final calls = <_Call>[];
  String? venueQuery;

  @override
  Future<List<Venue>> venues(String query) async {
    venueQuery = query;
    return venueResults;
  }

  @override
  Future<void> createCheckin({
    required int drinkId,
    int? rating,
    String note = '',
    int? venueId,
    String? serving,
    String? visibility,
    String? idempotencyKey,
    Uint8List? photo,
    String? photoFilename,
    String? photoMime,
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
      ),
    );
    final error = failure;
    if (error != null) throw error;
  }
}

void _useTallScreen(WidgetTester tester) {
  tester.view.physicalSize = const Size(420, 1200);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

Future<void> _openSheet(
  WidgetTester tester, {
  required ApiClient api,
  CheckinImagePicker? pickImage,
  CheckinVideoPicker? pickVideo,
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
                onPressed: () => showModalBottomSheet<bool>(
                  context: context,
                  isScrollControlled: true,
                  backgroundColor: Theme.of(context).colorScheme.surface,
                  builder: (_) => CheckinSheet(
                    drink: _drink,
                    pickImage: pickImage,
                    pickVideo: pickVideo,
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
}
