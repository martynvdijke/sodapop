import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/feed_screen.dart';
import 'package:sodapop/src/screens/home_shell.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

/// The viewer's own pour and a friend's pour, in the order the API sends them.
const _ownItem = FeedItem(
  id: 'c-own',
  handle: 'ada',
  displayName: 'Ada Lovelace',
  drinkName: 'Cherry Cola',
  drinkSlug: 'cherry-cola',
  rating: 16,
  note: 'Cold and sharp.',
);

const _friendItem = FeedItem(
  id: 'c-friend',
  handle: 'grace',
  displayName: 'Grace Hopper',
  drinkName: 'Lime Soda',
  drinkSlug: 'lime-soda',
  rating: 12,
);

class _FeedApi extends ApiClient {
  _FeedApi({this.items = const []});

  final List<FeedItem> items;
  int feedLoads = 0;

  @override
  Future<List<FeedItem>> feed({int limit = 30}) async {
    feedLoads++;
    return items;
  }

  @override
  Future<MyStats> myStats() async =>
      const MyStats(checkins: 0, uniqueDrinks: 0, badges: 0);

  @override
  Future<List<CheckinSummary>> myCheckins({
    int limit = 20,
    String? before,
  }) async => const [];

  @override
  Future<FriendsDirectory> friends() async => const FriendsDirectory();
}

class _SignedInAuth extends AuthController {
  @override
  AuthState build() => const AuthState(
    loading: false,
    serverUrl: 'https://soda.example',
    token: 'sd_test_token_1234567890',
    me: Me(
      id: 'u1',
      handle: 'ada',
      displayName: 'Ada Lovelace',
      email: 'ada@example.com',
    ),
  );
}

Widget _app(ApiClient api, Widget home) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(_SignedInAuth.new),
  ],
  child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
);

void _sizeTo(WidgetTester tester, Size size) {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  testWidgets('bottom navigation names the first tab Feed', (tester) async {
    _sizeTo(tester, const Size(420, 900));

    await tester.pumpWidget(
      _app(_FeedApi(items: const [_ownItem, _friendItem]), const HomeShell()),
    );
    await tester.pumpAndSettle();

    final nav = find.byType(NavigationBar);
    expect(nav, findsOneWidget);

    for (final label in ['Feed', 'Scan', 'Discover', 'You']) {
      expect(
        find.descendant(of: nav, matching: find.text(label)),
        findsOneWidget,
        reason: '$label should be a bottom navigation destination',
      );
    }

    // The feed is no longer labelled as a friends list.
    expect(
      find.descendant(of: nav, matching: find.text('Friends')),
      findsNothing,
    );
    expect(find.byIcon(Icons.dynamic_feed), findsOneWidget);
    expect(find.byIcon(Icons.people_alt_outlined), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets("feed shows your own check-in next to a friend's", (
    tester,
  ) async {
    _sizeTo(tester, const Size(420, 1200));

    await tester.pumpWidget(
      _app(_FeedApi(items: const [_ownItem, _friendItem]), const FeedScreen()),
    );
    await tester.pumpAndSettle();

    // Both rows come straight from the API; nothing filters the viewer out.
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('@ada'), findsOneWidget);
    expect(find.text('Lime Soda'), findsOneWidget);
    expect(find.text('@grace'), findsOneWidget);
    expect(find.textContaining('Your feed is quiet'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('empty feed explains that this is your feed', (tester) async {
    _sizeTo(tester, const Size(420, 900));

    await tester.pumpWidget(_app(_FeedApi(), const FeedScreen()));
    await tester.pumpAndSettle();

    expect(find.text('Your feed is quiet.'), findsOneWidget);
    expect(
      find.text('Scan a can or add a friend to see pours here.'),
      findsOneWidget,
    );
    expect(find.textContaining('No fizz yet'), findsNothing);

    // Adding a friend is still one tap away from the empty feed.
    expect(find.byTooltip('Add friend'), findsOneWidget);
    expect(find.text('Find friends'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('empty feed opens the add-friend screen', (tester) async {
    _sizeTo(tester, const Size(420, 900));

    await tester.pumpWidget(_app(_FeedApi(), const FeedScreen()));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Find friends'));
    await tester.pumpAndSettle();

    expect(find.text('Add a friend'), findsOneWidget);
    expect(find.byType(TextField), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
