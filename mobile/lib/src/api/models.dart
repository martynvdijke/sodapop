import 'dart:collection';

int? _int(Object? value) =>
    value is num ? value.toInt() : int.tryParse('${value ?? ''}');

double? _double(Object? value) =>
    value is num ? value.toDouble() : double.tryParse('${value ?? ''}');

String _string(Object? value) => value == null ? '' : '$value';

/// The drink's beverage type, defaulting to `soda` when the server omits it.
/// The nested Ent projection uses a Go field name, so accept both spellings.
String _beverageType(Map<String, dynamic> json) {
  final value = _firstString([json['beverage_type'], json['BeverageType']]);
  return value.isEmpty ? 'soda' : value;
}

/// The check-in preparation method, defaulting to `none`.
String _prepMethod(Map<String, dynamic> json) {
  final value = _firstString([json['prep_method'], json['PrepMethod']]);
  return value.isEmpty ? 'none' : value;
}

bool _bool(Object? value) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  final text = '${value ?? ''}'.toLowerCase();
  return text == 'true' || text == '1';
}

Map<String, dynamic> _map(Object? value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return value.cast<String, dynamic>();
  return const <String, dynamic>{};
}

List<dynamic> _list(Object? value) => value is List ? value : const <dynamic>[];

String _initialFor(String displayName, String handle) {
  final source = displayName.trim().isNotEmpty
      ? displayName.trim()
      : handle.trim();
  if (source.isEmpty) return '';
  return String.fromCharCode(source.runes.first).toUpperCase();
}

String _firstString(List<Object?> values) {
  for (final value in values) {
    final text = _string(value);
    if (text.isNotEmpty) return text;
  }
  return '';
}

Object? _firstValue(List<Object?> values) {
  for (final value in values) {
    if (value != null) return value;
  }
  return null;
}

List<dynamic> _firstList(List<Object?> values) {
  for (final value in values) {
    if (value is List) return value;
  }
  return const <dynamic>[];
}

/// A drink pinned to a profile as its favourite.
class FavouriteDrink {
  const FavouriteDrink({
    required this.id,
    this.name = '',
    this.slug = '',
    this.brand = '',
    this.style = '',
    this.imageUrl,
    this.averageRating,
  });

  final int id;
  final String name;
  final String slug;
  final String brand;
  final String style;

  /// Absolute URL for the drink's primary image, or null when it has none.
  final String? imageUrl;

  /// The hidden-below-three-raters average, or null when unshown.
  final double? averageRating;

  factory FavouriteDrink.fromJson(Map<String, dynamic> json) {
    final image = _string(json['image_url']);
    return FavouriteDrink(
      id: _int(json['id']) ?? 0,
      name: _string(json['name']),
      slug: _string(json['slug']),
      brand: _string(json['brand']),
      style: _string(json['style']),
      imageUrl: image.isEmpty ? null : image,
      averageRating: _double(json['average_rating']),
    );
  }
}

class Me {
  const Me({
    required this.id,
    required this.handle,
    required this.displayName,
    required this.email,
    this.avatarUrl,
    this.favouriteDrink,
  });

  final String id;
  final String handle;
  final String displayName;
  final String email;

  /// Absolute URL for the uploaded avatar, or null when none is set.
  final String? avatarUrl;

  /// The drink pinned as this member's favourite, or null when unpinned.
  final FavouriteDrink? favouriteDrink;

  factory Me.fromJson(Map<String, dynamic> json) {
    final avatar = _string(json['avatar_url']);
    final favourite = _map(json['favourite_drink']);
    return Me(
      id: '${json['id']}',
      handle: '${json['handle']}',
      displayName: '${json['display_name'] ?? json['handle']}',
      email: '${json['email'] ?? ''}',
      avatarUrl: avatar.isEmpty ? null : avatar,
      favouriteDrink: favourite.isEmpty
          ? null
          : FavouriteDrink.fromJson(favourite),
    );
  }
}

/// One flavour note attached to a drink.
class DrinkFlavour {
  const DrinkFlavour({this.name = '', this.slug = '', this.intensity = 0});

  final String name;
  final String slug;
  final int intensity;

  factory DrinkFlavour.fromJson(Map<String, dynamic> json) => DrinkFlavour(
    name: _string(json['name']),
    slug: _string(json['slug']),
    intensity: _int(json['intensity']) ?? 0,
  );
}

class Drink {
  const Drink({
    required this.id,
    required this.slug,
    required this.name,
    this.brand = '',
    this.style = '',
    this.beverageType = 'soda',
    this.abvPercent = 0,
    this.volumeMl = 0,
    this.container = '',
    this.rating,
    this.description = '',
    this.caffeineMgPer100ml = 0,
    this.sugarGPer100ml = 0,
    this.caloriesKcalPer100ml = 0,
    this.sweetener = '',
    this.carbonation = '',
    this.acid = '',
    this.status = '',
    this.raterCount = 0,
    this.ratingShown = false,
    this.flavours = const [],
    this.aliases = const [],
    this.barcodeCount = 0,
  });

  final int id;
  final String slug;
  final String name;
  final String brand;
  final String style;

  /// One of `soda`, `water`, `tea`, `coffee`, `na_beer`, `na_wine`,
  /// `na_spirit` or `other`. Older servers omit the field, so it defaults to
  /// `soda`.
  final String beverageType;

  /// Alcohol by volume 0–100. Zero for every non-alcoholic drink.
  final double abvPercent;
  final int volumeMl;
  final String container;
  final double? rating;
  final String description;
  final int caffeineMgPer100ml;
  final double sugarGPer100ml;
  final int caloriesKcalPer100ml;
  final String sweetener;
  final String carbonation;
  final String acid;
  final String status;
  final int raterCount;
  final bool ratingShown;
  final List<DrinkFlavour> flavours;
  final List<String> aliases;
  final int barcodeCount;

  String get subtitle {
    final bits = [
      brand,
      style,
      if (volumeMl > 0) '$volumeMl ml',
    ].where((v) => v.isNotEmpty);
    return bits.join(' · ');
  }

  factory Drink.fromJson(Map<String, dynamic> json) => Drink._parse(json);

  /// The barcode endpoint serialises `catalog.DrinkDetail`, whose nested Ent
  /// entity uses Go field names.
  factory Drink.fromDetail(Map<String, dynamic> json) {
    final drink = json['Drink'];
    if (drink is! Map) return Drink.fromJson(json);
    return Drink._parse(
      drink.cast<String, dynamic>(),
      brand: _firstString([json['BrandName'], json['brand']]),
      style: _firstString([json['StyleName'], json['style']]),
    );
  }

  /// Handles both the flat `/api/v1/drinks/{slug}` DTO and a nested Ent drink.
  factory Drink._parse(
    Map<String, dynamic> json, {
    String? brand,
    String? style,
  }) {
    final ratingJson = _map(json['rating']);
    final flavoursJson = _list(json['flavours']);
    return Drink(
      id: _int(json['id']) ?? 0,
      slug: _string(json['slug']),
      name: _firstString([json['name'], 'Unknown soda']),
      brand: brand ?? _string(json['brand']),
      style: style ?? _string(json['style']),
      beverageType: _beverageType(json),
      abvPercent: _double(_firstValue([json['abv_percent'], json['abv']])) ?? 0,
      volumeMl: _int(json['volume_ml']) ?? 0,
      container: _string(json['container']),
      rating: _double(
        _firstValue([
          json['avg_rating'],
          ratingJson['avg_rating'],
          json['rating'] is Map ? null : json['rating'],
        ]),
      ),
      description: _string(json['description']),
      caffeineMgPer100ml: _int(json['caffeine_mg_per_100ml']) ?? 0,
      sugarGPer100ml: _double(json['sugar_g_per_100ml']) ?? 0,
      caloriesKcalPer100ml: _int(json['calories_kcal_per_100ml']) ?? 0,
      sweetener: _string(json['sweetener']),
      carbonation: _string(json['carbonation']),
      acid: _string(json['acid']),
      status: _string(json['status']),
      raterCount:
          _int(_firstValue([json['rater_count'], ratingJson['rater_count']])) ??
          0,
      ratingShown: _bool(
        _firstValue([json['rating_shown'], ratingJson['rating_shown']]),
      ),
      flavours: flavoursJson
          .whereType<Map>()
          .map((row) => DrinkFlavour.fromJson(row.cast<String, dynamic>()))
          .toList(),
      aliases: _list(
        json['aliases'],
      ).map(_string).where((alias) => alias.isNotEmpty).toList(),
      barcodeCount: _int(json['barcode_count']) ?? 0,
    );
  }
}

/// One image in a drink's gallery.
class DrinkImage {
  const DrinkImage({
    required this.id,
    this.mediaId = '',
    this.url = '',
    this.thumbUrl = '',
    this.isPrimary = false,
    this.attribution = '',
    this.createdAt = '',
    this.canModerate = false,
  });

  final int id;
  final String mediaId;
  final String url;
  final String thumbUrl;
  final bool isPrimary;
  final String attribution;
  final String createdAt;
  final bool canModerate;

