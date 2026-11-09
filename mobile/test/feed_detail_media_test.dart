import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/checkin_detail_screen.dart';
import 'package:sodapop/src/screens/feed_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _FeedApi extends ApiClient {
  _FeedApi() {
    configure('https://soda.test', token: 'sd_test');
  }

  @override
  Future<List<FeedItem>> feed({int limit = 30}) async => const [
    FeedItem(
      id: 'c-photo',
      handle: 'ada',
      displayName: 'Ada Lovelace',
      drinkName: 'Cherry Cola',
      drinkSlug: 'cherry-cola',
      note: 'Cold and sharp.',
      venueName: 'Corner Shop',
      photoId: 'm9',
      photoMime: 'image/jpeg',
      taggedFriends: [TaggedFriend(id: 'u-grace', handle: 'grace')],
    ),
    FeedItem(
      id: 'c-video',
      handle: 'grace',
      displayName: 'Grace Hopper',
      drinkName: 'Lime Soda',
      drinkSlug: 'lime-soda',
      photoId: 'm10',
      photoMime: 'video/mp4',
      taggedFriends: [
        TaggedFriend(id: 'u-ada', handle: 'ada', displayName: 'Ada Lovelace'),
      ],
    ),
  ];
}

class _DetailApi extends ApiClient {
  @override
  Future<CheckinDetail> getCheckin(String id) async => const CheckinDetail(
    id: 'c-detail',
    handle: 'ada',
    displayName: 'Ada Lovelace',
    initial: 'A',
    drinkSlug: 'cherry-cola',
    drinkName: 'Cherry Cola',
    atHome: true,
    taggedFriends: [TaggedFriend(id: 'u-grace', handle: 'grace')],
  );

  @override
  Future<CommentPage> commentPage(
    String checkinId, {
    String? before,
    int limit = 50,
  }) async => const CommentPage();
}

Widget _app(ApiClient api, Widget home) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
);

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('feed renders photo, video affordance, tags and venue', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1200);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FeedApi(), const FeedScreen()));
    await tester.pumpAndSettle();

    // Image rows use the media URL; the test HTTP stub trips errorBuilder but
    // the Image widget itself is still the rendered photo surface.
    expect(find.byKey(const Key('feed-photo-c-photo')), findsOneWidget);
    expect(find.byKey(const Key('feed-video-c-video')), findsOneWidget);
    expect(find.text('with @grace'), findsOneWidget);
    expect(find.text('with Ada Lovelace'), findsOneWidget);
    expect(find.text('at Corner Shop'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('detail shows tagged friends and the location without a venue', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(420, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      _app(_DetailApi(), const CheckinDetailScreen(checkinId: 'c-detail')),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('detail-tags')), findsOneWidget);
    expect(find.text('With @grace'), findsOneWidget);
    expect(find.text('@home'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
