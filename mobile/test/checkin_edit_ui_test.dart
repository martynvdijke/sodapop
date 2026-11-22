import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
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
);

/// A timestamp an hour ago, so fixtures stay inside the 24-hour edit window
/// instead of expiring as the calendar moves on.
String _recentCreatedAt() => DateTime.now()
    .toUtc()
    .subtract(const Duration(hours: 1))
    .toIso8601String();

/// A check-in well inside the 24-hour window with every editable field set.
CheckinDetail _existing({
  String? createdAt,
  int? priceCents = 250,
  String? priceCurrency = 'EUR',
}) => CheckinDetail(
  id: 'c1',
  handle: 'ada',
  displayName: 'Ada Lovelace',
  initial: 'A',
  drinkSlug: 'cherry-cola',
  drinkName: 'Cherry Cola',
  brand: 'Soda Co',
  style: 'Cola',
  rating: 10,
  note: 'Cold and sharp.',
  visibility: 'friends',
  createdAt: createdAt ?? _recentCreatedAt(),
  venueId: 7,
  venueName: 'Corner Shop',
  canDelete: true,
  canComment: true,
  priceCents: priceCents,
  priceCurrency: priceCurrency,
);

class _CreateCall {
  _CreateCall({
    required this.rating,
    required this.note,
    required this.priceCents,
    required this.priceCurrency,
  });

  final int? rating;
  final String note;
  final int? priceCents;
  final String? priceCurrency;
}

class _UpdateCall {
  _UpdateCall({
    required this.id,
    required this.rating,
    required this.clearRating,
    required this.serving,
    required this.note,
    required this.venueId,
    required this.clearVenue,
    required this.visibility,
    required this.priceCents,
    required this.clearPrice,
    required this.priceCurrency,
  });

  final String id;
  final int? rating;
  final bool clearRating;
  final String? serving;
  final String? note;
  final int? venueId;
  final bool clearVenue;
  final String? visibility;
  final int? priceCents;
  final bool clearPrice;
  final String? priceCurrency;
}

class _EditApi extends ApiClient {
  _EditApi({this.detail, this.updateError});

  CheckinDetail? detail;
  Object? updateError;
  int loads = 0;
  final createCalls = <_CreateCall>[];
  final updateCalls = <_UpdateCall>[];

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
    bool atHome = false,
    List<String> friendIds = const [],
    int? priceCents,
    String? priceCurrency,
  }) async {
    createCalls.add(
      _CreateCall(
        rating: rating,
        note: note,
        priceCents: priceCents,
        priceCurrency: priceCurrency,
      ),
    );
    return 'c-new';
  }

  @override
  Future<CheckinDetail> updateCheckin(
    String id, {
    int? rating,
    bool clearRating = false,
    String? serving,
    String? prepMethod,
    String? prepNote,
    String? note,
    int? venueId,
    bool clearVenue = false,
    String? visibility,
    int? priceCents,
    bool clearPrice = false,
    String? priceCurrency,
  }) async {
    updateCalls.add(
      _UpdateCall(
        id: id,
        rating: rating,
        clearRating: clearRating,
        serving: serving,
        note: note,
        venueId: venueId,
        clearVenue: clearVenue,
        visibility: visibility,
        priceCents: priceCents,
        clearPrice: clearPrice,
        priceCurrency: priceCurrency,
      ),
    );
    final error = updateError;
    if (error != null) throw error;
    return detail ?? _existing();
  }

  @override
  Future<CheckinDetail> getCheckin(String id) async {
    loads += 1;
    return detail ?? _existing();
  }

  @override
  Future<CommentPage> commentPage(
    String checkinId, {
    String? before,
    int limit = 50,
  }) async => const CommentPage();

  @override
  Future<List<Comment>> listComments(String checkinId) async => const [];
}

void _useTallScreen(WidgetTester tester) {
  tester.view.physicalSize = const Size(420, 1600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

Future<void> _pump(
  WidgetTester tester,
  ApiClient api,
  Widget home,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
    ),
  );
  await tester.pumpAndSettle();
}