  factory DrinkImage.fromJson(Map<String, dynamic> json) {
    final mediaId = _string(json['media_id']);
    final url = _string(json['url']);
    final thumbUrl = _firstString([
      json['thumb_url'],
      mediaId.isNotEmpty ? '/media/$mediaId/thumb' : '',
      url.isNotEmpty ? '$url/thumb' : '',
    ]);
    return DrinkImage(
      id: _int(json['id']) ?? 0,
      mediaId: mediaId,
      url: url,
      thumbUrl: thumbUrl,
      isPrimary: _bool(json['is_primary']),
      attribution: _string(json['attribution']),
      createdAt: _string(json['created_at']),
      canModerate: _bool(json['can_moderate']),
    );
  }
}

/// A friend tagged on a check-in. The server sends only these three fields.
class TaggedFriend {
  const TaggedFriend({this.id = '', this.handle = '', this.displayName = ''});

  final String id;
  final String handle;
  final String displayName;

  String get name => displayName.isNotEmpty
      ? displayName
      : handle.isNotEmpty
      ? '@$handle'
      : '';

  factory TaggedFriend.fromJson(Map<String, dynamic> json) => TaggedFriend(
    id: _string(json['id']),
    handle: _string(json['handle']),
    displayName: _string(json['display_name']),
  );
}

class FeedItem {
  const FeedItem({
    required this.id,
    required this.handle,
    required this.displayName,
    required this.drinkName,
    required this.drinkSlug,
    this.brand = '',
    this.rating,
    this.note = '',
    this.prepMethod = 'none',
    this.prepNote = '',
    this.venueName = '',
    this.photoId = '',
    this.photoMime = '',
    this.createdAt = '',
    this.toasts = 0,
    this.comments = 0,
    this.toasted = false,
    this.saved = false,
    this.atHome = false,
    this.taggedFriends = const [],
  });

  final String id;
  final String handle;
  final String displayName;
  final String drinkName;
  final String drinkSlug;
  final String brand;
  final double? rating;
  final String note;
  final String prepMethod;
  final String prepNote;
  final String venueName;
  final String photoId;

  /// `image/...` or `video/...` when the feed row carries it. Older rows omit
  /// the mime, so only the id is guaranteed.
  final String photoMime;
  final String createdAt;
  final int toasts;

  /// How many comments the check-in carries. The feed DTO names this
  /// `comments`; the check-in detail projection names it `comments_count`.
  final int comments;
  final bool toasted;
  final bool saved;

  /// True when the pour was logged at home rather than at a venue or a
  /// coordinate. Check-ins no longer carry coordinates.
  final bool atHome;
  final List<TaggedFriend> taggedFriends;

  /// The friends feed is flat; `/me/saved` reuses the nested check-in detail
  /// projection and adds `saved`, so both shapes are accepted here.
  factory FeedItem.fromJson(Map<String, dynamic> json) {
    final user = _map(json['user']);
    final drink = _map(json['drink']);
    final venue = _map(json['venue']);
    final photo = _map(json['photo']);
    final drinkName = _firstString([json['drink_name'], drink['name'], 'Soda']);
    return FeedItem(
      id: _string(json['id']),
      handle: _firstString([json['handle'], user['handle']]),
      displayName: _firstString([
        json['display_name'],
        user['display_name'],
        json['handle'],
        user['handle'],
      ]),
      drinkName: drinkName,
      drinkSlug: _firstString([
        json['drink_slug'],
        drink['slug'],
        json['slug'],
      ]),
      brand: _firstString([drink['brand'], json['brand'], json['drink_brand']]),
      rating: _double(_firstValue([json['rating'], json['avg_rating']])),
      note: _string(json['note']),
      prepMethod: _prepMethod(json),
      prepNote: _string(json['prep_note']),
      venueName: _firstString([json['venue_name'], venue['name']]),
      photoId: _firstString([photo['id'], json['photo_id']]),
      photoMime: _firstString([photo['mime'], json['photo_mime']]),
      createdAt: _firstString([json['created_at'], json['createdAt']]),
      toasts: _int(_firstValue([json['toasts'], json['toast_count']])) ?? 0,
      comments:
          _int(
            _firstValue([
              json['comments'],
              json['comments_count'],
              json['comment_count'],
            ]),
          ) ??
          0,
      toasted: _bool(json['toasted']),
      saved: _bool(json['saved']),
      atHome: _bool(json['at_home']),
      taggedFriends: _list(json['tagged_friends'])
          .whereType<Map>()
          .map((row) => TaggedFriend.fromJson(row.cast<String, dynamic>()))
          .toList(),
    );
  }

  FeedItem copyWith({int? toasts, int? comments, bool? toasted, bool? saved}) =>
      FeedItem(
        id: id,
        handle: handle,
        displayName: displayName,
        drinkName: drinkName,
        drinkSlug: drinkSlug,
        brand: brand,
        rating: rating,
        note: note,
        venueName: venueName,
        photoId: photoId,
        photoMime: photoMime,
        createdAt: createdAt,
        toasts: toasts ?? this.toasts,
        comments: comments ?? this.comments,
        toasted: toasted ?? this.toasted,
        saved: saved ?? this.saved,
        atHome: atHome,
        taggedFriends: taggedFriends,
      );
}

/// One pour in the Nearby feed: a feed row plus the server's rounded distance.
/// The coordinates that produced it are never echoed back or kept here.
class NearbyItem {
  const NearbyItem({required this.checkin, this.distanceM = 0});

  final FeedItem checkin;
  final int distanceM;

  /// Accepts both the flat feed row (`FeedItem` fields + `distance_m`) and a
  /// nested `checkin` object, so a future shape change stays readable.
  factory NearbyItem.fromJson(Map<String, dynamic> json) {
    final nested = _map(json['checkin']);
    final source = nested.isEmpty ? json : nested;
    return NearbyItem(
      checkin: FeedItem.fromJson(source),
      distanceM:
          _int(_firstValue([json['distance_m'], source['distance_m']])) ?? 0,
    );
  }
}

/// One page of the Nearby feed. [nextBefore] is the cursor for older pours.
class NearbyPage {
  const NearbyPage({
    this.items = const [],
    this.count = 0,
    this.hasMore = false,
    this.nextBefore = '',
  });

  final List<NearbyItem> items;

  /// How many pours this page carries, not the size of the whole radius.
  final int count;
  final bool hasMore;

  /// Cursor for the next page, empty when [hasMore] is false.
  final String nextBefore;

  factory NearbyPage.fromJson(Map<String, dynamic> json) {
    final items = _list(json['data'])
        .whereType<Map>()
        .map((row) => NearbyItem.fromJson(row.cast<String, dynamic>()))
        .toList();
    final meta = _map(json['meta']);
    return NearbyPage(
      items: items,
      count: _int(meta['count']) ?? items.length,
      hasMore: _bool(meta['has_more']),
      nextBefore: _string(meta['next_before']),
    );
  }
}

/// An Open Food Facts guess for a barcode that is not in the catalog yet.
class BarcodeSuggestion {
  const BarcodeSuggestion({
    this.name = '',
    this.brand = '',
    this.quantity = '',
    this.imageUrl = '',
    this.barcode = '',
  });

  final String name;
  final String brand;
  final String quantity;
  final String imageUrl;
  final String barcode;

  bool get isEmpty =>
      name.isEmpty && brand.isEmpty && quantity.isEmpty && barcode.isEmpty;

  /// The label volume in millilitres when [quantity] is a plain metric amount.
  ///
  /// Open Food Facts also returns imperial and multipack strings; those are
  /// left to the drinker rather than guessed.
  int? get volumeMl {
    final match = RegExp(
      r'^\s*([0-9]+(?:[.,][0-9]+)?)\s*(ml|cl|l)\s*$',
      caseSensitive: false,
    ).firstMatch(quantity);
    if (match == null) return null;
    final amount = double.tryParse(match.group(1)!.replaceAll(',', '.'));
    if (amount == null) return null;
    final unit = match.group(2)!.toLowerCase();
    final ml = switch (unit) {
      'l' => amount * 1000,
      'cl' => amount * 10,
      _ => amount,
    };
    final rounded = ml.round();
    return rounded >= 1 && rounded <= 5000 ? rounded : null;
  }

  factory BarcodeSuggestion.fromJson(Map<String, dynamic> json) =>
      BarcodeSuggestion(
        name: _string(json['name']).trim(),
        brand: _string(json['brand']).trim(),
        quantity: _string(json['quantity']).trim(),
        imageUrl: _string(json['image_url']).trim(),
        barcode: _string(json['barcode']).trim(),
      );
}

class BarcodeResult {
  const BarcodeResult({required this.status, this.drink, this.suggested});

  final String status;
  final Drink? drink;

  /// The server's Open Food Facts guess when the barcode is not in the local
  /// catalog.
  final BarcodeSuggestion? suggested;

  bool get matched => status == 'matched' && drink != null;

  factory BarcodeResult.fromJson(Map<String, dynamic> json) => BarcodeResult(
    status: '${json['status'] ?? 'unknown'}',
    drink: json['drink'] is Map
        ? Drink.fromDetail((json['drink'] as Map).cast<String, dynamic>())
        : null,
    suggested: json['suggested'] is Map
        ? BarcodeSuggestion.fromJson(
            (json['suggested'] as Map).cast<String, dynamic>(),
          )
        : null,
  );
}

class Venue {
  const Venue({
    required this.name,
    this.id = 0,
    required this.kind,
    this.slug = '',
    this.city = '',
    this.address = '',
    this.region = '',
    this.country = '',
    this.website = '',
    this.lat = 0,
    this.lng = 0,
    this.distanceM = 0,
    this.source = 'local',
    this.provider = '',
    this.providerId = '',
  });

