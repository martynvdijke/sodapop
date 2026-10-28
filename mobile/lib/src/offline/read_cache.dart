import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/api_client.dart';
import '../api/models.dart';

/// A cached drink detail: the drink plus the gallery it was shown with.
typedef CachedDrink = ({Drink drink, List<DrinkImage> images});

/// A cached feed snapshot, used when the network is unavailable.
class ReadCache {
  static const maxFeedItems = 40;
  static const maxDrinks = 30;

  static const _feedPrefix = 'sodapop.cache.feed.v1.';
  static const _drinkPrefix = 'sodapop.cache.drink.v1.';
  static const _drinkIndex = 'sodapop.cache.drinks.v1';

  /// Stores [items] for [scope] (`friends` or `following`), newest first and
  /// capped at [maxFeedItems]. Best effort: storage failures are swallowed.
  Future<void> saveFeed(String scope, List<FeedItem> items) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final rows = items.take(maxFeedItems).map(_feedToJson).toList();
      await prefs.setString('$_feedPrefix$scope', jsonEncode(rows));
    } catch (_) {
      // Caching must never break a successful load.
    }
  }

  /// Returns the cached feed for [scope], or null when there is none.
  Future<List<FeedItem>?> loadFeed(String scope) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('$_feedPrefix$scope');
      if (raw == null) return null;
      final rows = jsonDecode(raw);
      if (rows is! List) return null;
      return rows
          .whereType<Map>()
          .map((row) => FeedItem.fromJson(row.cast<String, dynamic>()))
          .toList();
    } catch (_) {
      return null;
    }
  }

  /// Stores one drink with its gallery and keeps only the [maxDrinks] most
  /// recently saved entries.
  Future<void> saveDrink(Drink drink, List<DrinkImage> images) async {
    final slug = drink.slug.trim();
    if (slug.isEmpty) return;
    try {
      final prefs = await SharedPreferences.getInstance();
      final index = _decodeIndex(prefs.getString(_drinkIndex));
      var previous = 0;
      for (final row in index) {
        final value = _savedAt(row);
        if (value > previous) previous = value;
      }
      final now = DateTime.now().toUtc().millisecondsSinceEpoch;
      final savedAt = now > previous ? now : previous + 1;
      final entry = jsonEncode({
        'saved_at': savedAt,
        'drink': _drinkToJson(drink),
        'images': images.map(_drinkImageToJson).toList(),
      });
      await prefs.setString('$_drinkPrefix$slug', entry);

      index.removeWhere((row) => row['slug'] == slug);
      index.add({'slug': slug, 'saved_at': savedAt});
      index.sort((a, b) => _savedAt(b).compareTo(_savedAt(a)));
      final dropped = index.length > maxDrinks
          ? index.sublist(maxDrinks)
          : const <Map<String, dynamic>>[];
      final kept = index.length > maxDrinks
          ? index.sublist(0, maxDrinks)
          : index;
      await prefs.setString(_drinkIndex, jsonEncode(kept));
      for (final row in dropped) {
        final old = row['slug'];
        if (old is String && old.isNotEmpty) {
          await prefs.remove('$_drinkPrefix$old');
        }
      }
    } catch (_) {
      // Caching must never break a successful load.
    }
  }

  /// Returns the cached drink and gallery for [slug], or null when absent.
  Future<CachedDrink?> loadDrink(String slug) async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('$_drinkPrefix$slug');
      if (raw == null) return null;
      final row = jsonDecode(raw);
      if (row is! Map) return null;
      final drinkJson = row['drink'];
      if (drinkJson is! Map) return null;
      final imagesJson = row['images'];
      return (
        drink: Drink.fromJson(drinkJson.cast<String, dynamic>()),
        images: imagesJson is List
            ? imagesJson
                  .whereType<Map>()
                  .map(
                    (image) =>
                        DrinkImage.fromJson(image.cast<String, dynamic>()),
                  )
                  .toList()
            : const <DrinkImage>[],
      );
    } catch (_) {
      return null;
    }
  }

  /// Drops every cached feed and drink, for example on sign-out.
  Future<void> clear() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      for (final key in prefs.getKeys().toList()) {
        if (key.startsWith(_feedPrefix) || key.startsWith(_drinkPrefix)) {
          await prefs.remove(key);
        }
      }
      await prefs.remove(_drinkIndex);
    } catch (_) {
      // Best effort.
    }
  }

  static List<Map<String, dynamic>> _decodeIndex(String? raw) {
    if (raw == null) return <Map<String, dynamic>>[];
    try {
      final rows = jsonDecode(raw);
      if (rows is! List) return <Map<String, dynamic>>[];
      return rows
          .whereType<Map>()
          .map((row) => row.cast<String, dynamic>())
          .where((row) => row['slug'] is String)
          .toList();
    } catch (_) {
      return <Map<String, dynamic>>[];
    }
  }

  static int _savedAt(Map<String, dynamic> row) {
    final value = row['saved_at'];
    if (value is num) return value.toInt();
    return int.tryParse('${value ?? ''}') ?? 0;
  }
}

/// True when [error] looks like a connectivity problem rather than a server
/// rejection. Kept here so both the feed and drink detail can share it.
bool isOfflineError(Object error) {
  if (error is DioException) {
    return error.response == null ||
        error.type == DioExceptionType.connectionError ||
        error.type == DioExceptionType.connectionTimeout ||
        error.type == DioExceptionType.sendTimeout ||
        error.type == DioExceptionType.receiveTimeout;
  }
  final described = ApiClient.describe(error);
  return described.statusCode == null;
}

Map<String, dynamic> _feedToJson(FeedItem item) => <String, dynamic>{
  'id': item.id,
  'handle': item.handle,
  'display_name': item.displayName,
  'drink_name': item.drinkName,
  'drink_slug': item.drinkSlug,
  'rating': item.rating,
  'note': item.note,
  'venue_name': item.venueName,
  'toasts': item.toasts,
  'toasted': item.toasted,
  'saved': item.saved,
};

Map<String, dynamic> _drinkToJson(Drink drink) => <String, dynamic>{
  'id': drink.id,
  'slug': drink.slug,
  'name': drink.name,
  'brand': drink.brand,
  'style': drink.style,
  'volume_ml': drink.volumeMl,
  'container': drink.container,
  'rating': drink.rating,
  'description': drink.description,
  'caffeine_mg_per_100ml': drink.caffeineMgPer100ml,
  'sugar_g_per_100ml': drink.sugarGPer100ml,
  'calories_kcal_per_100ml': drink.caloriesKcalPer100ml,
  'sweetener': drink.sweetener,
  'carbonation': drink.carbonation,
  'acid': drink.acid,
  'status': drink.status,
  'rater_count': drink.raterCount,
  'rating_shown': drink.ratingShown,
  'flavours': drink.flavours
      .map(
        (flavour) => <String, dynamic>{
          'name': flavour.name,
          'slug': flavour.slug,
          'intensity': flavour.intensity,
        },
      )
      .toList(),
  'aliases': drink.aliases,
  'barcode_count': drink.barcodeCount,
};

Map<String, dynamic> _drinkImageToJson(DrinkImage image) => <String, dynamic>{
  'id': image.id,
  'media_id': image.mediaId,
  'url': image.url,
  'thumb_url': image.thumbUrl,
  'is_primary': image.isPrimary,
  'attribution': image.attribution,
  'created_at': image.createdAt,
  'can_moderate': image.canModerate,
};
