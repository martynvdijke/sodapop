int? _int(Object? value) => value is num ? value.toInt() : int.tryParse('${value ?? ''}');

double? _double(Object? value) => value is num ? value.toDouble() : double.tryParse('${value ?? ''}');

class Me {
  const Me({required this.id, required this.handle, required this.displayName, required this.email});

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
    final bits = [brand, style, if (volumeMl > 0) '$volumeMl ml'].where((v) => v.isNotEmpty);
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
  const BarcodeResult({required this.status, this.drink, this.suggestedName = ''});

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
  const Venue({required this.id, required this.name, required this.kind, this.city = ''});

  final int id;
  final String name;
  final String kind;
  final String city;

  factory Venue.fromJson(Map<String, dynamic> json) => Venue(
        id: _int(json['id']) ?? 0,
        name: '${json['name'] ?? ''}',
        kind: '${json['kind'] ?? 'other'}',
        city: '${json['city'] ?? ''}',
      );
}

class AuthStatus {
  const AuthStatus({required this.oidcEnabled, required this.passwordFallback, this.issuer = ''});

  final bool oidcEnabled;
  final bool passwordFallback;
  final String issuer;

  factory AuthStatus.fromJson(Map<String, dynamic> json) => AuthStatus(
        oidcEnabled: json['enabled'] == true,
        passwordFallback: json['password_fallback'] != false,
        issuer: '${json['issuer'] ?? ''}',
      );
}