  /// Zero for external (OpenStreetMap) suggestions, which have no Sodapop id.
  final int id;
  final String name;
  final String kind;
  final String slug;
  final String city;
  final String address;
  final String region;
  final String country;
  final String website;
  final double lat;
  final double lng;

  /// Distance from the coordinates sent with the request. Zero when the
  /// server did not have a location to compare against.
  final int distanceM;

  /// `local` for a Sodapop venue, `external` for a suggestion that cannot be
  /// attached as a `venue_id`.
  final String source;
  final String provider;
  final String providerId;

  bool get isExternal => source == 'external';

  factory Venue.fromJson(Map<String, dynamic> json) => Venue(
    id: _int(json['id']) ?? 0,
    name: _string(json['name']),
    kind: _firstString([json['kind'], 'other']),
    slug: _string(json['slug']),
    city: _string(json['city']),
    address: _string(json['address']),
    region: _string(json['region']),
    country: _string(json['country']),
    website: _string(json['website']),
    lat: _double(json['lat']) ?? 0,
    lng: _double(json['lng']) ?? 0,
    distanceM: _int(json['distance_m']) ?? 0,
    source: _firstString([json['source'], 'local']),
    provider: _string(json['provider']),
    providerId: _string(json['provider_id']),
  );
}

/// The venue search response: local rows plus the external lookup state.
class VenueSearch {
  const VenueSearch({this.venues = const [], this.externalStatus = ''});

  final List<Venue> venues;

  /// `unavailable` when the server could not reach the external provider.
  final String externalStatus;

  bool get externalUnavailable => externalStatus == 'unavailable';

  List<Venue> get local => venues.where((venue) => !venue.isExternal).toList();
  List<Venue> get external =>
      venues.where((venue) => venue.isExternal).toList();
}

class AuthStatus {
  const AuthStatus({
    required this.oidcEnabled,
    required this.passwordFallback,
    this.issuer = '',
  });

  final bool oidcEnabled;
  final bool passwordFallback;
  final String issuer;

  factory AuthStatus.fromJson(Map<String, dynamic> json) => AuthStatus(
    oidcEnabled: json['enabled'] == true,
    passwordFallback: json['password_fallback'] != false,
    issuer: '${json['issuer'] ?? ''}',
  );
}

/// One check-in with its display fields and viewer-specific capabilities.
class CheckinDetail {
  const CheckinDetail({
    required this.id,
    this.handle = '',
    this.displayName = '',
    this.initial = '',
    this.drinkSlug = '',
    this.drinkName = '',
    this.brand = '',
    this.style = '',
    this.rating,
    this.note = '',
    this.prepMethod = 'none',
    this.prepNote = '',
    this.visibility = '',
    this.toasts = 0,
    this.toasted = false,
    this.saved = false,
    this.createdAt = '',
    this.venueId,
    this.venueName = '',
    this.photoId = '',
    this.photoMime = '',
    this.atHome = false,
    this.taggedFriends = const [],
    this.canDelete = false,
    this.canToast = false,
    this.canComment = false,
    this.commentsCount = 0,
  });

  final String id;
  final String handle;
  final String displayName;
  final String initial;
  final String drinkSlug;
  final String drinkName;
  final String brand;
  final String style;
  final double? rating;
  final String note;

  /// How the drink was prepared (`none` when the server omits it).
  final String prepMethod;
  final String prepNote;
  final String visibility;
  final int toasts;
  final bool toasted;
  final bool saved;
  final String createdAt;
  final int? venueId;
  final String venueName;
  final String photoId;
  final String photoMime;

  /// True when the pour was logged with the @home marker.
  final bool atHome;
  final List<TaggedFriend> taggedFriends;

  final bool canDelete;
  final bool canToast;
  final bool canComment;
  final int commentsCount;

  factory CheckinDetail.fromJson(Map<String, dynamic> json) {
    final user = _map(json['user']);
    final drink = _map(json['drink']);
    final venue = _map(json['venue']);
    final photo = _map(json['photo']);

    final handle = _firstString([
      user['handle'],
      json['handle'],
      json['user_handle'],
    ]);
    final displayName = _firstString([
      user['display_name'],
      json['display_name'],
      json['user_display_name'],
      handle,
    ]);
    final initial = _firstString([
      user['initial'],
      json['initial'],
      json['user_initial'],
      _initialFor(displayName, handle),
    ]);
    final style = _firstString([
      drink['style'],
      json['style'],
      json['drink_style'],
    ]);

    return CheckinDetail(
      id: _string(json['id']),
      handle: handle,
      displayName: displayName,
      initial: initial,
      drinkSlug: _firstString([drink['slug'], json['drink_slug']]),
      drinkName: _firstString([drink['name'], json['drink_name']]),
      brand: _firstString([drink['brand'], json['brand'], json['drink_brand']]),
      style: style,
      rating: _double(json['rating']),
      note: _string(json['note']),
      prepMethod: _prepMethod(json),
      prepNote: _string(json['prep_note']),
      visibility: _string(json['visibility']),
      toasts: _int(_firstValue([json['toasts'], json['toast_count']])) ?? 0,
      toasted: _bool(json['toasted']),
      saved: _bool(json['saved']),
      createdAt: _firstString([json['created_at'], json['createdAt']]),
      venueId: _int(_firstValue([venue['id'], json['venue_id']])),
      venueName: _firstString([venue['name'], json['venue_name']]),
      photoId: _firstString([photo['id'], json['photo_id']]),
      photoMime: _firstString([photo['mime'], json['photo_mime']]),
      atHome: _bool(json['at_home']),
      taggedFriends: _list(json['tagged_friends'])
          .whereType<Map>()
          .map((row) => TaggedFriend.fromJson(row.cast<String, dynamic>()))
          .toList(),
      canDelete: _bool(json['can_delete']),
      canToast: _bool(json['can_toast']),
      canComment: _bool(json['can_comment']),
      commentsCount:
          _int(_firstValue([json['comments_count'], json['comment_count']])) ??
          0,
    );
  }
}

/// One row in a personal or friend's check-in history (`checkins.Item`).
class CheckinSummary {
  const CheckinSummary({
    required this.id,
    this.userId = '',
    this.drinkId = 0,
    this.drinkName = '',
    this.drinkSlug = '',
    this.brand = '',
    this.rating,
    this.serving = '',
    this.note = '',
    this.prepMethod = 'none',
    this.prepNote = '',
    this.venueId,
    this.venueName = '',
    this.atHome = false,
    this.visibility = '',
    this.createdAt = '',
    this.photoUrl = '',
  });

  final String id;
  final String userId;
  final int drinkId;
  final String drinkName;
  final String drinkSlug;
  final String brand;

  /// Quarter-star integer 0–20, or null when unrated.
  final int? rating;
  final String serving;
  final String note;
  final String prepMethod;
  final String prepNote;
  final int? venueId;
  final String venueName;
  final bool atHome;
  final String visibility;
  final String createdAt;
  final String photoUrl;

  factory CheckinSummary.fromJson(Map<String, dynamic> json) {
    final user = _map(json['user']);
    final drink = _map(json['drink']);
    final venue = _map(json['venue']);
    final photo = _map(json['photo']);
    return CheckinSummary(
      id: _string(json['id']),
      userId: _firstString([json['user_id'], user['id']]),
      drinkId: _int(_firstValue([json['drink_id'], drink['id']])) ?? 0,
      drinkName: _firstString([json['drink_name'], drink['name']]),
      drinkSlug: _firstString([json['drink_slug'], drink['slug']]),
      brand: _firstString([json['brand'], json['drink_brand'], drink['brand']]),
      rating: _int(json['rating']),
      serving: _string(json['serving']),
      note: _string(json['note']),
      prepMethod: _prepMethod(json),
      prepNote: _string(json['prep_note']),
      venueId: _int(_firstValue([json['venue_id'], venue['id']])),
      venueName: _firstString([json['venue_name'], venue['name']]),
      atHome: _bool(json['at_home']),
      visibility: _string(json['visibility']),
      createdAt: _firstString([json['created_at'], json['createdAt']]),
      photoUrl: _firstString([json['photo_url'], photo['url'], photo['id']]),
    );
  }
}

/// One page of check-in history. It is a [List] of rows so existing callers
/// that only iterate the page keep working; [hasNext] and [page] come from the
/// optional `meta` block when the server sends one.
class CheckinPage extends ListBase<CheckinSummary> {
  CheckinPage({
    List<CheckinSummary> items = const [],
    this.hasNext = false,
    this.page = 1,
    this.count = 0,
    this.nextBefore = '',
  }) : _items = List<CheckinSummary>.of(items);

  final List<CheckinSummary> _items;
  final bool hasNext;
  final int page;
  final int count;
  final String nextBefore;

  @override
  int get length => _items.length;

  @override
  set length(int newLength) => _items.length = newLength;

  @override
  CheckinSummary operator [](int index) => _items[index];

  @override
  void operator []=(int index, CheckinSummary value) => _items[index] = value;

