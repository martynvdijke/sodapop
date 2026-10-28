import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/offline/read_cache.dart';
import 'package:sodapop/src/screens/feed_screen.dart';

FeedItem _item(String id, {String drink = 'Cola'}) => FeedItem(
  id: id,
  handle: 'ada',
  displayName: 'Ada',
  drinkName: drink,
  drinkSlug: 'cola',
  rating: 16,
  note: 'Crisp',
  venueName: 'Corner Shop',
  toasts: 2,
  toasted: true,
  saved: true,
);

Drink _drink(String slug) => Drink(
  id: slug.hashCode,
  slug: slug,
  name: 'Soda $slug',
  brand: 'Soda Co',
  style: 'Cola',
  volumeMl: 330,
  container: 'can',
  rating: 4.5,
  description: 'A classic',
  caffeineMgPer100ml: 10,
  sugarGPer100ml: 10.6,
  caloriesKcalPer100ml: 42,
  sweetener: 'sugar',
  carbonation: 'standard',
  acid: 'phosphoric',
  status: 'active',
  raterCount: 4,
  ratingShown: true,
  flavours: const [
    DrinkFlavour(name: 'Cola', slug: 'cola', intensity: 4),
  ],
  aliases: const ['Coke'],
  barcodeCount: 2,
);

