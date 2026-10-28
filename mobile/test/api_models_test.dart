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
      expect(detail.saved, isFalse);
    });

    test('parses the saved flag', () {
      final detail = CheckinDetail.fromJson({'id': 'abc', 'saved': true});
      expect(detail.saved, isTrue);

      final unsaved = CheckinDetail.fromJson({'id': 'abc', 'saved': false});
      expect(unsaved.saved, isFalse);
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

  group('CommentPage', () {
    test('reads the data rows and the paging metadata', () {
      final page = CommentPage.fromJson({
        'data': [
          {
            'id': 'm1',
            'user': {'handle': 'ada', 'display_name': 'Ada'},
            'body': 'Older',
            'created_at': '2026-10-25T10:00:00Z',
            'can_delete': false,
            'user_id': 'u1',
          },
          {
            'id': 'm2',
            'user': {'handle': 'grace', 'display_name': 'Grace'},
            'body': 'Newer',
            'created_at': '2026-10-26T10:00:00Z',
            'can_delete': false,
            'user_id': 'u2',
          },
        ],
        'meta': {'count': 2, 'has_more': true, 'next_before': 'm1'},
      });

      expect(page.items.map((comment) => comment.id), ['m1', 'm2']);
      expect(page.items.first.userId, 'u1');
      expect(page.count, 2);
      expect(page.hasMore, isTrue);
      expect(page.nextBefore, 'm1');
    });

    test('tolerates a page without meta', () {
      final page = CommentPage.fromJson({
        'data': [
          {'id': 'm1', 'body': 'Only one'},
        ],
      });

      expect(page.items.single.body, 'Only one');
      expect(page.count, 1);
      expect(page.hasMore, isFalse);
      expect(page.nextBefore, '');
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
      expect(passport.totals.checkins, 0);
      expect(passport.styles, isEmpty);
      expect(passport.venueKinds, isEmpty);
      expect(passport.countries, isEmpty);
      expect(passport.recent, isEmpty);
    });

    test('reads totals, stamps and a null recent rating', () {
      final passport = Passport.fromJson({
        'stats': {'checkins': 40},
        'totals': {
          'checkins': 40,
          'styles': 5,
          'venue_kinds': 3,
          'countries': 2,
        },
        'styles': [
          {'name': 'Cola', 'count': 9, 'last_at': '2026-10-01'},
          {'name': 'Lemonade'},
        ],
        'venue_kinds': [
          {'kind': 'shop', 'count': 4},
        ],
        'countries': [
          {'country': 'GB', 'count': 7},
        ],
        'recent': [
          {
            'name': 'Cola',
            'slug': 'cola',
            'style': 'Cola',
            'created_at': '2026-10-26T10:00:00Z',
            'rating': null,
          },
          {
            'name': 'Lime',
            'slug': 'lime',
            'style': 'Lemonade',
            'created_at': '2026-10-25T10:00:00Z',
            'rating': 17,
          },
        ],
      });

      expect(passport.totals.checkins, 40);
      expect(passport.totals.styles, 5);
      expect(passport.totals.venueKinds, 3);
      expect(passport.totals.countries, 2);
      expect(passport.styles.first.name, 'Cola');
      expect(passport.styles.first.count, 9);
      expect(passport.styles.first.lastAt, '2026-10-01');
      expect(passport.styles.last.count, 0);
      expect(passport.styles.last.lastAt, '');
      expect(passport.venueKinds.single.kind, 'shop');
      expect(passport.venueKinds.single.count, 4);
      expect(passport.countries.single.country, 'GB');
      expect(passport.countries.single.count, 7);
      expect(passport.recent.first.slug, 'cola');
      expect(passport.recent.first.rating, isNull);
      expect(passport.recent.last.rating, 17);
      expect(passport.recent.last.createdAt, '2026-10-25T10:00:00Z');
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

  group('Drink detail', () {
    test('reads nutrition, flavours, aliases and rating flags', () {
      final drink = Drink.fromJson({
        'id': 5,
        'slug': 'cola',
        'name': 'Cola',
        'description': 'A classic',
        'brand': 'Soda Co',
        'style': 'Cola',
        'container': 'can',
        'volume_ml': 330,
        'caffeine_mg_per_100ml': 10,
        'sugar_g_per_100ml': 10.6,
        'calories_kcal_per_100ml': 42,
        'sweetener': 'sugar',
        'carbonation': 'standard',
        'acid': 'phosphoric',
        'status': 'active',
        'rating': {
          'avg_rating': 16.5,
          'rater_count': 4,
          'rating_shown': true,
        },
        'flavours': [
          {'name': 'Cola', 'slug': 'cola', 'intensity': 4},
        ],
        'aliases': ['Coke', ''],
        'barcode_count': 2,
        'avg_rating': 16.5,
        'rater_count': 4,
        'rating_shown': true,
      });

      expect(drink.id, 5);
      expect(drink.description, 'A classic');
      expect(drink.caffeineMgPer100ml, 10);
      expect(drink.sugarGPer100ml, 10.6);
      expect(drink.caloriesKcalPer100ml, 42);
      expect(drink.sweetener, 'sugar');
      expect(drink.carbonation, 'standard');
      expect(drink.acid, 'phosphoric');
      expect(drink.status, 'active');
      expect(drink.rating, 16.5);
      expect(drink.raterCount, 4);
      expect(drink.ratingShown, isTrue);
      expect(drink.flavours.single.name, 'Cola');
      expect(drink.flavours.single.intensity, 4);
      expect(drink.aliases, ['Coke']);
      expect(drink.barcodeCount, 2);
    });

    test('reads the nested barcode shape', () {
      final drink = Drink.fromDetail({
        'Drink': {
          'id': 5,
          'slug': 'cola',
          'name': 'Cola',
          'description': 'A classic',
          'container': 'can',
          'volume_ml': 330,
          'caffeine_mg_per_100ml': 10,
          'sugar_g_per_100ml': 10.6,
          'calories_kcal_per_100ml': 42,
          'sweetener': 'sugar',
          'carbonation': 'standard',
          'acid': 'phosphoric',
          'status': 'active',
        },
        'BrandName': 'Soda Co',
        'StyleName': 'Cola',
      });

      expect(drink.slug, 'cola');
      expect(drink.brand, 'Soda Co');
      expect(drink.style, 'Cola');
      expect(drink.caffeineMgPer100ml, 10);
      expect(drink.sugarGPer100ml, 10.6);
    });

    test('tolerates a sparse drink', () {
      final drink = Drink.fromJson({'id': 1});
      expect(drink.name, 'Unknown soda');
      expect(drink.rating, isNull);
      expect(drink.flavours, isEmpty);
      expect(drink.aliases, isEmpty);
      expect(drink.ratingShown, isFalse);
    });
  });

  group('DrinkImage', () {
    test('reads the gallery DTO and derives a thumb URL', () {
      final image = DrinkImage.fromJson({
        'id': 9,
        'media_id': 'm-9',
        'url': '/media/m-9',
        'is_primary': true,
        'attribution': 'Ada',
        'created_at': '2026-10-26T10:00:00Z',
        'can_moderate': true,
      });

      expect(image.id, 9);
      expect(image.mediaId, 'm-9');
      expect(image.url, '/media/m-9');
      expect(image.thumbUrl, '/media/m-9/thumb');
      expect(image.isPrimary, isTrue);
      expect(image.attribution, 'Ada');
      expect(image.canModerate, isTrue);
    });

    test('prefers an explicit thumb URL and tolerates sparse rows', () {
      final image = DrinkImage.fromJson({
        'id': 3,
        'media_id': 'm-3',
        'thumb_url': '/custom/thumb',
      });
      expect(image.thumbUrl, '/custom/thumb');

      final empty = DrinkImage.fromJson({'id': 4});
      expect(empty.thumbUrl, '');
      expect(empty.isPrimary, isFalse);
    });
  });

  group('CheckinSummary', () {
    test('reads the checkins.Item payload including venue and photo', () {
      final item = CheckinSummary.fromJson({
        'id': 'c1',
        'user_id': 'u1',
        'drink_id': 5,
        'drink_name': 'Cola',
        'drink_slug': 'cola',
        'brand': 'Soda Co',
        'rating': 17,
        'serving': 'can',
        'note': 'Crisp',
        'venue_id': 7,
        'venue_name': 'Corner Shop',
        'visibility': 'friends',
        'created_at': '2026-10-26T10:00:00Z',
        'photo_url': '/media/m1',
      });

      expect(item.id, 'c1');
      expect(item.userId, 'u1');
      expect(item.drinkId, 5);
      expect(item.drinkName, 'Cola');
      expect(item.brand, 'Soda Co');
      expect(item.rating, 17);
      expect(item.serving, 'can');
      expect(item.venueId, 7);
      expect(item.venueName, 'Corner Shop');
      expect(item.createdAt, '2026-10-26T10:00:00Z');
      expect(item.photoUrl, '/media/m1');
    });

    test('tolerates an unrated sparse row', () {
      final item = CheckinSummary.fromJson({'id': 'c1'});
      expect(item.rating, isNull);
      expect(item.venueId, isNull);
      expect(item.drinkId, 0);
    });
  });

  group('FriendsDirectory', () {
    test('reads the capitalised Go keys', () {
      final directory = FriendsDirectory.fromJson({
        'Friends': [
          {'id': 'u1', 'handle': 'ada', 'display_name': 'Ada', 'since': '2026-01-01'},
        ],
        'Incoming': [
          {'id': 'f1', 'handle': 'bob', 'display_name': 'Bob', 'created_at': '2026-02-01'},
        ],
        'Outgoing': [
          {'id': 'f2', 'handle': 'cat'},
        ],
      });

      expect(directory.friends.single.handle, 'ada');
      expect(directory.friends.single.since, '2026-01-01');
      expect(directory.incoming.single.handle, 'bob');
      expect(directory.outgoing.single.displayName, 'cat');
    });

    test('accepts lowercase keys and a data wrapper', () {
      final directory = FriendsDirectory.fromJson({
        'data': {
          'friends': [
            {'id': 'u1', 'handle': 'ada'},
          ],
          'incoming': <Object>[],
        },
      });
      expect(directory.friends.single.displayName, 'ada');
      expect(directory.incoming, isEmpty);
      expect(directory.outgoing, isEmpty);
    });
  });

  group('AiDraftTurn', () {
    test('reads a full turn with a free-form draft', () {
      final turn = AiDraftTurn.fromJson({
        'session_id': 's1',
        'status': 'ready',
        'message': 'Here is a first draft.',
        'draft': {
          'name': 'Cherry Cola',
          'brand': 'Soda Co',
          'style': 'Cola',
          'container': 'can',
          'volume_ml': 355,
          'sugar_g_per_100ml': 10.6,
          'caffeine_mg_per_100ml': 12,
          'calories_kcal_per_100ml': 44,
          'description': 'Sharp cherry and a dry finish.',
          'flavours': [
            {'name': 'Cherry', 'intensity': 3},
          ],
        },
        'upstream_session_id': 'up-1',
      });

      expect(turn.sessionId, 's1');
      expect(turn.status, 'ready');
      expect(turn.message, 'Here is a first draft.');
      expect(turn.upstreamSessionId, 'up-1');
      expect(turn.draftName, 'Cherry Cola');
      expect(turn.canCommit, isTrue);
      expect(turn.draft['volume_ml'], 355);
      expect(turn.draft['sugar_g_per_100ml'], 10.6);
    });

    test('tolerates missing fields and an empty draft', () {
      final turn = AiDraftTurn.fromJson({
        'session_id': 's1',
        'draft': null,
      });

      expect(turn.sessionId, 's1');
      expect(turn.status, '');
      expect(turn.message, '');
      expect(turn.draft, isEmpty);
      expect(turn.upstreamSessionId, '');
      expect(turn.draftName, '');
      expect(turn.canCommit, isFalse);
    });

    test('a nameless draft cannot be committed', () {
      final turn = AiDraftTurn.fromJson({
        'session_id': 's1',
        'draft': {'brand': 'Soda Co', 'volume_ml': 330},
      });
      expect(turn.canCommit, isFalse);
    });
  });

  group('AiCommit', () {
    test('reads the committed drink', () {
      final commit = AiCommit.fromJson({
        'drink_id': 12,
        'slug': 'cherry-cola',
        'name': 'Cherry Cola',
        'status': 'provisional',
        'session_id': 's1',
        'message': 'provisional drink committed',
      });

      expect(commit.drinkId, 12);
      expect(commit.slug, 'cherry-cola');
      expect(commit.name, 'Cherry Cola');
      expect(commit.status, 'provisional');
      expect(commit.sessionId, 's1');
      expect(commit.message, 'provisional drink committed');
    });

    test('falls back to id and tolerates a sparse payload', () {
      final commit = AiCommit.fromJson({'id': 9, 'slug': 'lime'});
      expect(commit.drinkId, 9);
      expect(commit.slug, 'lime');
      expect(commit.name, '');
      expect(commit.status, '');
      expect(commit.sessionId, '');
      expect(commit.message, '');
    });

    test('an empty payload parses to defaults', () {
      final commit = AiCommit.fromJson({});
      expect(commit.drinkId, 0);
      expect(commit.slug, '');
    });
  });

  group('FeedItem saved projection', () {
    test('parses the nested saved detail shape', () {
      final item = FeedItem.fromJson({
        'id': 'c1',
        'user': {'handle': 'ada', 'display_name': 'Ada'},
        'drink': {'slug': 'cola', 'name': 'Cola'},
        'venue': {'name': 'Corner Shop'},
        'rating': 16,
        'note': 'Good',
        'saved': true,
      });

      expect(item.handle, 'ada');
      expect(item.displayName, 'Ada');
      expect(item.drinkName, 'Cola');
      expect(item.drinkSlug, 'cola');
      expect(item.venueName, 'Corner Shop');
      expect(item.rating, 16);
      expect(item.saved, isTrue);
    });

    test('keeps the flat feed shape', () {
      final item = FeedItem.fromJson({
        'id': 'c2',
        'handle': 'bob',
        'display_name': 'Bob',
        'drink_name': 'Lime',
        'drink_slug': 'lime',
        'toasts': 2,
      });
      expect(item.drinkName, 'Lime');
      expect(item.toasts, 2);
      expect(item.saved, isFalse);
    });
  });

  group('AccountSettings', () {
    test('reads the full settings payload', () {
      final settings = AccountSettings.fromJson({
        'profile': {
          'handle': 'ada',
          'display_name': 'Ada Lovelace',
          'bio': 'Fizz fan',
          'email': 'ada@soda.test',
          'email_verified': true,
          'role': 'admin',
        },
        'prefs': {
          'theme': 'dark',
          'units': 'imperial',
          'email_weekly': true,
          'email_friend_req': true,
          'push_enabled': true,
          'public_profile': true,
          'analytics_opt_out': true,
          'caffeine_bedtime': '21:30',
          'default_visibility': 'friends',
        },
        'sessions': [
          {
            'id': 9,
            'ip': '203.0.113.7',
            'user_agent': 'Firefox',
            'created_at': '2026-10-20T10:00:00Z',
            'last_seen_at': '2026-10-27T10:00:00Z',
            'current': true,
          },
        ],
        'token_count': 2,
        'blocked_count': 1,
      });

      expect(settings.profile.handle, 'ada');
      expect(settings.profile.displayName, 'Ada Lovelace');
      expect(settings.profile.bio, 'Fizz fan');
      expect(settings.profile.emailVerified, isTrue);
      expect(settings.profile.role, 'admin');
      expect(settings.prefs.theme, 'dark');
      expect(settings.prefs.units, 'imperial');
      expect(settings.prefs.emailFriendReq, isTrue);
      expect(settings.prefs.pushEnabled, isTrue);
      expect(settings.prefs.publicProfile, isTrue);
      expect(settings.prefs.analyticsOptOut, isTrue);
      expect(settings.prefs.caffeineBedtime, '21:30');
      expect(settings.prefs.defaultVisibility, 'friends');
      expect(settings.sessions.single.id, 9);
      expect(settings.sessions.single.ip, '203.0.113.7');
      expect(settings.sessions.single.current, isTrue);
      expect(settings.tokenCount, 2);
      expect(settings.blockedCount, 1);
    });

    test('tolerates an empty payload with default prefs', () {
      final settings = AccountSettings.fromJson({});

      expect(settings.profile.handle, '');
      expect(settings.profile.role, 'user');
      expect(settings.prefs.theme, 'system');
      expect(settings.prefs.units, 'metric');
      expect(settings.prefs.defaultVisibility, 'public');
      expect(settings.prefs.caffeineBedtime, '');
      expect(settings.prefs.emailWeekly, isFalse);
      expect(settings.sessions, isEmpty);
      expect(settings.tokenCount, 0);
      expect(settings.blockedCount, 0);
    });

    test('parses an empty sessions list and tolerates string ids', () {
      final settings = AccountSettings.fromJson({
        'profile': {'handle': 'ada', 'display_name': ''},
        'prefs': {'theme': '', 'units': '', 'default_visibility': ''},
        'sessions': [
          {'id': '12', 'current': 'true'},
        ],
      });

      expect(settings.profile.displayName, 'ada');
      expect(settings.prefs.theme, 'system');
      expect(settings.prefs.units, 'metric');
      expect(settings.prefs.defaultVisibility, 'public');
      expect(settings.sessions.single.id, 12);
      expect(settings.sessions.single.current, isTrue);

      final empty = AccountSettings.fromJson({'sessions': <Object>[]});
      expect(empty.sessions, isEmpty);
    });
  });

  group('BlockedUser', () {
    test('reads the block row and falls back to the handle', () {
      final blocked = BlockedUser.fromJson({
        'id': 'u2',
        'handle': 'bob',
        'display_name': 'Bob',
        'blocked_at': '2026-10-20T10:00:00Z',
      });

      expect(blocked.id, 'u2');
      expect(blocked.handle, 'bob');
      expect(blocked.displayName, 'Bob');
      expect(blocked.blockedAt, '2026-10-20T10:00:00Z');

      final sparse = BlockedUser.fromJson({'handle': 'cat'});
      expect(sparse.displayName, 'cat');
      expect(sparse.blockedAt, '');
      expect(sparse.id, '');
    });
  });

  group('AuthConfig', () {
    test('reads the public capabilities', () {
      final config = AuthConfig.fromJson({
        'registration_open': true,
        'password_fallback': true,
        'oidc_enabled': true,
        'email_verification_required': true,
      });

      expect(config.registrationOpen, isTrue);
      expect(config.passwordFallback, isTrue);
      expect(config.oidcEnabled, isTrue);
      expect(config.emailVerificationRequired, isTrue);
    });

    test('defaults every capability to false', () {
      final config = AuthConfig.fromJson({});

      expect(config.registrationOpen, isFalse);
      expect(config.passwordFallback, isFalse);
      expect(config.oidcEnabled, isFalse);
      expect(config.emailVerificationRequired, isFalse);
    });
  });

  group('RegistrationResult', () {
    test('derives verificationRequired from the status', () {
      expect(
        RegistrationResult.fromJson({'status': 'verification_sent'}).status,
        'verification_sent',
      );
      expect(
        RegistrationResult.fromJson({
          'status': 'verification_sent',
        }).verificationRequired,
        isTrue,
      );
      expect(
        RegistrationResult.fromJson({'status': 'created'}).verificationRequired,
        isFalse,
      );
    });

    test('tolerates a missing status', () {
      final result = RegistrationResult.fromJson({});

      expect(result.status, '');
      expect(result.verificationRequired, isFalse);
    });
  });
}