  /// Accepts the wrapped `{"data": [...], "meta": {...}}` page body. A bare
  /// `{"data": {"items": [...]}}` collection is handled too.
  factory CheckinPage.fromJson(Map<String, dynamic> json) {
    final data = json['data'];
    late final List<dynamic> rows;
    if (data is List) {
      rows = data;
    } else if (data is Map) {
      final items = data['items'] ?? data['results'];
      rows = items is List ? items : const <dynamic>[];
    } else {
      rows = const <dynamic>[];
    }
    final items = rows
        .whereType<Map>()
        .map((row) => CheckinSummary.fromJson(row.cast<String, dynamic>()))
        .toList();
    final meta = _map(json['meta']);
    return CheckinPage(
      items: items,
      hasNext: _bool(meta['has_next']),
      page: _int(meta['page']) ?? 1,
      count: _int(meta['count']) ?? items.length,
      nextBefore: _string(meta['next_before']),
    );
  }
}

/// An accepted friend with the date the friendship started.
class FriendUser {
  const FriendUser({
    required this.id,
    this.handle = '',
    this.displayName = '',
    this.since = '',
  });

  final String id;
  final String handle;
  final String displayName;
  final String since;

  factory FriendUser.fromJson(Map<String, dynamic> json) {
    final handle = _string(json['handle']);
    return FriendUser(
      id: _string(json['id']),
      handle: handle,
      displayName: _firstString([json['display_name'], handle]),
      since: _firstString([json['since'], json['created_at']]),
    );
  }
}

/// A friend request that has not been accepted yet.
class PendingFriend {
  const PendingFriend({
    required this.id,
    this.handle = '',
    this.displayName = '',
    this.createdAt = '',
  });

  final String id;
  final String handle;
  final String displayName;
  final String createdAt;

  factory PendingFriend.fromJson(Map<String, dynamic> json) {
    final handle = _string(json['handle']);
    return PendingFriend(
      id: _string(json['id']),
      handle: handle,
      displayName: _firstString([json['display_name'], handle]),
      createdAt: _firstString([json['created_at'], json['createdAt']]),
    );
  }
}

/// `/api/v1/friends` payload. `FriendsDetailed` in Go has no JSON tags, so the
/// server emits capitalised keys; lowercase is accepted too.
class FriendsDirectory {
  const FriendsDirectory({
    this.friends = const [],
    this.incoming = const [],
    this.outgoing = const [],
  });

  final List<FriendUser> friends;
  final List<PendingFriend> incoming;
  final List<PendingFriend> outgoing;

  factory FriendsDirectory.fromJson(Map<String, dynamic> json) {
    final root = json['data'] is Map ? _map(json['data']) : json;
    List<PendingFriend> pending(List<Object?> values) => _firstList(values)
        .whereType<Map>()
        .map((row) => PendingFriend.fromJson(row.cast<String, dynamic>()))
        .toList();
    return FriendsDirectory(
      friends: _firstList([root['Friends'], root['friends']])
          .whereType<Map>()
          .map((row) => FriendUser.fromJson(row.cast<String, dynamic>()))
          .toList(),
      incoming: pending([root['Incoming'], root['incoming']]),
      outgoing: pending([root['Outgoing'], root['outgoing']]),
    );
  }
}

/// One comment on a check-in.
class Comment {
  const Comment({
    required this.id,
    this.userId = '',
    this.handle = '',
    this.displayName = '',
    this.initial = '',
    this.body = '',
    this.createdAt = '',
    this.canDelete = false,
  });

  final String id;
  final String userId;
  final String handle;
  final String displayName;
  final String initial;
  final String body;
  final String createdAt;
  final bool canDelete;

  factory Comment.fromJson(Map<String, dynamic> json) {
    final user = _map(json['user']);
    final handle = _firstString([user['handle'], json['handle']]);
    final displayName = _firstString([
      user['display_name'],
      json['display_name'],
      handle,
    ]);
    return Comment(
      id: _string(json['id']),
      userId: _firstString([json['user_id'], user['id']]),
      handle: handle,
      displayName: displayName,
      initial: _firstString([
        user['initial'],
        json['initial'],
        _initialFor(displayName, handle),
      ]),
      body: _firstString([json['body'], json['text']]),
      createdAt: _firstString([json['created_at'], json['createdAt']]),
      canDelete: _bool(json['can_delete']),
    );
  }
}

/// One page of comments on a check-in. Comments are oldest first within the
/// page and [nextBefore] is the id of the oldest one shown.
class CommentPage {
  const CommentPage({
    this.items = const [],
    this.count = 0,
    this.hasMore = false,
    this.nextBefore = '',
  });

  final List<Comment> items;

  /// How many comments this page carries, not the size of the whole thread.
  final int count;
  final bool hasMore;

  /// Cursor for the next older page, empty when [hasMore] is false.
  final String nextBefore;

  factory CommentPage.fromJson(Map<String, dynamic> json) {
    final items = _list(json['data'])
        .whereType<Map>()
        .map((row) => Comment.fromJson(row.cast<String, dynamic>()))
        .toList();
    final meta = _map(json['meta']);
    return CommentPage(
      items: items,
      count: _int(meta['count']) ?? items.length,
      hasMore: _bool(meta['has_more']),
      nextBefore: _string(meta['next_before']),
    );
  }
}

/// Aggregate counters shown on a profile.
class ProfileStats {
  const ProfileStats({
    this.checkins = 0,
    this.drinks = 0,
    this.toasts = 0,
    this.badges = 0,
  });

  final int checkins;
  final int drinks;
  final int toasts;
  final int badges;

  factory ProfileStats.fromJson(Map<String, dynamic> json) => ProfileStats(
    checkins: _int(_firstValue([json['checkins'], json['pours']])) ?? 0,
    drinks:
        _int(
          _firstValue([
            json['drinks'],
            json['distinct_drinks'],
            json['unique_drinks'],
            json['styles'],
          ]),
        ) ??
        0,
    toasts: _int(_firstValue([json['toasts'], json['toasts_received']])) ?? 0,
    badges: json['badges'] is List ? 0 : _int(json['badges']) ?? 0,
  );
}

class ProfileBadge {
  const ProfileBadge({
    required this.slug,
    this.name = '',
    this.description = '',
    this.awardedAt = '',
  });

  final String slug;
  final String name;
  final String description;
  final String awardedAt;

  factory ProfileBadge.fromJson(Map<String, dynamic> json) => ProfileBadge(
    slug: _string(json['slug']),
    name: _firstString([json['name'], json['title']]),
    description: _string(json['description']),
    awardedAt: _firstString([json['awarded_at'], json['earned_at']]),
  );
}

class ProfileTopStyle {
  const ProfileTopStyle({required this.slug, this.name = '', this.count = 0});

  final String slug;
  final String name;
  final int count;

  factory ProfileTopStyle.fromJson(Map<String, dynamic> json) =>
      ProfileTopStyle(
        slug: _string(json['slug']),
        name: _firstString([json['name'], json['style']]),
        count: _int(json['count']) ?? 0,
      );
}

/// A profile as seen by the signed-in viewer.
class UserProfile {
  const UserProfile({
    required this.id,
    required this.handle,
    this.displayName = '',
    this.bio = '',
    this.createdAt = '',
    this.stats = const ProfileStats(),
    this.followerCount = 0,
    this.followingCount = 0,
    this.isFollowing = false,
    this.isFriend = false,
    this.canBlock = false,
    this.badges = const [],
    this.topStyles = const [],
    this.lists = const [],
    this.avatarUrl,
    this.favouriteDrink,
  });

  final String id;
  final String handle;
  final String displayName;
  final String bio;
  final String createdAt;
  final ProfileStats stats;
  final int followerCount;
  final int followingCount;
  final bool isFollowing;
  final bool isFriend;
  final bool canBlock;
  final List<ProfileBadge> badges;
  final List<ProfileTopStyle> topStyles;
  final List<ListSummary> lists;

  /// Absolute URL for the member's avatar, or null when none is set.
  final String? avatarUrl;

  /// The drink pinned as this member's favourite, or null when unpinned.
  final FavouriteDrink? favouriteDrink;

  factory UserProfile.fromJson(Map<String, dynamic> json) {
    final statsJson = _map(_firstValue([json['stats'], json['counts']]));
    final handle = _string(json['handle']);
    final displayName = _firstString([
      json['display_name'],
      json['displayName'],
      handle,
    ]);
    final avatar = _string(json['avatar_url']);
    final favourite = _map(json['favourite_drink']);
    return UserProfile(
      id: _firstString([json['id'], json['user_id']]),
      handle: handle,
      displayName: displayName,
      bio: _string(json['bio']),
      createdAt: _firstString([json['created_at'], json['joined_at']]),
      stats: statsJson.isEmpty
          ? ProfileStats(
              checkins:
                  _int(_firstValue([json['checkins'], json['pours']])) ?? 0,
              drinks:
                  _int(
                    _firstValue([
                      json['drinks'],
                      json['unique_drinks'],
                      json['styles'],
                    ]),
                  ) ??
                  0,
              toasts: _int(json['toasts']) ?? 0,
            )
          : ProfileStats.fromJson(statsJson),
      followerCount:
          _int(_firstValue([json['follower_count'], json['followers']])) ?? 0,
      followingCount:
          _int(_firstValue([json['following_count'], json['following']])) ?? 0,
      isFollowing: _bool(json['is_following']),
      isFriend: _bool(_firstValue([json['is_friend'], json['are_friends']])),
      canBlock: _bool(json['can_block']),
      badges: _list(_firstValue([json['badges'], json['earned_badges']]))
          .whereType<Map>()
          .map((row) => ProfileBadge.fromJson(row.cast<String, dynamic>()))
          .toList(),
      topStyles: _list(_firstValue([json['top_styles'], json['topStyles']]))
          .whereType<Map>()
          .map((row) => ProfileTopStyle.fromJson(row.cast<String, dynamic>()))
          .toList(),
      lists: _list(json['lists'])
          .whereType<Map>()
          .map((row) => ListSummary.fromJson(row.cast<String, dynamic>()))
          .toList(),
      avatarUrl: avatar.isEmpty ? null : avatar,
      favouriteDrink: favourite.isEmpty
          ? null
          : FavouriteDrink.fromJson(favourite),
    );
  }
}

