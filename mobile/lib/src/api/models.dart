int? _int(Object? value) =>
    value is num ? value.toInt() : int.tryParse('${value ?? ''}');

double? _double(Object? value) =>
    value is num ? value.toDouble() : double.tryParse('${value ?? ''}');

String _string(Object? value) => value == null ? '' : '$value';

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

class Me {
  const Me({
    required this.id,
    required this.handle,
    required this.displayName,
    required this.email,
  });

  final String id;
  final String handle;
  final String displayName;
  final String email;

  factory Me.fromJson(Map<String, dynamic> json) => Me(
    id: '${json['id']}',
    handle: '${json['handle']}',
    displayName: '${json['display_name'] ?? json['handle']}',
    email: '${json['email'] ?? ''}',
  );
}

class Drink {
  const Drink({
    required this.id,
    required this.slug,
    required this.name,
    this.brand = '',
    this.style = '',
    this.volumeMl = 0,
    this.container = '',
    this.rating,
  });

  final int id;
  final String slug;
  final String name;
  final String brand;
  final String style;
  final int volumeMl;
  final String container;
  final double? rating;

  String get subtitle {
    final bits = [
      brand,
      style,
      if (volumeMl > 0) '$volumeMl ml',
    ].where((v) => v.isNotEmpty);
    return bits.join(' · ');
  }

  factory Drink.fromJson(Map<String, dynamic> json) => Drink(
    id: _int(json['id']) ?? 0,
    slug: '${json['slug'] ?? ''}',
    name: '${json['name'] ?? 'Unknown soda'}',
    brand: '${json['brand'] ?? ''}',
    style: '${json['style'] ?? ''}',
    volumeMl: _int(json['volume_ml']) ?? 0,
    container: '${json['container'] ?? ''}',
    rating: _double(json['avg_rating']),
  );

  /// The barcode endpoint serialises `catalog.DrinkDetail`, whose nested Ent
  /// entity uses Go field names.
  factory Drink.fromDetail(Map<String, dynamic> json) {
    final drink = json['Drink'];
    if (drink is! Map) return Drink.fromJson(json);
    final row = drink.cast<String, dynamic>();
    return Drink(
      id: _int(row['id']) ?? 0,
      slug: '${row['slug'] ?? ''}',
      name: '${row['name'] ?? 'Unknown soda'}',
      brand: '${json['BrandName'] ?? ''}',
      style: '${json['StyleName'] ?? ''}',
      volumeMl: _int(row['volume_ml']) ?? 0,
      container: '${row['container'] ?? ''}',
    );
  }
}

class FeedItem {
  const FeedItem({
    required this.id,
    required this.handle,
    required this.displayName,
    required this.drinkName,
    required this.drinkSlug,
    this.rating,
    this.note = '',
    this.venueName = '',
    this.toasts = 0,
    this.toasted = false,
  });

  final String id;
  final String handle;
  final String displayName;
  final String drinkName;
  final String drinkSlug;
  final double? rating;
  final String note;
  final String venueName;
  final int toasts;
  final bool toasted;

  factory FeedItem.fromJson(Map<String, dynamic> json) => FeedItem(
    id: '${json['id']}',
    handle: '${json['handle']}',
    displayName: '${json['display_name'] ?? json['handle']}',
    drinkName: '${json['drink_name'] ?? 'Soda'}',
    drinkSlug: '${json['drink_slug'] ?? ''}',
    rating: _double(json['rating']),
    note: '${json['note'] ?? ''}',
    venueName: '${json['venue_name'] ?? ''}',
    toasts: _int(json['toasts']) ?? 0,
    toasted: json['toasted'] == true,
  );
}

class BarcodeResult {
  const BarcodeResult({
    required this.status,
    this.drink,
    this.suggestedName = '',
  });

  final String status;
  final Drink? drink;
  final String suggestedName;

  bool get matched => status == 'matched' && drink != null;

  factory BarcodeResult.fromJson(Map<String, dynamic> json) => BarcodeResult(
    status: '${json['status'] ?? 'unknown'}',
    drink: json['drink'] is Map<String, dynamic>
        ? Drink.fromJson(json['drink'] as Map<String, dynamic>)
        : null,
    suggestedName: '${json['suggested_name'] ?? ''}',
  );
}

class Venue {
  const Venue({
    required this.id,
    required this.name,
    required this.kind,
    this.slug = '',
    this.city = '',
    this.address = '',
    this.region = '',
    this.country = '',
    this.website = '',
    this.lat = 0,
    this.lng = 0,
  });

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
  );
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
    this.visibility = '',
    this.toasts = 0,
    this.toasted = false,
    this.createdAt = '',
    this.venueId,
    this.venueName = '',
    this.photoId = '',
    this.photoMime = '',
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
  final String visibility;
  final int toasts;
  final bool toasted;
  final String createdAt;
  final int? venueId;
  final String venueName;
  final String photoId;
  final String photoMime;
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
      visibility: _string(json['visibility']),
      toasts: _int(_firstValue([json['toasts'], json['toast_count']])) ?? 0,
      toasted: _bool(json['toasted']),
      createdAt: _firstString([json['created_at'], json['createdAt']]),
      venueId: _int(_firstValue([venue['id'], json['venue_id']])),
      venueName: _firstString([venue['name'], json['venue_name']]),
      photoId: _firstString([photo['id'], json['photo_id']]),
      photoMime: _firstString([photo['mime'], json['photo_mime']]),
      canDelete: _bool(json['can_delete']),
      canToast: _bool(json['can_toast']),
      canComment: _bool(json['can_comment']),
      commentsCount:
          _int(_firstValue([json['comments_count'], json['comment_count']])) ??
          0,
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

  factory UserProfile.fromJson(Map<String, dynamic> json) {
    final statsJson = _map(_firstValue([json['stats'], json['counts']]));
    final handle = _string(json['handle']);
    final displayName = _firstString([
      json['display_name'],
      json['displayName'],
      handle,
    ]);
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

/// The viewer's lifetime passport: stats plus earned badges.
class Passport {
  const Passport({this.stats = const ProfileStats(), this.badges = const []});

  final ProfileStats stats;
  final List<ProfileBadge> badges;

  factory Passport.fromJson(Map<String, dynamic> json) => Passport(
    stats: ProfileStats.fromJson(_map(json['stats'])),
    badges: _list(_firstValue([json['badges'], json['earned_badges']]))
        .whereType<Map>()
        .map((row) => ProfileBadge.fromJson(row.cast<String, dynamic>()))
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
