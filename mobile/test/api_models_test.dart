import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/models.dart';

void main() {
  group('CheckinDetail', () {
    test('reads the nested detail payload', () {
      final detail = CheckinDetail.fromJson({
        'id': 'abc',
        'user': {
          'handle': 'ada',
          'display_name': 'Ada Lovelace',
          'initial': 'A',
        },
        'drink': {
          'slug': 'cola',
          'name': 'Cola',
          'brand': 'Soda Co',
          'style': 'Cola',
        },
        'rating': 12,
        'note': 'Good',
        'visibility': 'friends',
        'toasts': 3,
        'toasted': true,
        'created_at': '2026-10-26T10:00:00Z',
        'venue': {'id': 7, 'name': 'Corner Shop'},
        'photo': {'id': 'p1', 'mime': 'image/jpeg'},
        'can_delete': true,
        'can_toast': false,
        'can_comment': true,
        'comments_count': 2,
      });

      expect(detail.id, 'abc');
      expect(detail.handle, 'ada');
      expect(detail.displayName, 'Ada Lovelace');
      expect(detail.initial, 'A');
      expect(detail.drinkSlug, 'cola');
      expect(detail.brand, 'Soda Co');
      expect(detail.rating, 12);
      expect(detail.venueId, 7);
      expect(detail.venueName, 'Corner Shop');
      expect(detail.photoId, 'p1');
      expect(detail.canComment, isTrue);
      expect(detail.commentsCount, 2);
    });

    test('tolerates missing and null fields', () {
      final detail = CheckinDetail.fromJson({'id': 'abc', 'user': null});
      expect(detail.handle, '');
      expect(detail.rating, isNull);
      expect(detail.toasts, 0);
      expect(detail.canDelete, isFalse);
    });
  });

  group('Comment', () {
    test('derives the initial when the server omits it', () {
      final comment = Comment.fromJson({
        'id': 'c1',
        'user': {'handle': 'ada', 'display_name': 'Ada'},
        'body': 'Nice',
        'created_at': '2026-10-26T10:00:00Z',
        'can_delete': true,
      });
      expect(comment.initial, 'A');
      expect(comment.canDelete, isTrue);
    });
  });

  group('UserProfile', () {
    test('reads stats, badges, styles and lists', () {
      final profile = UserProfile.fromJson({
        'id': 'u1',
        'handle': 'ada',
        'display_name': 'Ada',
        'bio': 'Hello',
        'created_at': '2026-01-01T00:00:00Z',
        'stats': {'checkins': 4, 'drinks': 3, 'toasts': 9},
        'follower_count': 2,
        'following_count': 5,
        'is_following': true,
        'is_friend': false,
        'can_block': true,
        'badges': [
          {
            'slug': 'first-pour',
            'name': 'First Pour',
            'awarded_at': '2026-01-02',
          },
        ],
        'top_styles': [
          {'slug': 'cola', 'name': 'Cola', 'count': 3},
        ],
        'lists': [
          {'id': 'l1', 'name': 'Favourites', 'item_count': 2},
        ],
      });

      expect(profile.stats.checkins, 4);
      expect(profile.stats.drinks, 3);
      expect(profile.stats.toasts, 9);
      expect(profile.followerCount, 2);
      expect(profile.isFollowing, isTrue);
      expect(profile.canBlock, isTrue);
      expect(profile.badges.single.awardedAt, '2026-01-02');
      expect(profile.topStyles.single.count, 3);
      expect(profile.lists.single.itemCount, 2);
    });

    test('tolerates a sparse payload', () {
      final profile = UserProfile.fromJson({'handle': 'ada'});
      expect(profile.stats.checkins, 0);
      expect(profile.badges, isEmpty);
      expect(profile.topStyles, isEmpty);
      expect(profile.lists, isEmpty);
    });
  });

  group('NotificationItem', () {
    test('uses text for the body and actor for the identity', () {
      final item = NotificationItem.fromJson({
        'id': 'n1',
        'kind': 'friend_request',
        'subject_id': 's1',
        'text': 'wants to be friends',
        'actor': {'handle': 'bob', 'display_name': 'Bob'},
        'href': '/friends',
        'friend_request_id': 'f1',
        'read': false,
        'created_at': '2026-10-26T10:00:00Z',
      });

      expect(item.body, 'wants to be friends');
      expect(item.actorHandle, 'bob');
      expect(item.actorInitial, 'B');
      expect(item.friendRequestId, 'f1');
      expect(item.read, isFalse);
    });

    test('tolerates missing actor and text', () {
      final item = NotificationItem.fromJson({'id': 'n1'});
      expect(item.actorHandle, '');
      expect(item.body, '');
      expect(item.friendRequestId, isNull);
    });
  });

  group('ListDetail', () {
    test('reads items and falls back to drink fields', () {
      final list = ListDetail.fromJson({
        'id': 'l1',
        'name': 'Favourites',
        'visibility': 'private',
        'is_system': true,
        'items': [
          {
            'drink_id': 5,
            'slug': 'cola',
            'name': 'Cola',
            'brand': 'Soda Co',
            'note': 'Try',
            'created_at': '2026-01-01',
          },
        ],
      });

      expect(list.isSystem, isTrue);
      expect(list.items.single.drinkId, 5);
      expect(list.items.single.brand, 'Soda Co');
    });

    test('tolerates a summary-only payload', () {
      final list = ListDetail.fromJson({'id': 'l1'});
      expect(list.items, isEmpty);
      expect(list.itemCount, 0);
    });
  });

  test('SavedVenue reuses the venue parser', () {
    final venue = SavedVenue.fromJson({
      'id': 7,
      'name': 'Corner Shop',
      'kind': 'shop',
      'city': 'Leeds',
    });
    expect(venue.name, 'Corner Shop');
    expect(venue.kind, 'shop');
    expect(venue.city, 'Leeds');
  });

  group('Insights', () {
    test('reads series, top drinks and caffeine fields', () {
      final insights = Insights.fromJson({
        'range_days': 30,
        'from': '2026-09-27T00:00:00Z',
        'to': '2026-10-26T12:00:00Z',
        'pours': 12,
        'sugar_g': 240.5,
        'caffeine_mg': 96.25,
        'calories_kcal': 1200,
        'avg_sugar_per_day_g': 8.02,
        'avg_caffeine_per_day_mg': 3.21,
        'series': [
          {
            'date': '2026-10-26',
            'sugar_g': 20.5,
            'caffeine_mg': 8,
            'calories_kcal': 100,
            'pours': 2,
          },
        ],
        'top_drinks': [
          {
            'slug': 'cola',
            'name': 'Cola',
            'brand': 'Soda Co',
            'pours': 5,
            'sugar_g': 100,
            'caffeine_mg': 30,
          },
        ],
        'bedtime': '23:00',
        'last_caffeine_at': '2026-10-26T10:00:00Z',
        'caffeine_clear_by': '2026-10-26T16:00:00Z',
        'late_caffeine': true,
      });

      expect(insights.rangeDays, 30);
      expect(insights.sugarG, 240.5);
      expect(insights.series.single.pours, 2);
      expect(insights.topDrinks.single.brand, 'Soda Co');
      expect(insights.lateCaffeine, isTrue);
    });

    test('tolerates missing fields', () {
      final insights = Insights.fromJson({});
      expect(insights.rangeDays, 0);
      expect(insights.series, isEmpty);
      expect(insights.topDrinks, isEmpty);
    });
  });

  group('MixRecipe', () {
    test('reads components and serialises a create payload', () {
      final mix = MixRecipe.fromJson({
        'id': 12,
        'slug': 'mix-cola-abc1',
        'name': 'Cola Mix',
        'notes': 'Half and half',
        'visibility': 'friends',
        'created_by': 'u1',
        'created_at': '2026-10-26T10:00:00Z',
        'components': [
          {
            'drink_id': 5,
            'slug': 'cola',
            'name': 'Cola',
            'parts': 2,
            'volume_ml': 200,
          },
          {'drink_id': 6, 'slug': 'lime', 'name': 'Lime', 'parts': 1},
        ],
      });

      expect(mix.id, '12');
      expect(mix.components.length, 2);
      expect(mix.components.first.volumeMl, 200);
      expect(mix.components.last.toJson(), {'drink_id': 6, 'parts': 1});
    });

    test('tolerates missing components', () {
      final mix = MixRecipe.fromJson({'id': 'x'});
      expect(mix.components, isEmpty);
      expect(mix.name, '');
    });
  });

  group('ProfileStats', () {
    test('reads the API stat keys', () {
      final stats = ProfileStats.fromJson({
        'checkins': 12,
        'distinct_drinks': 7,
        'toasts_received': 4,
      });
      expect(stats.checkins, 12);
      expect(stats.drinks, 7);
      expect(stats.toasts, 4);
    });

    test('ignores an array in the badges field', () {
      final stats = ProfileStats.fromJson({
        'badges': ['first-pour'],
      });
      expect(stats.badges, 0);
    });
  });

  group('Passport', () {
    test('reads stats and earned badges', () {
      final passport = Passport.fromJson({
        'stats': {
          'checkins': 40,
          'unique_drinks': 22,
          'toasts_received': 9,
          'badges': 3,
        },
        'badges': [
          {
            'slug': 'first-pour',
            'name': 'First Pour',
            'description': 'Log your first soda',
            'awarded_at': '2026-01-02',
          },
        ],
      });

      expect(passport.stats.checkins, 40);
      expect(passport.stats.drinks, 22);
      expect(passport.stats.toasts, 9);
      expect(passport.stats.badges, 3);
      expect(passport.badges.single.name, 'First Pour');
    });

    test('tolerates a sparse passport', () {
      final passport = Passport.fromJson({});
      expect(passport.stats.checkins, 0);
      expect(passport.badges, isEmpty);
    });
  });

  group('VenueDetail', () {
    test('reads the nested venue and recent drinks', () {
      final detail = VenueDetail.fromJson({
        'venue': {
          'id': 7,
          'slug': 'corner-shop',
          'name': 'Corner Shop',
          'kind': 'shop',
          'address': '1 High Street',
          'city': 'Leeds',
          'region': 'West Yorkshire',
          'country': 'GB',
          'website': 'https://example.com',
          'lat': 53.8,
          'lng': -1.55,
        },
        'drinks': [
          {
            'drink_slug': 'cola',
            'drink_name': 'Cola',
            'brand': 'Soda Co',
            'last_seen_at': '2026-10-26T10:00:00Z',
            'on_offer': true,
          },
        ],
      });

      expect(detail.venue.name, 'Corner Shop');
      expect(detail.venue.address, '1 High Street');
      expect(detail.venue.lat, 53.8);
      expect(detail.drinks.single.name, 'Cola');
      expect(detail.drinks.single.onOffer, isTrue);
    });

    test('tolerates a flat venue with no drinks', () {
      final detail = VenueDetail.fromJson({'id': 8, 'name': 'Kiosk'});
      expect(detail.venue.id, 8);
      expect(detail.venue.kind, 'other');
      expect(detail.drinks, isEmpty);
    });
  });

  group('VenueMedia', () {
    test('reads media capabilities', () {
      final media = VenueMedia.fromJson({
        'id': 'm1',
        'media_id': 'media-1',
        'kind': 'photo',
        'label': 'Front',
        'uploader_handle': 'ada',
        'created_at': '2026-10-26T10:00:00Z',
        'can_remove': true,
      });
      expect(media.mediaId, 'media-1');
      expect(media.uploaderHandle, 'ada');
      expect(media.canRemove, isTrue);
    });
  });
}