class ListSummary {
  const ListSummary({
    required this.id,
    this.name = '',
    this.description = '',
    this.visibility = '',
    this.itemCount = 0,
    this.isSystem = false,
  });

  final String id;
  final String name;
  final String description;
  final String visibility;
  final int itemCount;
  final bool isSystem;

  factory ListSummary.fromJson(Map<String, dynamic> json) => ListSummary(
    id: _string(json['id']),
    name: _string(json['name']),
    description: _string(json['description']),
    visibility: _string(json['visibility']),
    itemCount:
        _int(
          _firstValue([json['item_count'], json['count'], json['items_count']]),
        ) ??
        0,
    isSystem: _bool(json['is_system']),
  );
}

class ListItem {
  const ListItem({
    required this.drinkId,
    this.slug = '',
    this.name = '',
    this.brand = '',
    this.note = '',
    this.createdAt = '',
  });

  final int drinkId;
  final String slug;
  final String name;
  final String brand;
  final String note;
  final String createdAt;

  factory ListItem.fromJson(Map<String, dynamic> json) {
    final drink = _map(json['drink']);
    return ListItem(
      drinkId: _int(_firstValue([json['drink_id'], drink['id']])) ?? 0,
      slug: _firstString([json['slug'], drink['slug']]),
      name: _firstString([json['name'], drink['name']]),
      brand: _firstString([json['brand'], drink['brand']]),
      note: _string(json['note']),
      createdAt: _firstString([json['created_at'], json['added_at']]),
    );
  }
}

class ListDetail extends ListSummary {
  const ListDetail({
    required super.id,
    super.name,
    super.description,
    super.visibility,
    super.itemCount,
    super.isSystem,
    this.items = const [],
  });

  final List<ListItem> items;

  factory ListDetail.fromJson(Map<String, dynamic> json) {
    final summary = ListSummary.fromJson(json);
    return ListDetail(
      id: summary.id,
      name: summary.name,
      description: summary.description,
      visibility: summary.visibility,
      itemCount: summary.itemCount,
      isSystem: summary.isSystem,
      items: _list(_firstValue([json['items'], json['drinks']]))
          .whereType<Map>()
          .map((row) => ListItem.fromJson(row.cast<String, dynamic>()))
          .toList(),
    );
  }
}

/// One notification row. Older servers send the copy as `text`.
class NotificationItem {
  const NotificationItem({
    required this.id,
    this.kind = '',
    this.subjectId = '',
    this.actorHandle = '',
    this.actorDisplayName = '',
    this.actorInitial = '',
    this.href = '',
    this.body = '',
    this.read = false,
    this.createdAt = '',
    this.friendRequestId,
  });

  final String id;
  final String kind;
  final String subjectId;
  final String actorHandle;
  final String actorDisplayName;
  final String actorInitial;
  final String href;
  final String body;
  final bool read;
  final String createdAt;
  final String? friendRequestId;

  factory NotificationItem.fromJson(Map<String, dynamic> json) {
    final actor = _map(json['actor']);
    final handle = _firstString([actor['handle'], json['actor_handle']]);
    final displayName = _firstString([
      actor['display_name'],
      json['actor_display_name'],
      handle,
    ]);
    final friendRequest = _firstValue([
      json['friend_request_id'],
      json['request_id'],
      actor['friend_request_id'],
    ]);
    return NotificationItem(
      id: _string(json['id']),
      kind: _string(json['kind']),
      subjectId: _string(json['subject_id']),
      actorHandle: handle,
      actorDisplayName: displayName,
      actorInitial: _firstString([
        actor['initial'],
        json['actor_initial'],
        _initialFor(displayName, handle),
      ]),
      href: _firstString([json['href'], json['url']]),
      body: _firstString([json['text'], json['body'], json['message']]),
      read: _bool(json['read']),
      createdAt: _firstString([json['created_at'], json['createdAt']]),
      friendRequestId: friendRequest == null ? null : _string(friendRequest),
    );
  }
}

/// A venue the viewer has saved.
class SavedVenue extends Venue {
  const SavedVenue({
    required super.id,
    required super.name,
    required super.kind,
    super.slug,
    super.city,
    super.address,
    super.region,
    super.country,
    super.website,
    super.lat,
    super.lng,
  });

  factory SavedVenue.fromJson(Map<String, dynamic> json) {
    final venue = Venue.fromJson(json);
    return SavedVenue(
      id: venue.id,
      name: venue.name,
      kind: venue.kind,
      slug: venue.slug,
      city: venue.city,
      address: venue.address,
      region: venue.region,
      country: venue.country,
      website: venue.website,
      lat: venue.lat,
      lng: venue.lng,
    );
  }
}

class InsightPoint {
  const InsightPoint({
    this.date = '',
    this.sugarG = 0,
    this.caffeineMg = 0,
    this.caloriesKcal = 0,
    this.pours = 0,
  });

  final String date;
  final double sugarG;
  final double caffeineMg;
  final int caloriesKcal;
  final int pours;

  factory InsightPoint.fromJson(Map<String, dynamic> json) => InsightPoint(
    date: _string(json['date']),
    sugarG: _double(json['sugar_g']) ?? 0,
    caffeineMg: _double(json['caffeine_mg']) ?? 0,
    caloriesKcal: _int(json['calories_kcal']) ?? 0,
    pours: _int(json['pours']) ?? 0,
  );
}

class InsightDrink {
  const InsightDrink({
    this.slug = '',
    this.name = '',
    this.brand = '',
    this.pours = 0,
    this.sugarG = 0,
    this.caffeineMg = 0,
  });

  final String slug;
  final String name;
  final String brand;
  final int pours;
  final double sugarG;
  final double caffeineMg;

  factory InsightDrink.fromJson(Map<String, dynamic> json) => InsightDrink(
    slug: _string(json['slug']),
    name: _string(json['name']),
    brand: _string(json['brand']),
    pours: _int(json['pours']) ?? 0,
    sugarG: _double(json['sugar_g']) ?? 0,
    caffeineMg: _double(json['caffeine_mg']) ?? 0,
  );
}

/// Nutrition summary for the viewer's check-ins in a rolling window.
class Insights {
  const Insights({
    this.rangeDays = 0,
    this.from = '',
    this.to = '',
    this.pours = 0,
    this.sugarG = 0,
    this.caffeineMg = 0,
    this.caloriesKcal = 0,
    this.avgSugarPerDayG = 0,
    this.avgCaffeinePerDayMg = 0,
    this.series = const [],
    this.topDrinks = const [],
    this.bedtime = '',
    this.lastCaffeineAt = '',
    this.caffeineClearBy = '',
    this.lateCaffeine = false,
  });

  final int rangeDays;
  final String from;
  final String to;
  final int pours;
  final double sugarG;
  final double caffeineMg;
  final int caloriesKcal;
  final double avgSugarPerDayG;
  final double avgCaffeinePerDayMg;
  final List<InsightPoint> series;
  final List<InsightDrink> topDrinks;
  final String bedtime;
  final String lastCaffeineAt;
  final String caffeineClearBy;
  final bool lateCaffeine;

  factory Insights.fromJson(Map<String, dynamic> json) => Insights(
    rangeDays: _int(_firstValue([json['range_days'], json['days']])) ?? 0,
    from: _string(json['from']),
    to: _string(json['to']),
    pours: _int(json['pours']) ?? 0,
    sugarG: _double(json['sugar_g']) ?? 0,
    caffeineMg: _double(json['caffeine_mg']) ?? 0,
    caloriesKcal: _int(json['calories_kcal']) ?? 0,
    avgSugarPerDayG: _double(json['avg_sugar_per_day_g']) ?? 0,
    avgCaffeinePerDayMg: _double(json['avg_caffeine_per_day_mg']) ?? 0,
    series: _list(json['series'])
        .whereType<Map>()
        .map((row) => InsightPoint.fromJson(row.cast<String, dynamic>()))
        .toList(),
    topDrinks: _list(_firstValue([json['top_drinks'], json['topDrinks']]))
        .whereType<Map>()
        .map((row) => InsightDrink.fromJson(row.cast<String, dynamic>()))
        .toList(),
    bedtime: _string(json['bedtime']),
    lastCaffeineAt: _string(json['last_caffeine_at']),
    caffeineClearBy: _string(json['caffeine_clear_by']),
    lateCaffeine: _bool(json['late_caffeine']),
  );
}

/// Another member whose ratings resemble the viewer's.
class Twin {
  const Twin({
    this.handle = '',
    this.displayName = '',
    this.score = 0,
    this.commonDrinks = 0,
  });

