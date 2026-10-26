import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/checkin_detail_screen.dart';
import 'package:sodapop/src/screens/notifications_screen.dart';
import 'package:sodapop/src/screens/user_profile_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _FakeCheckinApi extends ApiClient {
  String? posted;

  @override
  Future<CheckinDetail> getCheckin(String id) async => const CheckinDetail(
    id: 'c1',
    handle: 'ada',
    displayName: 'Ada Lovelace',
    initial: 'A',
    drinkSlug: 'cherry-cola',
    drinkName: 'Cherry Cola',
    brand: 'Soda Co',
    style: 'Cola',
    rating: 13,
    note: 'Sharp and cold.',
    visibility: 'friends',
    toasts: 2,
    createdAt: '2026-10-20T10:00:00Z',
    venueName: 'Corner Shop',
    canToast: true,
    canComment: true,
    commentsCount: 1,
  );

  @override
  Future<List<Comment>> listComments(String checkinId) async => const [
    Comment(
      id: 'm1',
      handle: 'grace',
      displayName: 'Grace Hopper',
      initial: 'G',
      body: 'Great fizz.',
      createdAt: '2026-10-21T10:00:00Z',
    ),
  ];

  @override
  Future<Comment> createComment(String checkinId, String body) async {
    posted = body;
    return Comment(
      id: 'm2',
      handle: 'me',
      displayName: 'Me',
      initial: 'M',
      body: body,
      createdAt: '2026-10-22T10:00:00Z',
    );
  }
}

class _UnavailableApi extends ApiClient {
  @override
  Future<UserProfile> userProfile(String handle) async {
    throw ApiException('not found', statusCode: 404);
  }
}

class _NotificationsApi extends ApiClient {
  String? accepted;
  String? declined;
  bool markedAll = false;

  @override
  Future<List<NotificationItem>> notifications({int limit = 30}) async =>
      const [
        NotificationItem(
          id: 'n1',
          kind: 'friend_request',
          actorHandle: 'grace',
          actorDisplayName: 'Grace Hopper',
          actorInitial: 'G',
          body: 'asked to be your friend.',
          href: '/friends',
          read: false,
          createdAt: '2026-10-24T10:00:00Z',
          friendRequestId: '7',
        ),
        NotificationItem(
          id: 'n2',
          kind: 'toast',
          actorHandle: 'ada',
          actorDisplayName: 'Ada Lovelace',
          actorInitial: 'A',
          body: 'toasted your check-in.',
          href: '/checkins/c1',
          read: false,
          createdAt: '2026-10-25T10:00:00Z',
        ),
      ];

  @override
  Future<void> acceptFriendRequest(String id) async => accepted = id;

  @override
  Future<void> removeFriend(String id) async => declined = id;

  @override
  Future<void> markNotificationsRead() async => markedAll = true;
}

class _ProfileApi extends ApiClient {
  @override
  Future<UserProfile> userProfile(String handle) async => const UserProfile(
    id: 'u1',
    handle: 'ada',
    displayName: 'Ada Lovelace',
    bio: 'Cola, mostly. Always cold.',
    createdAt: '2025-03-01T09:00:00Z',
    stats: ProfileStats(checkins: 42, drinks: 18, toasts: 7),
    followerCount: 12,
    followingCount: 3,
    isFollowing: false,
    canBlock: true,
    badges: [
      ProfileBadge(
        slug: 'first-pour',
        name: 'First Pour',
        description: 'Logged your first soda.',
        awardedAt: '2025-03-02T09:00:00Z',
      ),
    ],
    topStyles: [ProfileTopStyle(slug: 'cola', name: 'Cola', count: 9)],
    lists: [ListSummary(id: 'l1', name: 'Weekend fridge', itemCount: 3)],
  );
}

void main() {
  testWidgets('check-in detail shows the check-in and posts a comment', (
    tester,
  ) async {
    final api = _FakeCheckinApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const CheckinDetailScreen(checkinId: 'c1'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Ada Lovelace'), findsOneWidget);
    expect(find.text('Sharp and cold.'), findsOneWidget);
    expect(find.text('3.25'), findsOneWidget);
    expect(find.text('Great fizz.'), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'Nice and sharp.');
    await tester.ensureVisible(find.text('Post'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Post'));
    await tester.pumpAndSettle();

    expect(api.posted, 'Nice and sharp.');
    expect(find.text('Nice and sharp.'), findsOneWidget);
    expect(find.text('2 comments'), findsOneWidget);
  });

  testWidgets('a missing profile shows the neutral unavailable state', (
    tester,
  ) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(_UnavailableApi())],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const UserProfileScreen(handle: 'ghost'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Profile unavailable'), findsOneWidget);
    expect(
      find.text('This profile is private or no longer exists.'),
      findsOneWidget,
    );
    expect(find.text('Back'), findsOneWidget);
  });

  testWidgets('notifications accept a friend request and mark all read', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _NotificationsApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const NotificationsScreen(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.textContaining('Grace Hopper', findRichText: true),
      findsOneWidget,
    );
    expect(find.text('Accept'), findsOneWidget);
    expect(find.text('Decline'), findsOneWidget);

    await tester.tap(find.text('Accept'));
    await tester.pumpAndSettle();

    expect(api.accepted, '7');
    expect(find.text('Accepted'), findsOneWidget);

    await tester.tap(find.byTooltip('Mark all as read'));
    await tester.pumpAndSettle();

    expect(api.markedAll, isTrue);
    expect(find.text('All caught up'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a profile renders on a narrow screen', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(_ProfileApi())],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: const UserProfileScreen(handle: 'ada'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Ada Lovelace'), findsOneWidget);
    expect(find.text('Follow'), findsOneWidget);
    expect(find.text('Check-ins'), findsOneWidget);
    expect(find.text('Badges'), findsOneWidget);
    expect(find.text('Weekend fridge'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