const _images = [
  DrinkImage(
    id: 9,
    mediaId: 'm9',
    url: '/media/m9',
    thumbUrl: '/media/m9/thumb',
    isPrimary: true,
    attribution: 'Ada',
    createdAt: '2026-10-26T10:00:00Z',
    canModerate: true,
  ),
];

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
  });

  group('feed cache', () {
    test('round-trips both scopes independently', () async {
      final cache = ReadCache();

      await cache.saveFeed('friends', [_item('c1')]);
      await cache.saveFeed('following', [
        _item('c2', drink: 'Lime'),
        _item('c3', drink: 'Cherry'),
      ]);

      final friends = await cache.loadFeed('friends');
      final following = await cache.loadFeed('following');

      expect(friends, hasLength(1));
      expect(friends!.single.id, 'c1');
      expect(friends.single.drinkName, 'Cola');
      expect(friends.single.handle, 'ada');
      expect(friends.single.displayName, 'Ada');
      expect(friends.single.rating, 16);
      expect(friends.single.note, 'Crisp');
      expect(friends.single.venueName, 'Corner Shop');
      expect(friends.single.toasts, 2);
      expect(friends.single.toasted, isTrue);
      expect(friends.single.saved, isTrue);

      expect(following, hasLength(2));
      expect(following!.first.drinkName, 'Lime');
      expect(following.last.drinkName, 'Cherry');
    });

    test('returns null when a scope has no cache', () async {
      expect(await ReadCache().loadFeed('friends'), isNull);
    });

    test('caps stored rows at 40', () async {
      final cache = ReadCache();
      await cache.saveFeed(
        'friends',
        [for (var i = 0; i < 45; i++) _item('c$i')],
      );

      final rows = await cache.loadFeed('friends');
      expect(rows, hasLength(ReadCache.maxFeedItems));
      expect(rows!.first.id, 'c0');
      expect(rows.last.id, 'c39');
    });
  });

  group('drink cache', () {
    test('round-trips the drink and its gallery', () async {
      final cache = ReadCache();

      await cache.saveDrink(_drink('cola'), _images);

      final cached = await cache.loadDrink('cola');
      expect(cached, isNotNull);
      expect(cached!.drink.slug, 'cola');
      expect(cached.drink.name, 'Soda cola');
      expect(cached.drink.brand, 'Soda Co');
      expect(cached.drink.volumeMl, 330);
      expect(cached.drink.sugarGPer100ml, 10.6);
      expect(cached.drink.rating, 4.5);
      expect(cached.drink.ratingShown, isTrue);
      expect(cached.drink.flavours.single.name, 'Cola');
      expect(cached.drink.aliases, ['Coke']);
      expect(cached.images.single.id, 9);
      expect(cached.images.single.mediaId, 'm9');
      expect(cached.images.single.thumbUrl, '/media/m9/thumb');
      expect(cached.images.single.isPrimary, isTrue);
      expect(cached.images.single.canModerate, isTrue);
    });

    test('returns null for an unknown slug', () async {
      expect(await ReadCache().loadDrink('missing'), isNull);
    });

    test('keeps the 30 most recent drinks', () async {
      final cache = ReadCache();

      for (var i = 0; i < 31; i++) {
        await cache.saveDrink(_drink('soda-$i'), const []);
      }

      expect(await cache.loadDrink('soda-0'), isNull);
      expect(await cache.loadDrink('soda-1'), isNotNull);
      final newest = await cache.loadDrink('soda-30');
      expect(newest, isNotNull);
      expect(newest!.drink.slug, 'soda-30');

      final prefs = await SharedPreferences.getInstance();
      final index = prefs.getString('sodapop.cache.drinks.v1');
      expect(index, isNotNull);
      expect(index!.split('"slug"'), hasLength(ReadCache.maxDrinks + 1));
    });

    test('re-saving a drink refreshes it without growing the index', () async {
      final cache = ReadCache();

      for (var i = 0; i < 30; i++) {
        await cache.saveDrink(_drink('soda-$i'), const []);
      }
      await cache.saveDrink(_drink('soda-0'), _images);

      final refreshed = await cache.loadDrink('soda-0');
      expect(refreshed!.images.single.id, 9);
      expect(await cache.loadDrink('soda-1'), isNotNull);
    });
  });

  group('malformed and failing storage', () {
    test('malformed feed JSON returns null', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('sodapop.cache.feed.v1.friends', '{not json');

      expect(await ReadCache().loadFeed('friends'), isNull);
    });

    test('a non-list feed payload returns null', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('sodapop.cache.feed.v1.friends', '{"a":1}');

      expect(await ReadCache().loadFeed('friends'), isNull);
    });

    test('malformed drink JSON returns null', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('sodapop.cache.drink.v1.cola', 'oops');

      expect(await ReadCache().loadDrink('cola'), isNull);
    });

    test('a malformed index does not throw and is replaced', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString('sodapop.cache.drinks.v1', 'not-json');

      await expectLater(
        ReadCache().saveDrink(_drink('cola'), _images),
        completes,
      );
      expect(await ReadCache().loadDrink('cola'), isNotNull);
    });

    test('saving an empty slug is a no-op', () async {
      await expectLater(
        ReadCache().saveDrink(_drink(''), const []),
        completes,
      );
      expect(await ReadCache().loadDrink(''), isNull);
    });

    test('a malformed row inside the index is ignored', () async {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(
        'sodapop.cache.drinks.v1',
        '[{"slug":42},{"slug":"gone","saved_at":"bad"}]',
      );

      await expectLater(
        ReadCache().saveDrink(_drink('cola'), const []),
        completes,
      );
      expect(await ReadCache().loadDrink('cola'), isNotNull);
    });
  });

  group('clear', () {
    test('removes feed and drink entries', () async {
      final cache = ReadCache();
      await cache.saveFeed('friends', [_item('c1')]);
      await cache.saveDrink(_drink('cola'), _images);

      await cache.clear();

      expect(await cache.loadFeed('friends'), isNull);
      expect(await cache.loadDrink('cola'), isNull);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getKeys().where((key) => key.startsWith('sodapop.cache.')),
          isEmpty);
    });
  });

  group('isOfflineError', () {
    test('treats connection problems as offline', () {
      expect(
        isOfflineError(ApiException('Cannot reach the server')),
        isTrue,
      );
      expect(
        isOfflineError(
          ApiException('The server did not answer (network)'),
        ),
        isTrue,
      );
    });

    test('does not treat an HTTP rejection as offline', () {
      expect(
        isOfflineError(ApiException('Not found', statusCode: 404)),
        isFalse,
      );
    });
  });

  group('feed screen offline fallback', () {
    testWidgets('shows the cached feed with a banner', (tester) async {
      await ReadCache().saveFeed('friends', [_item('c1', drink: 'Cherry Cola')]);

      await tester.pumpWidget(
        ProviderScope(
          overrides: [apiProvider.overrideWithValue(_OfflineApi())],
          child: const MaterialApp(home: FeedScreen()),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.text(
          'Showing saved feed. Pull to refresh when you are online.',
        ),
        findsOneWidget,
      );
      expect(find.text('Cherry Cola'), findsOneWidget);
      expect(find.text('Try again'), findsNothing);
    });

    testWidgets('keeps the error state without a cache', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [apiProvider.overrideWithValue(_OfflineApi())],
          child: const MaterialApp(home: FeedScreen()),
        ),
      );
      await tester.pumpAndSettle();

      expect(
        find.text(
          'Showing saved feed. Pull to refresh when you are online.',
        ),
        findsNothing,
      );
      expect(find.text('Try again'), findsOneWidget);
    });
  });
}

class _OfflineApi extends ApiClient {
  @override
  Future<List<FeedItem>> feed({int limit = 30}) async {
    throw ApiException('Cannot reach the server. Check the address and try again.');
  }
}