/// Opens [CheckinSheet] through [showCheckinSheet] so the edit-mode wiring is
/// exercised end to end.
Future<void> _openSheet(
  WidgetTester tester,
  ApiClient api, {
  CheckinDetail? existing,
}) async {
  await _pump(
    tester,
    api,
    Scaffold(
      body: Builder(
        builder: (context) => Center(
          child: FilledButton(
            onPressed: () =>
                showCheckinSheet(context, _drink, existing: existing),
            child: const Text('Open sheet'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('Open sheet'));
  await tester.pumpAndSettle();
}

Future<void> _openDetail(WidgetTester tester, ApiClient api) async {
  await _pump(
    tester,
    api,
    Builder(
      builder: (context) => Scaffold(
        body: Center(
          child: TextButton(
            onPressed: () => CheckinDetailScreen.open(context, 'c1'),
            child: const Text('open check-in'),
          ),
        ),
      ),
    ),
  );
  await tester.tap(find.text('open check-in'));
  await tester.pumpAndSettle();
}

Future<void> _tapVisible(WidgetTester tester, String label) async {
  final finder = find.text(label);
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

/// The current text of a keyed [TextField], read from its controller so hint
/// text elsewhere on screen cannot be mistaken for the value.
String _textOf(WidgetTester tester, String key) =>
    tester.widget<TextField>(find.byKey(Key(key))).controller!.text;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  group('check-in sheet edit mode', () {
    testWidgets('seeds the fields and PATCHes instead of creating', (
      tester,
    ) async {
      _useTallScreen(tester);
      final api = _EditApi(detail: _existing());
      await _openSheet(tester, api, existing: _existing());

      expect(find.text('Edit check-in'), findsOneWidget);
      expect(find.text('2.5'), findsOneWidget);
      expect(find.text('Cold and sharp.'), findsOneWidget);
      expect(find.text('Corner Shop'), findsOneWidget);
      expect(_textOf(tester, 'checkin-price'), '2.50');
      expect(_textOf(tester, 'checkin-price-currency'), 'EUR');
      // PATCH has no photo field, so the attachment section is hidden.
      expect(find.text('Photo or video'), findsNothing);
      expect(find.text('Add a photo'), findsNothing);

      await _tapVisible(tester, 'Save changes');

      expect(api.createCalls, isEmpty);
      final call = api.updateCalls.single;
      expect(call.id, 'c1');
      expect(call.rating, 10);
      expect(call.clearRating, isFalse);
      expect(call.note, 'Cold and sharp.');
      expect(call.venueId, 7);
      expect(call.clearVenue, isFalse);
      expect(call.visibility, 'friends');
      expect(call.priceCents, 250);
      expect(call.priceCurrency, 'EUR');
      expect(call.clearPrice, isFalse);
      // Serving is not on the detail payload, so an untouched sheet leaves the
      // stored serving alone.
      expect(call.serving, isNull);
    });

    testWidgets('a serving picked in edit mode is sent', (tester) async {
      _useTallScreen(tester);
      final api = _EditApi(detail: _existing());
      await _openSheet(tester, api, existing: _existing());

      await _tapVisible(tester, 'Bottle');
      await _tapVisible(tester, 'Save changes');

      expect(api.updateCalls.single.serving, 'bottle');
    });

    testWidgets('blanking the price sends an explicit clear', (tester) async {
      _useTallScreen(tester);
      final api = _EditApi(detail: _existing());
      await _openSheet(tester, api, existing: _existing());

      await tester.enterText(find.byKey(const Key('checkin-price')), '');
      await _tapVisible(tester, 'Save changes');

      final call = api.updateCalls.single;
      expect(call.priceCents, isNull);
      expect(call.clearPrice, isTrue);
      expect(call.priceCurrency, isNull);
    });

    testWidgets('a closed window surfaces the friendly message', (
      tester,
    ) async {
      _useTallScreen(tester);
      final api = _EditApi(
        detail: _existing(),
        updateError: ApiException(
          'check-ins can only be edited within 24 hours',
          statusCode: 400,
          code: 'edit_window_closed',
        ),
      );
      await _openSheet(tester, api, existing: _existing());

      await _tapVisible(tester, 'Save changes');

      expect(
        find.text('Check-ins can be edited for 24 hours.'),
        findsOneWidget,
      );
      expect(find.text('Save changes'), findsOneWidget);
    });
  });

  group('price section', () {
    testWidgets('rejects malformed amounts without calling the API', (
      tester,
    ) async {
      _useTallScreen(tester);
      final api = _EditApi();
      await _openSheet(tester, api);

      await tester.enterText(find.byKey(const Key('checkin-price')), '1.234');
      await _tapVisible(tester, 'Save check-in');

      expect(find.text('Enter an amount like 2.50.'), findsOneWidget);
      expect(api.createCalls, isEmpty);
      expect(find.text('Save check-in'), findsOneWidget);
    });

    testWidgets('rejects an amount above the cap instead of clamping it', (
      tester,
    ) async {
      _useTallScreen(tester);
      final api = _EditApi();
      await _openSheet(tester, api);

      await tester.enterText(
        find.byKey(const Key('checkin-price')),
        '1000.01',
      );
      await _tapVisible(tester, 'Save check-in');

      expect(find.text('Enter an amount like 2.50.'), findsOneWidget);
      expect(api.createCalls, isEmpty);

      // Exactly at the cap is accepted.
      await tester.enterText(find.byKey(const Key('checkin-price')), '1000');
      await _tapVisible(tester, 'Save check-in');
      expect(api.createCalls.single.priceCents, 100000);
    });

    testWidgets('sends major units as cents and upper-cases the currency', (
      tester,
    ) async {
      _useTallScreen(tester);
      final api = _EditApi();
      await _openSheet(tester, api);

      await tester.enterText(find.byKey(const Key('checkin-price')), '2.50');
      await tester.enterText(
        find.byKey(const Key('checkin-price-currency')),
        'eur',
      );
      expect(_textOf(tester, 'checkin-price-currency'), 'EUR');

      await _tapVisible(tester, 'Save check-in');

      final call = api.createCalls.single;
      expect(call.priceCents, 250);
      expect(call.priceCurrency, 'EUR');
    });

    testWidgets('a currency alone does not create a price', (tester) async {
      _useTallScreen(tester);
      final api = _EditApi();
      await _openSheet(tester, api);

      await tester.enterText(
        find.byKey(const Key('checkin-price-currency')),
        'EUR',
      );
      await _tapVisible(tester, 'Save check-in');

      // A blank amount means "no price"; the currency is ignored with it.
      expect(api.createCalls.single.priceCents, isNull);
      expect(api.createCalls.single.priceCurrency, isNull);
    });

    testWidgets('a short currency code is a field error', (tester) async {
      _useTallScreen(tester);
      final api = _EditApi();
      await _openSheet(tester, api);

      await tester.enterText(find.byKey(const Key('checkin-price')), '2');
      await tester.enterText(
        find.byKey(const Key('checkin-price-currency')),
        'EU',
      );
      await _tapVisible(tester, 'Save check-in');

      expect(
        find.text('Use a three-letter code like EUR.'),
        findsOneWidget,
      );
      expect(api.createCalls, isEmpty);
    });

    testWidgets('remembers the last-used currency for the next check-in', (
      tester,
    ) async {
      _useTallScreen(tester);
      SharedPreferences.setMockInitialValues({
        'sodapop.price.currency.v1': 'GBP',
      });
      final api = _EditApi();
      await _openSheet(tester, api);

      expect(find.text('GBP'), findsOneWidget);
      expect(_textOf(tester, 'checkin-price-currency'), 'GBP');
    });
  });

  group('detail popup', () {
    testWidgets('offers Edit inside the 24-hour window', (tester) async {
      _useTallScreen(tester);
      final api = _EditApi(detail: _existing());
      await _openDetail(tester, api);

      await tester.tap(find.byTooltip('Check-in options'));
      await tester.pumpAndSettle();

      expect(find.text('Edit check-in'), findsOneWidget);

      await tester.tap(find.text('Edit check-in'));
      await tester.pumpAndSettle();

      // The sheet opened on the existing row, not on a new check-in.
      expect(find.text('Save changes'), findsOneWidget);
      expect(find.text('Cold and sharp.'), findsWidgets);
      expect(find.text('Log a soda'), findsNothing);
    });

    testWidgets('hides Edit once the window has closed', (tester) async {
      _useTallScreen(tester);
      final api = _EditApi(
        detail: _existing(createdAt: '2020-01-01T00:00:00Z'),
      );
      await _openDetail(tester, api);

      await tester.tap(find.byTooltip('Check-in options'));
      await tester.pumpAndSettle();

      expect(find.text('Edit check-in'), findsNothing);
      expect(find.text('Delete check-in'), findsOneWidget);
      expect(find.text('Report check-in'), findsOneWidget);
    });
  });
}
