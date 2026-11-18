import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/checkin_detail_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

Comment _comment(
  String id,
  String body, {
  String handle = 'ada',
  bool canDelete = true,
  DateTime? editedAt,
}) => Comment(
  id: id,
  userId: 'u-$handle',
  handle: handle,
  displayName: handle == 'ada' ? 'Ada Lovelace' : 'Grace Hopper',
  body: body,
  createdAt: '2026-10-26T10:00:00Z',
  editedAt: editedAt,
  canDelete: canDelete,
);

class _EditCall {
  const _EditCall(this.id, this.body);

  final String id;
  final String body;
}

class _DetailApi extends ApiClient {
  _DetailApi({this.comments = const [], this.failEdit});

  final List<Comment> comments;

  /// Thrown from [updateComment] when set, to exercise the failure paths.
  final ApiException? failEdit;

  final List<_EditCall> edits = [];

  @override
  Future<CheckinDetail> getCheckin(String id) async => const CheckinDetail(
    id: 'c1',
    handle: 'ada',
    displayName: 'Ada Lovelace',
    initial: 'A',
    drinkSlug: 'cherry-cola',
    drinkName: 'Cherry Cola',
    rating: 13,
    visibility: 'friends',
    createdAt: '2026-10-25T10:00:00Z',
    canToast: true,
    canComment: true,
  );

  @override
  Future<CommentPage> commentPage(
    String checkinId, {
    String? before,
    int limit = 50,
  }) async => CommentPage(items: comments, count: comments.length);

  @override
  Future<Comment> updateComment(String commentId, String body) async {
    edits.add(_EditCall(commentId, body));
    final failure = failEdit;
    if (failure != null) throw failure;
    final current = comments.firstWhere((row) => row.id == commentId);
    return Comment(
      id: current.id,
      userId: current.userId,
      handle: current.handle,
      displayName: current.displayName,
      body: body,
      createdAt: current.createdAt,
      editedAt: DateTime.parse('2026-10-26T11:00:00Z'),
      canDelete: true,
    );
  }
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const CheckinDetailScreen(checkinId: 'c1'),
  ),
);

Finder _dialogField() => find.descendant(
  of: find.byType(AlertDialog),
  matching: find.byType(TextField),
);

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('edits an own comment in place and marks it edited', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _DetailApi(comments: [_comment('m1', 'First fizz thought')]);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    // Own comments offer Edit next to Delete; the marker is absent before.
    expect(find.widgetWithText(TextButton, 'Edit'), findsOneWidget);
    expect(find.widgetWithText(TextButton, 'Delete'), findsOneWidget);
    expect(find.textContaining('(edited)'), findsNothing);

    await tester.tap(find.widgetWithText(TextButton, 'Edit'));
    await tester.pumpAndSettle();

    expect(find.text('Edit comment'), findsOneWidget);
    // The editor arrives prefilled with the current body.
    expect(
      tester.widget<TextField>(_dialogField()).controller?.text,
      'First fizz thought',
    );

    await tester.enterText(_dialogField(), 'Second fizz thought');
    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();

    expect(api.edits.map((call) => call.id), ['m1']);
    expect(api.edits.single.body, 'Second fizz thought');
    expect(find.text('Second fizz thought'), findsOneWidget);
    expect(find.text('First fizz thought'), findsNothing);
    expect(find.textContaining('(edited)'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('hides Edit on other people\'s comments', (tester) async {
    tester.view.physicalSize = const Size(420, 1400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _DetailApi(
      comments: [
        _comment('m1', 'Nice pour', handle: 'grace', canDelete: false),
      ],
    );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    expect(find.widgetWithText(TextButton, 'Edit'), findsNothing);
    expect(find.widgetWithText(TextButton, 'Delete'), findsNothing);
    expect(find.widgetWithText(TextButton, 'Report'), findsOneWidget);
  });

  testWidgets('refuses a blank body without calling the API', (tester) async {
    tester.view.physicalSize = const Size(420, 1400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _DetailApi(comments: [_comment('m1', 'First fizz thought')]);
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(TextButton, 'Edit'));
    await tester.pumpAndSettle();

    await tester.enterText(_dialogField(), '   ');
    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();

    expect(find.text('A comment needs some words.'), findsOneWidget);
    expect(find.text('Edit comment'), findsOneWidget);
    expect(api.edits, isEmpty);
  });

  testWidgets('drops the tile when the comment was deleted elsewhere', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _DetailApi(
      comments: [_comment('m1', 'First fizz thought')],
      failEdit: ApiException('Not found', statusCode: 404, code: 'not_found'),
    );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(TextButton, 'Edit'));
    await tester.pumpAndSettle();

    await tester.enterText(_dialogField(), 'Second fizz thought');
    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();

    expect(api.edits.length, 1);
    expect(find.text('First fizz thought'), findsNothing);
    expect(find.text('That comment is no longer here.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('surfaces other failures without losing the tile', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1400);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _DetailApi(
      comments: [_comment('m1', 'First fizz thought')],
      failEdit: ApiException(
        'The server did not answer (500)',
        statusCode: 500,
      ),
    );
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.widgetWithText(TextButton, 'Edit'));
    await tester.pumpAndSettle();

    await tester.enterText(_dialogField(), 'Second fizz thought');
    await tester.tap(find.widgetWithText(FilledButton, 'Save'));
    await tester.pumpAndSettle();

    expect(find.text('First fizz thought'), findsOneWidget);
    expect(find.text('The server did not answer (500)'), findsOneWidget);
    expect(find.textContaining('(edited)'), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
