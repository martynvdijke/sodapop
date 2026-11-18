import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/follow_list_screen.dart';
import 'package:sodapop/src/screens/user_profile_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

/// Serves canned follow pages keyed by the `before` cursor, and records what
/// the screen asked for.
class _FollowApi extends ApiClient {
  _FollowApi({
    this.followerPages = const {},
    this.followingPages = const {},
    this.profile,
  });

  /// `null` is the first page; any other key is the cursor sent as `before`.
  final Map<String?, FollowPage> followerPages;
  final Map<String?, FollowPage> followingPages;
  final UserProfile? profile;

  final List<String?> followerCursors = [];
  final List<String?> followingCursors = [];

  ApiException? followersFailure;
  ApiException? followingFailure;

  @override
  Future<FollowPage> listFollowers(
    String handle, {
    int? limit,
    String? before,
  }) async {
    followerCursors.add(before);
    final failure = followersFailure;
    if (failure != null) throw failure;
    return followerPages[before] ?? const FollowPage();
  }

  @override
  Future<FollowPage> listFollowing(
    String handle, {
    int? limit,
    String? before,
  }) async {
    followingCursors.add(before);
    final failure = followingFailure;
    if (failure != null) throw failure;
    return followingPages[before] ?? const FollowPage();
  }

  @override
  Future<UserProfile> userProfile(String handle) async =>
      profile ?? const UserProfile(id: 'u1', handle: 'ada');
}

const _grace = FollowUser(
  handle: 'grace',
  displayName: 'Grace Hopper',
  bio: 'Navy fizz.',
  isFriend: true,
);

const _linus = FollowUser(
  handle: 'linus',
  displayName: 'Linus S.',
  followedByViewer: true,
);

Widget _app(ApiClient api, Widget home) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
);

void main() {
  testWidgets('followers list shows people and their follow state', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FollowApi(
      followerPages: {
        null: const FollowPage(items: [_grace, _linus]),
      },
    );
    await tester.pumpWidget(
      _app(
        api,
        const FollowListScreen(handle: 'ada', mode: FollowListMode.followers),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Followers'), findsOneWidget);
    expect(find.text('Grace Hopper'), findsOneWidget);
    expect(find.text('@grace'), findsOneWidget);
    expect(find.text('Friend'), findsOneWidget);
    expect(find.text('Linus S.'), findsOneWidget);
    expect(find.text('Following'), findsOneWidget);
    expect(api.followerCursors, [null]);
    expect(api.followingCursors, isEmpty);
    expect(tester.takeException(), isNull);
  });

  testWidgets('following list asks the following endpoint and opens profiles', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FollowApi(
      followingPages: {
        null: const FollowPage(items: [_grace]),
      },
      profile: const UserProfile(
        id: 'u2',
        handle: 'grace',
        displayName: 'Grace Hopper',
      ),
    );
    await tester.pumpWidget(
      _app(
        api,
        const FollowListScreen(handle: 'ada', mode: FollowListMode.following),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Following'), findsOneWidget);
    expect(api.followingCursors, [null]);
    expect(api.followerCursors, isEmpty);

    await tester.tap(find.text('Grace Hopper'));
    await tester.pumpAndSettle();

    expect(find.text('@grace'), findsWidgets);
    expect(find.text('Profile unavailable'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('the end of the list loads the next cursor page', (tester) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FollowApi(
      followerPages: {
        null: const FollowPage(items: [_grace], nextCursor: 'c1'),
        'c1': const FollowPage(items: [_linus]),
      },
    );
    await tester.pumpWidget(
      _app(
        api,
        const FollowListScreen(handle: 'ada', mode: FollowListMode.followers),
      ),
    );
    await tester.pumpAndSettle();

    expect(api.followerCursors, [null, 'c1']);
    expect(find.text('Grace Hopper'), findsOneWidget);
    expect(find.text('Linus S.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a private or missing profile reads as unavailable', (
    tester,
  ) async {
    final api = _FollowApi()
      ..followersFailure = ApiException('not found', statusCode: 404);
    await tester.pumpWidget(
      _app(
        api,
        const FollowListScreen(handle: 'ghost', mode: FollowListMode.followers),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Profile unavailable'), findsOneWidget);
    expect(
      find.text('This profile is private or no longer exists.'),
      findsOneWidget,
    );
    expect(find.text('Back'), findsOneWidget);
    expect(api.followerCursors, [null]);
  });

  testWidgets('an empty followers list says nobody follows yet', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        _FollowApi(),
        const FollowListScreen(handle: 'ada', mode: FollowListMode.followers),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('No followers yet'), findsOneWidget);
    expect(find.text('Nobody follows @ada yet.'), findsOneWidget);
  });

  testWidgets('an empty following list names the member', (tester) async {
    await tester.pumpWidget(
      _app(
        _FollowApi(),
        const FollowListScreen(handle: 'ada', mode: FollowListMode.following),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Not following anyone yet'), findsOneWidget);
    expect(find.text('@ada does not follow anyone yet.'), findsOneWidget);
  });

  testWidgets('profile counts open the followers and following lists', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(400, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    final api = _FollowApi(
      followerPages: {
        null: const FollowPage(items: [_grace]),
      },
      followingPages: {
        null: const FollowPage(items: [_linus]),
      },
      profile: const UserProfile(
        id: 'u1',
        handle: 'ada',
        displayName: 'Ada Lovelace',
        followerCount: 12,
        followingCount: 3,
      ),
    );
    await tester.pumpWidget(_app(api, const UserProfileScreen(handle: 'ada')));
    await tester.pumpAndSettle();

    expect(find.text('12 followers'), findsOneWidget);
    expect(find.text('3 following'), findsOneWidget);

    await tester.tap(find.byKey(const Key('profile-followers')));
    await tester.pumpAndSettle();

    expect(api.followerCursors, [null]);
    expect(find.text('Grace Hopper'), findsOneWidget);

    await tester.pageBack();
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('profile-following')));
    await tester.pumpAndSettle();

    expect(api.followingCursors, [null]);
    expect(find.text('Linus S.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
