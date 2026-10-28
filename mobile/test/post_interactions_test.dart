import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/checkin_detail_screen.dart';
import 'package:sodapop/src/screens/feed_screen.dart';
import 'package:sodapop/src/screens/saved_posts_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _unsavedPour = FeedItem(
  id: 'c2',
  handle: 'grace',
  displayName: 'Grace Hopper',
  drinkName: 'Lime Soda',
  drinkSlug: 'lime-soda',
  rating: 16,
  venueName: 'Corner Shop',
  createdAt: '2026-10-26T10:00:00Z',
);

const _savedPour = FeedItem(
  id: 'c1',
  handle: 'ada',
  displayName: 'Ada Lovelace',
  drinkName: 'Cherry Cola',
  drinkSlug: 'cherry-cola',
  brand: 'Soda Co',
  rating: 13,
  saved: true,
  createdAt: '2026-10-25T10:00:00Z',
);

class _FeedApi extends ApiClient {
  _FeedApi({this.failSave = false});

  final bool failSave;
  final List<String> saved = [];
  final List<String> unsaved = [];

  @override
  Future<List<FeedItem>> feed({int limit = 30}) async =>
      const [_savedPour, _unsavedPour];

  @override
  Future<void> saveCheckin(String id) async {
    if (failSave) throw ApiException('Could not save this pour');
    saved.add(id);
  }

  @override
  Future<void> unsaveCheckin(String id) async {
    if (failSave) throw ApiException('Could not remove this save');
    unsaved.add(id);
  }
}

Comment _comment(
  String id,
  String body, {
  String handle = 'grace',
  bool canDelete = false,
}) => Comment(
  id: id,
  userId: 'u-$handle',
  handle: handle,
  displayName: handle == 'grace' ? 'Grace Hopper' : 'Ada Lovelace',
  body: body,
  createdAt: '2026-10-26T10:00:00Z',
  canDelete: canDelete,
);

class _DetailApi extends ApiClient {
  _DetailApi({
    this.saved = false,
    this.failSave = false,
    this.newest = const CommentPage(),
    this.older = const CommentPage(),
  });

  final bool saved;
  final bool failSave;
  final CommentPage newest;
  final CommentPage older;

  final List<String> savedCalls = [];
  final List<String> unsavedCalls = [];
  final List<String> earlierCursors = [];

  @override
  Future<CheckinDetail> getCheckin(String id) async => CheckinDetail(
    id: id,
    handle: 'ada',
    displayName: 'Ada Lovelace',
    initial: 'A',
    drinkSlug: 'cherry-cola',
    drinkName: 'Cherry Cola',
    rating: 13,
    visibility: 'friends',
    saved: saved,
    createdAt: '2026-10-25T10:00:00Z',
    canToast: true,
    canComment: true,
    commentsCount: newest.count,
  );

  @override
  Future<CommentPage> commentPage(
    String checkinId, {
    String? before,
    int limit = 50,
  }) async {
    if (before == null || before.isEmpty) return newest;
    earlierCursors.add(before);
    return older;
  }

  @override
  Future<void> saveCheckin(String id) async {
    if (failSave) throw ApiException('Could not save this pour');
    savedCalls.add(id);
  }

  @override
  Future<void> unsaveCheckin(String id) async {
    if (failSave) throw ApiException('Could not remove this save');
    unsavedCalls.add(id);
  }
}

class _SavedApi extends ApiClient {
  _SavedApi(this.pages);

  final Map<String, List<FeedItem>> pages;
  final List<String?> cursors = [];

  @override
  Future<List<FeedItem>> savedCheckins({int limit = 20, String? before}) async {
    cursors.add(before);
    return pages[before ?? 'first'] ?? const [];
  }
}

Widget _app(ApiClient api, Widget home) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
);