  final String handle;
  final String displayName;
  final int score;
  final int commonDrinks;

  factory Twin.fromJson(Map<String, dynamic> json) => Twin(
    handle: _string(json['handle']),
    displayName: _firstString([json['display_name'], json['handle']]),
    score: _int(json['score']) ?? 0,
    commonDrinks:
        _int(_firstValue([json['common_drinks'], json['common']])) ?? 0,
  );
}

class Recommendation {
  const Recommendation({
    this.drinkId = 0,
    this.slug = '',
    this.name = '',
    this.brand = '',
    this.style = '',
    this.score = 0,
    this.reason = '',
  });

  final int drinkId;
  final String slug;
  final String name;
  final String brand;
  final String style;
  final double score;
  final String reason;

  factory Recommendation.fromJson(Map<String, dynamic> json) => Recommendation(
    drinkId: _int(json['drink_id']) ?? 0,
    slug: _string(json['slug']),
    name: _string(json['name']),
    brand: _string(json['brand']),
    style: _string(json['style']),
    score: _double(json['score']) ?? 0,
    reason: _string(json['reason']),
  );
}

class BadgeStatus {
  const BadgeStatus({
    required this.slug,
    this.name = '',
    this.description = '',
    this.kind = '',
    this.earned = false,
    this.level = 0,
    this.awardedAt = '',
    this.progress = 0,
    this.target = 0,
    this.percent = 0,
  });

  final String slug;
  final String name;
  final String description;
  final String kind;
  final bool earned;
  final int level;
  final String awardedAt;
  final int progress;
  final int target;
  final int percent;

  factory BadgeStatus.fromJson(Map<String, dynamic> json) => BadgeStatus(
    slug: _string(json['slug']),
    name: _string(json['name']),
    description: _string(json['description']),
    kind: _string(json['kind']),
    earned: _bool(json['earned']),
    level: _int(json['level']) ?? 0,
    awardedAt: _firstString([json['awarded_at'], json['earned_at']]),
    progress: _int(json['progress']) ?? 0,
    target: _int(json['target']) ?? 0,
    percent: _int(json['percent']) ?? 0,
  );
}

/// Lifetime counters summarised on the passport header.
class PassportTotals {
  const PassportTotals({
    this.checkins = 0,
    this.styles = 0,
    this.venueKinds = 0,
    this.countries = 0,
  });

  final int checkins;
  final int styles;
  final int venueKinds;
  final int countries;

  factory PassportTotals.fromJson(Map<String, dynamic> json) => PassportTotals(
    checkins: _int(json['checkins']) ?? 0,
    styles: _int(json['styles']) ?? 0,
    venueKinds:
        _int(_firstValue([json['venue_kinds'], json['venueKinds']])) ?? 0,
    countries: _int(json['countries']) ?? 0,
  );
}

/// One style the viewer has checked into, with its most recent pour.
class PassportStyle {
  const PassportStyle({this.name = '', this.count = 0, this.lastAt = ''});

  final String name;
  final int count;
  final String lastAt;

  factory PassportStyle.fromJson(Map<String, dynamic> json) => PassportStyle(
    name: _firstString([json['name'], json['style']]),
    count: _int(json['count']) ?? 0,
    lastAt: _firstString([json['last_at'], json['lastAt']]),
  );
}

/// One venue kind the viewer has visited.
class PassportVenueKind {
  const PassportVenueKind({this.kind = '', this.count = 0});

  final String kind;
  final int count;

  factory PassportVenueKind.fromJson(Map<String, dynamic> json) =>
      PassportVenueKind(
        kind: _string(json['kind']),
        count: _int(json['count']) ?? 0,
      );
}

/// One country the viewer has checked in from.
class PassportCountry {
  const PassportCountry({this.country = '', this.count = 0});

  final String country;
  final int count;

  factory PassportCountry.fromJson(Map<String, dynamic> json) =>
      PassportCountry(
        country: _string(json['country']),
        count: _int(json['count']) ?? 0,
      );
}

/// A recent pour shown on the passport.
class PassportRecent {
  const PassportRecent({
    this.name = '',
    this.slug = '',
    this.style = '',
    this.createdAt = '',
    this.rating,
  });

  final String name;
  final String slug;
  final String style;
  final String createdAt;

  /// Quarter-star integer 0–20, or null when unrated.
  final int? rating;

  factory PassportRecent.fromJson(Map<String, dynamic> json) => PassportRecent(
    name: _string(json['name']),
    slug: _string(json['slug']),
    style: _string(json['style']),
    createdAt: _firstString([json['created_at'], json['createdAt']]),
    rating: _int(json['rating']),
  );
}

/// The viewer's lifetime passport: stats, badges and the stamp collection.
class Passport {
  const Passport({
    this.stats = const ProfileStats(),
    this.badges = const [],
    this.totals = const PassportTotals(),
    this.styles = const [],
    this.venueKinds = const [],
    this.countries = const [],
    this.recent = const [],
  });

  final ProfileStats stats;
  final List<ProfileBadge> badges;
  final PassportTotals totals;
  final List<PassportStyle> styles;
  final List<PassportVenueKind> venueKinds;
  final List<PassportCountry> countries;
  final List<PassportRecent> recent;

  factory Passport.fromJson(Map<String, dynamic> json) => Passport(
    stats: ProfileStats.fromJson(_map(json['stats'])),
    badges: _list(_firstValue([json['badges'], json['earned_badges']]))
        .whereType<Map>()
        .map((row) => ProfileBadge.fromJson(row.cast<String, dynamic>()))
        .toList(),
    totals: PassportTotals.fromJson(_map(json['totals'])),
    styles: _list(json['styles'])
        .whereType<Map>()
        .map((row) => PassportStyle.fromJson(row.cast<String, dynamic>()))
        .toList(),
    venueKinds: _list(_firstValue([json['venue_kinds'], json['venueKinds']]))
        .whereType<Map>()
        .map((row) => PassportVenueKind.fromJson(row.cast<String, dynamic>()))
        .toList(),
    countries: _list(json['countries'])
        .whereType<Map>()
        .map((row) => PassportCountry.fromJson(row.cast<String, dynamic>()))
        .toList(),
    recent: _list(json['recent'])
        .whereType<Map>()
        .map((row) => PassportRecent.fromJson(row.cast<String, dynamic>()))
        .toList(),
  );
}

class MixComponent {
  const MixComponent({
    required this.drinkId,
    this.slug = '',
    this.name = '',
    this.parts = 0,
    this.volumeMl = 0,
  });

  final int drinkId;
  final String slug;
  final String name;
  final int parts;
  final int volumeMl;

  factory MixComponent.fromJson(Map<String, dynamic> json) => MixComponent(
    drinkId: _int(json['drink_id']) ?? 0,
    slug: _string(json['slug']),
    name: _string(json['name']),
    parts: _int(json['parts']) ?? 0,
    volumeMl: _int(json['volume_ml']) ?? 0,
  );

  Map<String, dynamic> toJson() => <String, dynamic>{
    'drink_id': drinkId,
    if (parts > 0) 'parts': parts,
    if (volumeMl > 0) 'volume_ml': volumeMl,
  };
}

class MixRecipe {
  const MixRecipe({
    required this.id,
    this.slug = '',
    this.name = '',
    this.notes = '',
    this.visibility = '',
    this.createdBy = '',
    this.createdAt = '',
    this.components = const [],
  });

  final String id;
  final String slug;
  final String name;
  final String notes;
  final String visibility;
  final String createdBy;
  final String createdAt;
  final List<MixComponent> components;

  factory MixRecipe.fromJson(Map<String, dynamic> json) => MixRecipe(
    id: _string(json['id']),
    slug: _string(json['slug']),
    name: _string(json['name']),
    notes: _string(json['notes']),
    visibility: _string(json['visibility']),
    createdBy: _string(json['created_by']),
    createdAt: _string(json['created_at']),
    components: _list(json['components'])
        .whereType<Map>()
        .map((row) => MixComponent.fromJson(row.cast<String, dynamic>()))
        .toList(),
  );
}

/// One drink recently seen at a venue.
class VenueDrink {
  const VenueDrink({
    this.slug = '',
    this.name = '',
    this.brand = '',
    this.lastSeenAt = '',
    this.onOffer = false,
  });

  final String slug;
  final String name;
  final String brand;
  final String lastSeenAt;
  final bool onOffer;

  factory VenueDrink.fromJson(Map<String, dynamic> json) => VenueDrink(
    slug: _firstString([json['drink_slug'], json['slug']]),
    name: _firstString([json['drink_name'], json['name']]),
    brand: _string(json['brand']),
    lastSeenAt: _firstString([json['last_seen_at'], json['created_at']]),
    onOffer: _bool(json['on_offer']),
  );
}

class VenueDetail {
  const VenueDetail({required this.venue, this.drinks = const []});

  final Venue venue;
  final List<VenueDrink> drinks;

  factory VenueDetail.fromJson(Map<String, dynamic> json) {
    final venueJson = _firstValue([json['venue'], json]);
    return VenueDetail(
      venue: Venue.fromJson(_map(venueJson)),
      drinks: _list(_firstValue([json['drinks'], json['recent_drinks']]))
          .whereType<Map>()
          .map((row) => VenueDrink.fromJson(row.cast<String, dynamic>()))
          .toList(),
    );
  }
}

