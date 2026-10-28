import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/friends_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _FriendsApi extends ApiClient {
  bool pending = true;
  int loads = 0;
  String? sent;
  String? accepted;
  String? removed;

  @override
  Future<FriendsDirectory> friends() async {
    loads += 1;
    return FriendsDirectory(
      friends: [
        const FriendUser(
          id: 'f1',
          handle: 'ada',
          displayName: 'Ada Lovelace',
          since: '2025-03-02T09:00:00Z',
        ),
        if (!pending)
          const FriendUser(
            id: 'f2',
            handle: 'grace',
            displayName: 'Grace Hopper',
            since: '2026-10-26T10:00:00Z',
          ),
      ],
      incoming: pending
          ? [
              const PendingFriend(
                id: '7',
                handle: 'grace',
                displayName: 'Grace Hopper',
                createdAt: '2026-10-24T10:00:00Z',
              ),
            ]
          : const [],
      outgoing: const [
        PendingFriend(
          id: '9',
          handle: 'linus',
          displayName: 'Linus T.',
          createdAt: '2026-10-25T10:00:00Z',
        ),
      ],
    );
  }

  @override
  Future<void> sendFriendRequest(String handle) async => sent = handle;

  @override
  Future<void> acceptFriendRequest(String id) async {
    accepted = id;
    pending = false;
  }

  @override
  Future<void> removeFriend(String id) async => removed = id;
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const FriendsScreen(),
  ),
);

void main() {
  testWidgets('friends directory lists friends and pending requests', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FriendsApi()));
    await tester.pumpAndSettle();

    expect(find.text('Ada Lovelace'), findsOneWidget);
    expect(find.textContaining('@ada'), findsOneWidget);
    expect(find.textContaining('Friends since'), findsOneWidget);
    expect(find.text('1 friend'), findsOneWidget);
    expect(find.text('Requests (2)'), findsOneWidget);

    await tester.tap(find.text('Requests (2)'));
    await tester.pumpAndSettle();

    expect(find.text('Incoming'), findsOneWidget);
    expect(find.text('Sent'), findsOneWidget);
    expect(find.text('Grace Hopper'), findsOneWidget);
    expect(find.text('Linus T.'), findsOneWidget);
    expect(find.text('Accept'), findsOneWidget);
    expect(find.text('Cancel'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('sending a request calls the API and shows neutral copy', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FriendsApi();
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Add'));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField), '@Grace_Hopper');
    await tester.tap(find.text('Send request'));
    await tester.pumpAndSettle();

    expect(api.sent, 'grace_hopper');
    expect(api.loads, greaterThan(1));
    expect(
      find.text('If that account exists, a request has been sent.'),
      findsOneWidget,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('accepting a request refreshes the directory', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FriendsApi();
    await tester.pumpWidget(_app(api));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Requests (2)'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Accept'));
    await tester.pumpAndSettle();

    expect(api.accepted, '7');
    expect(api.loads, greaterThan(1));
    expect(find.text('Friend added.'), findsOneWidget);
    expect(find.text('Requests (1)'), findsOneWidget);

    await tester.tap(find.text('Friends').first);
    await tester.pumpAndSettle();

    expect(find.text('Grace Hopper'), findsOneWidget);
    expect(find.text('2 friends'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