FeedItem _savedRow(int index) => FeedItem(
  id: 's$index',
  handle: 'ada',
  displayName: 'Ada Lovelace',
  drinkName: 'Soda $index',
  drinkSlug: 'soda-$index',
  createdAt: '2026-10-20T10:00:00Z',
);

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  group('feed bookmarks', () {
    testWidgets('saves an unsaved row and flips the icon', (tester) async {
      tester.view.physicalSize = const Size(360, 780);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      final api = _FeedApi();
      await tester.pumpWidget(_app(api, const FeedScreen()));
      await tester.pumpAndSettle();

      expect(find.text('Cherry Cola'), findsOneWidget);
      expect(find.text('Lime Soda'), findsOneWidget);
      expect(find.byIcon(Icons.bookmark_rounded), findsOneWidget);
      expect(find.byIcon(Icons.bookmark_border_rounded), findsOneWidget);

      await tester.tap(find.byIcon(Icons.bookmark_border_rounded));
      await tester.pumpAndSettle();

      expect(api.saved, ['c2']);
      expect(api.unsaved, isEmpty);
      expect(find.byIcon(Icons.bookmark_rounded), findsNWidgets(2));
      expect(find.byIcon(Icons.bookmark_border_rounded), findsNothing);
      expect(tester.takeException(), isNull);
    });

    testWidgets('unsaves a saved row', (tester) async {
      final api = _FeedApi();
      await tester.pumpWidget(_app(api, const FeedScreen()));
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.bookmark_rounded));
      await tester.pumpAndSettle();

      expect(api.unsaved, ['c1']);
      expect(find.byIcon(Icons.bookmark_border_rounded), findsNWidgets(2));
    });

    testWidgets('wires the toggle state into semantics', (tester) async {
      final api = _FeedApi();
      await tester.pumpWidget(_app(api, const FeedScreen()));
      await tester.pumpAndSettle();

      Finder toggled(bool value) => find.byWidgetPredicate(
        (widget) => widget is Semantics && widget.properties.toggled == value,
      );

      expect(toggled(true), findsOneWidget);
      expect(toggled(false), findsOneWidget);
      expect(find.byTooltip('Saved'), findsOneWidget);
      expect(find.byTooltip('Save'), findsOneWidget);
    });

    testWidgets('reverts and explains when the save fails', (tester) async {
      final api = _FeedApi(failSave: true);
      await tester.pumpWidget(_app(api, const FeedScreen()));
      await tester.pumpAndSettle();

      await tester.tap(find.byIcon(Icons.bookmark_border_rounded));
      await tester.pumpAndSettle();

      expect(api.saved, isEmpty);
      expect(find.byIcon(Icons.bookmark_border_rounded), findsOneWidget);
      expect(find.byIcon(Icons.bookmark_rounded), findsOneWidget);
      expect(find.text('Could not save this pour'), findsOneWidget);
    });
  });

  group('check-in detail save', () {
    testWidgets('renders the saved state and unsaves it', (tester) async {
      final api = _DetailApi(saved: true);
      await tester.pumpWidget(
        _app(api, const CheckinDetailScreen(checkinId: 'c1')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Saved'), findsOneWidget);
      expect(find.text('Save'), findsNothing);

      await tester.tap(find.text('Saved'));
      await tester.pumpAndSettle();

      expect(api.unsavedCalls, ['c1']);
      expect(find.text('Save'), findsOneWidget);
      expect(find.text('Saved'), findsNothing);
      expect(tester.takeException(), isNull);
    });

    testWidgets('saves an unsaved check-in and reverts on failure', (
      tester,
    ) async {
      final api = _DetailApi(failSave: true);
      await tester.pumpWidget(
        _app(api, const CheckinDetailScreen(checkinId: 'c1')),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(api.savedCalls, isEmpty);
      expect(find.text('Save'), findsOneWidget);
      expect(find.text('Could not save this pour'), findsOneWidget);
    });
  });

  group('saved posts', () {
    testWidgets('loads the first page and appends older saves', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(420, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      final api = _SavedApi({
        'first': [for (var i = 1; i <= 20; i++) _savedRow(i)],
        's20': const [
          FeedItem(
            id: 's21',
            handle: 'grace',
            displayName: 'Grace Hopper',
            drinkName: 'Older pour',
            drinkSlug: 'older-pour',
            createdAt: '2026-10-01T10:00:00Z',
          ),
        ],
      });

      await tester.pumpWidget(_app(api, const SavedPostsScreen()));
      await tester.pumpAndSettle();

      expect(find.text('Soda 1'), findsOneWidget);
      expect(find.text('Check-ins you saved, newest first.'), findsOneWidget);

      await tester.scrollUntilVisible(find.text('Load older saves'), 400);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Load older saves'));
      await tester.pumpAndSettle();

      expect(api.cursors, [null, 's20']);
      expect(find.text('Load older saves'), findsNothing);

      await tester.scrollUntilVisible(find.text('Older pour'), 400);
      await tester.pumpAndSettle();
      expect(find.text('Older pour'), findsOneWidget);
      expect(find.text('by Grace Hopper'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('shows the empty hint', (tester) async {
      final api = _SavedApi({'first': const []});
      await tester.pumpWidget(_app(api, const SavedPostsScreen()));
      await tester.pumpAndSettle();

      expect(find.text('No saved posts yet'), findsOneWidget);
      expect(
        find.text('Save a pour from your feed and it waits here for you.'),
        findsOneWidget,
      );
    });
  });

  group('comments', () {
    testWidgets('prepends earlier comments without duplicating', (
      tester,
    ) async {
      tester.view.physicalSize = const Size(420, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      final api = _DetailApi(
        newest: CommentPage(
          items: [_comment('m2', 'Middle note'), _comment('m3', 'Newest note')],
          count: 2,
          hasMore: true,
          nextBefore: 'm1',
        ),
        older: CommentPage(
          items: [
            _comment('m1', 'Oldest note'),
            _comment('m2', 'Middle note'),
          ],
          count: 2,
        ),
      );

      await tester.pumpWidget(
        _app(api, const CheckinDetailScreen(checkinId: 'c1')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Show earlier comments'), findsOneWidget);
      await tester.tap(find.text('Show earlier comments'));
      await tester.pumpAndSettle();

      expect(api.earlierCursors, ['m1']);
      expect(find.text('Show earlier comments'), findsNothing);
      expect(find.text('Middle note'), findsOneWidget);

      final oldest = tester.getTopLeft(find.text('Oldest note')).dy;
      final middle = tester.getTopLeft(find.text('Middle note')).dy;
      final newest = tester.getTopLeft(find.text('Newest note')).dy;
      expect(oldest, lessThan(middle));
      expect(middle, lessThan(newest));
      expect(tester.takeException(), isNull);
    });

    testWidgets('reply prefills the composer with a mention', (tester) async {
      tester.view.physicalSize = const Size(420, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      final api = _DetailApi(
        newest: CommentPage(
          items: [_comment('m1', 'Great fizz.'), _comment('m2', 'Thank you', handle: 'ada', canDelete: true)],
          count: 2,
        ),
      );

      await tester.pumpWidget(
        _app(api, const CheckinDetailScreen(checkinId: 'c1')),
      );
      await tester.pumpAndSettle();

      expect(find.text('Reply'), findsOneWidget);

      await tester.tap(find.text('Reply'));
      await tester.pumpAndSettle();

      final field = tester.widget<TextField>(find.byType(TextField));
      expect(field.controller?.text, '@grace ');
      expect(field.controller?.selection.baseOffset, '@grace '.length);
      expect(field.focusNode?.hasFocus, isTrue);
    });

    testWidgets('mention links hand the handle to the profile', (
      tester,
    ) async {
      String? tapped;
      await tester.pumpWidget(
        MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: Scaffold(
            body: Align(
              alignment: Alignment.topLeft,
              child: SizedBox(
                width: 320,
                child: MentionText(
                  body: '@Grace and @ada_nine both fizz',
                  onMention: (handle) => tapped = handle,
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tapAt(
        tester.getTopLeft(find.byType(MentionText)) + const Offset(10, 8),
      );
      await tester.pumpAndSettle();

      expect(tapped, 'grace');
    });
  });
}