/// One drink a member reported seeing in stock at a venue.
class ShelfEntry {
  const ShelfEntry({
    this.drinkId = 0,
    this.drinkSlug = '',
    this.drinkName = '',
    this.brand = '',
    this.style = '',
    this.priceCents,
    this.note = '',
    this.seenAt = '',
    this.ageDays = 0,
    this.band = '',
    this.reporters = 0,
    this.mine = false,
    this.reportId,
    this.source = '',
  });

  final int drinkId;
  final String drinkSlug;
  final String drinkName;
  final String brand;
  final String style;

  /// Price in minor units, with no currency attached. Null when unreported.
  final int? priceCents;
  final String note;
  final String seenAt;

  /// Whole days between [seenAt] and the server's clock.
  final int ageDays;

  /// `fresh` or `stale`, as decided by the server.
  final String band;
  final int reporters;
  final bool mine;

  /// The viewer's own report id, present only when [mine] is true.
  final String? reportId;

  /// `report`, `checkin` or `both`.
  final String source;

  factory ShelfEntry.fromJson(Map<String, dynamic> json) {
    final reportId = _string(json['report_id']);
    return ShelfEntry(
      drinkId: _int(json['drink_id']) ?? 0,
      drinkSlug: _string(json['drink_slug']),
      drinkName: _string(json['drink_name']),
      brand: _string(json['brand']),
      style: _string(json['style']),
      priceCents: _int(json['price_cents']),
      note: _string(json['note']),
      seenAt: _string(json['seen_at']),
      ageDays: _int(json['age_days']) ?? 0,
      band: _string(json['band']),
      reporters: _int(json['reporters']) ?? 0,
      mine: _bool(json['mine']),
      reportId: reportId.isEmpty ? null : reportId,
      source: _string(json['source']),
    );
  }
}

/// Which wishlist events the server may notify the member about.
class WishlistAlerts {
  const WishlistAlerts({this.friendCheckins = false, this.venueShelf = false});

  final bool friendCheckins;
  final bool venueShelf;

  factory WishlistAlerts.fromJson(Map<String, dynamic> json) => WishlistAlerts(
    friendCheckins: _bool(json['friend_checkins']),
    venueShelf: _bool(json['venue_shelf']),
  );

  Map<String, dynamic> toJson() => <String, dynamic>{
    'friend_checkins': friendCheckins,
    'venue_shelf': venueShelf,
  };

  WishlistAlerts copyWith({bool? friendCheckins, bool? venueShelf}) =>
      WishlistAlerts(
        friendCheckins: friendCheckins ?? this.friendCheckins,
        venueShelf: venueShelf ?? this.venueShelf,
      );
}

class MyStats {
  const MyStats({
    this.checkins = 0,
    this.uniqueDrinks = 0,
    this.avgRating,
    this.toastsReceived = 0,
    this.badges = 0,
  });

  final int checkins;
  final int uniqueDrinks;
  final double? avgRating;
  final int toastsReceived;
  final int badges;

  factory MyStats.fromJson(Map<String, dynamic> json) => MyStats(
    checkins: _int(json['checkins']) ?? 0,
    uniqueDrinks: _int(json['unique_drinks']) ?? 0,
    avgRating: _double(json['avg_rating']),
    toastsReceived: _int(json['toasts_received']) ?? 0,
    badges: _int(json['badges']) ?? 0,
  );
}

class VenueMedia {
  const VenueMedia({
    required this.id,
    this.mediaId = '',
    this.kind = '',
    this.label = '',
    this.uploaderHandle = '',
    this.createdAt = '',
    this.canRemove = false,
  });

  final String id;
  final String mediaId;
  final String kind;
  final String label;
  final String uploaderHandle;
  final String createdAt;
  final bool canRemove;

  factory VenueMedia.fromJson(Map<String, dynamic> json) => VenueMedia(
    id: _string(json['id']),
    mediaId: _string(json['media_id']),
    kind: _string(json['kind']),
    label: _string(json['label']),
    uploaderHandle: _string(json['uploader_handle']),
    createdAt: _string(json['created_at']),
    canRemove: _bool(json['can_remove']),
  );
}

/// One turn of a conversational AI drink draft.
///
/// [draft] stays free-form because the assistant can add keys without a new
/// release. Known keys are `name`, `brand`, `style`, `container`, `volume_ml`,
/// `description`, the per-100 ml nutrients and `flavours`.
class AiDraftTurn {
  const AiDraftTurn({
    this.sessionId = '',
    this.status = '',
    this.message = '',
    this.draft = const <String, dynamic>{},
    this.upstreamSessionId = '',
  });

  final String sessionId;
  final String status;
  final String message;
  final Map<String, dynamic> draft;
  final String upstreamSessionId;

  /// The draft's name, or an empty string while it is still forming. The
  /// server refuses to commit a draft without one.
  String get draftName => _string(draft['name']);

  bool get canCommit => draftName.trim().isNotEmpty;

  factory AiDraftTurn.fromJson(Map<String, dynamic> json) => AiDraftTurn(
    sessionId: _string(json['session_id']),
    status: _string(json['status']),
    message: _string(json['message']),
    draft: _map(json['draft']),
    upstreamSessionId: _string(json['upstream_session_id']),
  );
}

/// The private profile block returned by `/api/v1/me/settings`.
class SettingsProfile {
  const SettingsProfile({
    this.handle = '',
    this.displayName = '',
    this.bio = '',
    this.email = '',
    this.emailVerified = false,
    this.role = '',
    this.avatarUrl,
  });

  final String handle;
  final String displayName;
  final String bio;
  final String email;
  final bool emailVerified;
  final String role;

  /// Absolute URL for the uploaded avatar, or null when none is set.
  final String? avatarUrl;

  factory SettingsProfile.fromJson(Map<String, dynamic> json) {
    final handle = _string(json['handle']);
    final avatar = _string(json['avatar_url']);
    return SettingsProfile(
      handle: handle,
      displayName: _firstString([json['display_name'], handle]),
      bio: _string(json['bio']),
      email: _string(json['email']),
      emailVerified: _bool(json['email_verified']),
      role: _firstString([json['role'], 'user']),
      avatarUrl: avatar.isEmpty ? null : avatar,
    );
  }
}

/// The editable preference fields on the settings screen.
class SettingsPrefs {
  const SettingsPrefs({
    this.theme = 'system',
    this.units = 'metric',
    this.emailWeekly = false,
    this.emailFriendReq = false,
    this.pushEnabled = false,
    this.publicProfile = false,
    this.analyticsOptOut = false,
    this.caffeineBedtime = '',
    this.defaultVisibility = 'public',
  });

  final String theme;
  final String units;
  final bool emailWeekly;
  final bool emailFriendReq;
  final bool pushEnabled;
  final bool publicProfile;
  final bool analyticsOptOut;
  final String caffeineBedtime;
  final String defaultVisibility;

  factory SettingsPrefs.fromJson(Map<String, dynamic> json) => SettingsPrefs(
    theme: _firstString([json['theme'], 'system']),
    units: _firstString([json['units'], 'metric']),
    emailWeekly: _bool(json['email_weekly']),
    emailFriendReq: _bool(json['email_friend_req']),
    pushEnabled: _bool(json['push_enabled']),
    publicProfile: _bool(json['public_profile']),
    analyticsOptOut: _bool(json['analytics_opt_out']),
    caffeineBedtime: _string(json['caffeine_bedtime']),
    defaultVisibility: _firstString([json['default_visibility'], 'public']),
  );
}

/// One browser session in the settings screen.
class SettingsSession {
  const SettingsSession({
    this.id = 0,
    this.ip = '',
    this.userAgent = '',
    this.createdAt = '',
    this.lastSeenAt = '',
    this.current = false,
  });

  final int id;
  final String ip;
  final String userAgent;
  final String createdAt;
  final String lastSeenAt;
  final bool current;

  factory SettingsSession.fromJson(Map<String, dynamic> json) =>
      SettingsSession(
        id: _int(json['id']) ?? 0,
        ip: _string(json['ip']),
        userAgent: _string(json['user_agent']),
        createdAt: _string(json['created_at']),
        lastSeenAt: _string(json['last_seen_at']),
        current: _bool(json['current']),
      );
}

/// The full `/api/v1/me/settings` payload.
class AccountSettings {
  const AccountSettings({
    this.profile = const SettingsProfile(),
    this.prefs = const SettingsPrefs(),
    this.sessions = const [],
    this.tokenCount = 0,
    this.blockedCount = 0,
  });

  final SettingsProfile profile;
  final SettingsPrefs prefs;
  final List<SettingsSession> sessions;
  final int tokenCount;
  final int blockedCount;

  factory AccountSettings.fromJson(Map<String, dynamic> json) =>
      AccountSettings(
        profile: SettingsProfile.fromJson(_map(json['profile'])),
        prefs: SettingsPrefs.fromJson(_map(json['prefs'])),
        sessions: _list(json['sessions'])
            .whereType<Map>()
            .map((row) => SettingsSession.fromJson(row.cast<String, dynamic>()))
            .toList(),
        tokenCount: _int(json['token_count']) ?? 0,
        blockedCount: _int(json['blocked_count']) ?? 0,
      );
}

/// One account the viewer has blocked.
class BlockedUser {
  const BlockedUser({
    this.id = '',
    this.handle = '',
    this.displayName = '',
    this.blockedAt = '',
  });

  final String id;
  final String handle;
  final String displayName;
  final String blockedAt;

  factory BlockedUser.fromJson(Map<String, dynamic> json) {
    final handle = _string(json['handle']);
    return BlockedUser(
      id: _string(json['id']),
      handle: handle,
      displayName: _firstString([json['display_name'], handle]),
      blockedAt: _string(json['blocked_at']),
    );
  }
}

/// Public capabilities from `/api/v1/auth/config`.
class AuthConfig {
  const AuthConfig({
    this.registrationOpen = false,
    this.passwordFallback = false,
    this.oidcEnabled = false,
    this.emailVerificationRequired = false,
    this.demoEnabled = false,
    this.demoHandle = '',
    this.demoPassword = '',
  });

  final bool registrationOpen;
  final bool passwordFallback;
  final bool oidcEnabled;
  final bool emailVerificationRequired;
  final bool demoEnabled;
  final String demoHandle;
  final String demoPassword;

  factory AuthConfig.fromJson(Map<String, dynamic> json) {
    final demo = _map(json['demo']);
    return AuthConfig(
      registrationOpen: _bool(json['registration_open']),
      passwordFallback: _bool(json['password_fallback']),
      oidcEnabled: _bool(json['oidc_enabled']),
      emailVerificationRequired: _bool(json['email_verification_required']),
      demoEnabled: _bool(demo['enabled']),
      demoHandle: _string(demo['handle']),
      demoPassword: _string(demo['password']),
    );
  }
}

/// One opted-in public profile from the logged-out people directory.
class PublicPerson {
  const PublicPerson({
    required this.handle,
    this.displayName = '',
    this.bio = '',
    this.pours = 0,
    this.styles = 0,
    this.lastPour = '',
    this.avatarUrl = '',
  });

  final String handle;
  final String displayName;
  final String bio;
  final int pours;
  final int styles;
  final String lastPour;
  final String avatarUrl;

  factory PublicPerson.fromJson(Map<String, dynamic> json) => PublicPerson(
    handle: _string(json['handle']),
    displayName: _string(json['display_name']),
    bio: _string(json['bio']),
    pours: _int(json['pours']) ?? 0,
    styles: _int(json['styles']) ?? 0,
    lastPour: _string(json['last_pour']),
    avatarUrl: _string(json['avatar_url']),
  );
}

/// Public profile header for the read-only preview.
class PublicProfile {
  const PublicProfile({
    required this.handle,
    this.displayName = '',
    this.bio = '',
    this.joinedAt = '',
    this.pours = 0,
    this.styles = 0,
    this.venues = 0,
    this.badges = 0,
    this.uniqueDrinks = 0,
    this.toasts = 0,
    this.avatarUrl,
  });

  final String handle;
  final String displayName;
  final String bio;
  final String joinedAt;
  final int pours;
  final int styles;
  final int venues;
  final int badges;
  final int uniqueDrinks;
  final int toasts;

  /// Absolute URL for the member's avatar, or null when none is set.
  final String? avatarUrl;

  factory PublicProfile.fromJson(Map<String, dynamic> json) {
    final stats = _map(json['stats']);
    final avatar = _string(json['avatar_url']);
    return PublicProfile(
      handle: _string(json['handle']),
      displayName: _string(json['display_name']),
      bio: _string(json['bio']),
      joinedAt: _string(json['joined_at']),
      pours: _int(stats['pours']) ?? 0,
      styles: _int(stats['styles']) ?? 0,
      venues: _int(stats['venues']) ?? 0,
      badges: _int(stats['badges']) ?? 0,
      uniqueDrinks: _int(stats['unique_drinks']) ?? 0,
      toasts: _int(stats['toasts']) ?? 0,
      avatarUrl: avatar.isEmpty ? null : avatar,
    );
  }
}

/// The outcome of `POST /api/v1/auth/register`.
class RegistrationResult {
  const RegistrationResult({this.status = ''});

  /// `created` or `verification_sent`.
  final String status;

  /// True when the account exists but still needs the emailed link.
  bool get verificationRequired => status == 'verification_sent';

  factory RegistrationResult.fromJson(Map<String, dynamic> json) =>
      RegistrationResult(status: _string(json['status']));
}

/// The provisional drink created when a draft is committed.
class AiCommit {
  const AiCommit({
    this.drinkId = 0,
    this.slug = '',
    this.name = '',
    this.status = '',
    this.sessionId = '',
    this.message = '',
  });

  final int drinkId;
  final String slug;
  final String name;
  final String status;
  final String sessionId;
  final String message;

  factory AiCommit.fromJson(Map<String, dynamic> json) => AiCommit(
    drinkId: _int(_firstValue([json['drink_id'], json['id']])) ?? 0,
    slug: _string(json['slug']),
    name: _string(json['name']),
    status: _string(json['status']),
    sessionId: _string(json['session_id']),
    message: _string(json['message']),
  );
}

/// The outcome of `POST /api/v1/drinks`.
///
/// The server deduplicates: it answers 201 when it created the row and 200
/// when an equivalent soda already existed. [created] carries that distinction
/// so callers can word the confirmation without re-checking the status code.
class DrinkCreation {
  const DrinkCreation({
    this.id = 0,
    this.slug = '',
    this.name = '',
    this.status = '',
    this.message = '',
    this.created = false,
  });

  final int id;
  final String slug;
  final String name;
  final String status;
  final String message;

  /// True only for a fresh row (HTTP 201).
  final bool created;

  factory DrinkCreation.fromJson(
    Map<String, dynamic> json, {
    required bool created,
  }) => DrinkCreation(
    id: _int(json['id']) ?? 0,
    slug: _string(json['slug']),
    name: _string(json['name']),
    status: _string(json['status']),
    message: _string(json['message']),
    created: created,
  );
}

/// One row of the admin activity feed returned by
/// `GET /api/v1/admin/overview`.
///
/// [kind] is one of `checkin`, `drink`, `user`, `venue`, `report`, `comment`
/// or `toast`. Unknown kinds are kept as they arrive so a newer server never
/// breaks the app; the screen falls back to a neutral label.
class AdminOverviewEvent {
  const AdminOverviewEvent({
    this.id = '',
    this.kind = '',
    this.createdAt = '',
    this.actorId = '',
    this.actorHandle = '',
    this.actorName = '',
    this.title = '',
    this.detail = '',
    this.status = '',
    this.visibility = '',
    this.subjectId = '',
    this.href = '',
  });

  final String id;
  final String kind;

  /// RFC 3339 UTC timestamp.
  final String createdAt;
  final String actorId;

  /// Empty for system events, which the screen words as "system".
  final String actorHandle;
  final String actorName;
  final String title;
  final String detail;
  final String status;
  final String visibility;
  final String subjectId;

  /// Server-relative web path such as `/checkins/<id>`, empty when the event
  /// has no page to open.
  final String href;

  factory AdminOverviewEvent.fromJson(Map<String, dynamic> json) =>
      AdminOverviewEvent(
        id: _string(json['id']),
        kind: _string(json['kind']).toLowerCase(),
        createdAt: _string(json['created_at']),
        actorId: _string(json['actor_id']),
        actorHandle: _string(json['actor_handle']),
        actorName: _string(json['actor_name']),
        title: _string(json['title']),
        detail: _string(json['detail']),
        status: _string(json['status']),
        visibility: _string(json['visibility']),
        subjectId: _string(json['subject_id']),
        href: _string(json['href']),
      );
}

/// One page of the admin activity feed. [nextBefore] is empty on the last
/// page, which is how the screen knows to stop asking for more.
class AdminOverviewPage {
  const AdminOverviewPage({this.items = const [], this.nextBefore = ''});

  final List<AdminOverviewEvent> items;
  final String nextBefore;

  bool get hasMore => nextBefore.isNotEmpty;

  /// Accepts the wrapped `{"data": [...], "meta": {"next_before": ...}}` body.
  factory AdminOverviewPage.fromJson(Map<String, dynamic> json) {
    final data = json['data'];
    final rows = data is List ? data : const <dynamic>[];
    return AdminOverviewPage(
      items: rows
          .whereType<Map>()
          .map(
            (row) => AdminOverviewEvent.fromJson(row.cast<String, dynamic>()),
          )
          .toList(),
      nextBefore: _string(_map(json['meta'])['next_before']),
    );
  }
}

/// The five counters shown above the admin activity feed.
class AdminOverviewCounts {
  const AdminOverviewCounts({
    this.checkins24h = 0,
    this.drinks24h = 0,
    this.users7d = 0,
    this.openReports = 0,
    this.pendingDrinks = 0,
  });

  final int checkins24h;
  final int drinks24h;
  final int users7d;
  final int openReports;
  final int pendingDrinks;

  factory AdminOverviewCounts.fromJson(Map<String, dynamic> json) =>
      AdminOverviewCounts(
        checkins24h: _int(json['checkins_24h']) ?? 0,
        drinks24h: _int(json['drinks_24h']) ?? 0,
        users7d: _int(json['users_7d']) ?? 0,
        openReports: _int(json['open_reports']) ?? 0,
        pendingDrinks: _int(json['pending_drinks']) ?? 0,
      );
}
