import 'dart:math';
import 'dart:typed_data';

import 'package:dio/dio.dart';

import '../utils/url.dart';
import 'models.dart';

class ApiException implements Exception {
  ApiException(this.message, {this.statusCode});

  final String message;
  final int? statusCode;

  @override
  String toString() => message;
}

class ApiClient {
  /// [dio] exists so tests can supply an adapter; production callers use the
  /// default client configured by [configure].
  ApiClient({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 25),
              headers: {
                'Accept': 'application/json',
                'User-Agent': 'sodapop-android',
              },
            ),
          );

  final Dio _dio;
  String? _server;
  String? _token;

  bool get configured => _server != null;
  String? get server => _server;

  /// Absolute URL for a stored media row.
  String mediaUrl(String id) => '${_server ?? ''}/api/v1/media/$id';

  /// Bearer headers for an authenticated image fetch; empty when signed out.
  Map<String, String> get imageHeaders {
    final token = _token;
    return token == null ? const {} : {'Authorization': 'Bearer $token'};
  }

  void configure(String serverUrl, {String? token}) {
    _server = serverUrl;
    _token = token;
    _dio.options.baseUrl = '$serverUrl/';
  }

  void setToken(String? token) => _token = token;

  Options _options({bool auth = true, String? idempotencyKey}) {
    final headers = <String, String>{'traceparent': _traceparent()};
    if (auth && _token != null) headers['Authorization'] = 'Bearer $_token';
    if (idempotencyKey != null) headers['Idempotency-Key'] = idempotencyKey;
    return Options(headers: headers);
  }

  String _traceparent() {
    final random = Random.secure();
    String hex(int bytes) => List.generate(
      bytes,
      (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'),
    ).join();
    return '00-${hex(16)}-${hex(8)}-01';
  }

  dynamic _data(Response<dynamic> response) {
    final body = response.data;
    if (body is Map<String, dynamic>) return body['data'];
    throw ApiException(
      'Unexpected response from the server',
      statusCode: response.statusCode,
    );
  }

  /// Normalises a collection response. Most endpoints return a bare list under
  /// `data`; a few wrap it in `items` or `results`.
  List<dynamic> _rows(dynamic data) {
    if (data is List) return data;
    if (data is Map) {
      final items = data['items'] ?? data['results'];
      if (items is List) return items;
    }
    return const [];
  }

  Future<T> _guard<T>(Future<T> Function() request) async {
    try {
      return await request();
    } catch (error) {
      throw ApiClient.describe(error);
    }
  }

  Future<AuthStatus> oidcStatus() async {
    final response = await _dio.get<dynamic>(
      'api/v1/auth/oidc/status',
      options: _options(auth: false),
    );
    return AuthStatus.fromJson(
      (_data(response) as Map).cast<String, dynamic>(),
    );
  }

  Future<Me> me({String? tokenOverride}) async {
    final old = _token;
    if (tokenOverride != null) _token = tokenOverride;
    try {
      final response = await _dio.get<dynamic>(
        'api/v1/me',
        options: _options(),
      );
      return Me.fromJson((_data(response) as Map).cast<String, dynamic>());
    } finally {
      _token = old;
    }
  }

  Future<String> passwordToken(String login, String password) async {
    final response = await _dio.post<dynamic>(
      'api/v1/auth/token',
      data: {'login': login, 'password': password, 'name': 'Android app'},
      options: _options(auth: false),
    );
    final data = (_data(response) as Map).cast<String, dynamic>();
    return '${data['token']}';
  }

  /// Exchanges the OIDC browser session cookie for a personal API token.
  Future<String> exchangeBrowserSession({
    required String cookie,
    required String csrf,
    required String serverUrl,
  }) async {
    final response = await Dio().post<dynamic>(
      '$serverUrl/api/v1/auth/mobile-token',
      options: Options(
        headers: {
          'Cookie': cookie,
          'X-CSRF-Token': csrf,
          'Accept': 'application/json',
        },
      ),
    );
    final body = response.data;
    final data = body is Map<String, dynamic> ? body['data'] : null;
    if (data is! Map || data['token'] == null) {
      throw ApiException('Could not create a device token');
    }
    return '${data['token']}';
  }

  Future<List<FeedItem>> feed({int limit = 30}) async {
    final response = await _dio.get<dynamic>(
      'api/v1/feed',
      queryParameters: {'limit': limit},
      options: _options(),
    );
    final rows = _data(response);
    return (rows as List)
        .map((e) => FeedItem.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<List<Drink>> search(String query, {String? style}) async {
    final response = await _dio.get<dynamic>(
      'api/v1/search',
      queryParameters: {'q': query, 'style': ?style},
      options: _options(),
    );
    final rows = _data(response);
    return (rows as List)
        .map((e) => Drink.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  Future<BarcodeResult> barcode(String gtin) async {
    final response = await _dio.get<dynamic>(
      'api/v1/barcodes/$gtin',
      options: _options(),
    );
    final data = (_data(response) as Map).cast<String, dynamic>();
    Drink? drink;
    if (data['drink'] is Map) {
      drink = Drink.fromDetail((data['drink'] as Map).cast<String, dynamic>());
    }
    var suggestedName = '';
    if (data['suggested'] is Map) {
      final suggested = (data['suggested'] as Map).cast<String, dynamic>();
      suggestedName = '${suggested['name'] ?? ''}';
    }
    return BarcodeResult(
      status: '${data['status'] ?? 'unknown'}',
      drink: drink,
      suggestedName: suggestedName,
    );
  }

  /// Creates a check-in. A [photo] switches the request to `multipart/form-data`
  /// with the bytes in the `image` field; without one the JSON body is unchanged.
  Future<void> createCheckin({
    required int drinkId,
    int? rating,
    String note = '',
    int? venueId,
    String? serving,
    String? visibility,
    String? idempotencyKey,
    Uint8List? photo,
    String? photoFilename,
    String? photoMime,
  }) async {
    final key = idempotencyKey ?? newIdempotencyKey();
    final ratingValue = rating == null ? null : rating / 4;
    final noteText = note.trim();
    final servingText = _trimmed(serving);
    final visibilityText = _trimmed(visibility);

    if (photo == null) {
      final payload = <String, dynamic>{
        'drink_id': drinkId,
        'venue_id': ?venueId,
        'serving': ?servingText,
        'visibility': ?visibilityText,
      };
      if (ratingValue != null) payload['rating'] = ratingValue;
      if (noteText.isNotEmpty) payload['note'] = noteText;
      await _dio.post<dynamic>(
        'api/v1/checkins',
        data: payload,
        options: _options(idempotencyKey: key),
      );
      return;
    }

    // Dio owns the multipart boundary and Content-Type; setting it here would
    // strip the boundary.
    final form = FormData();
    form.fields.add(MapEntry('drink_id', '$drinkId'));
    if (venueId != null) form.fields.add(MapEntry('venue_id', '$venueId'));
    if (ratingValue != null) {
      form.fields.add(MapEntry('rating', '$ratingValue'));
    }
    if (noteText.isNotEmpty) form.fields.add(MapEntry('note', noteText));
    if (servingText != null) form.fields.add(MapEntry('serving', servingText));
    if (visibilityText != null) {
      form.fields.add(MapEntry('visibility', visibilityText));
    }
    form.fields.add(MapEntry('idempotency_key', key));
    form.files.add(
      MapEntry(
        'image',
        MultipartFile.fromBytes(
          photo,
          filename: _trimmed(photoFilename) ?? 'photo.jpg',
          contentType: _mediaType(photoMime),
        ),
      ),
    );
    await _dio.post<dynamic>(
      'api/v1/checkins',
      data: form,
      options: _options(idempotencyKey: key),
    );
  }

  static String? _trimmed(String? value) {
    final text = value?.trim();
    return text == null || text.isEmpty ? null : text;
  }

  /// Avoids importing `http_parser` directly; Dio exports this alias.
  static DioMediaType? _mediaType(String? value) {
    final text = _trimmed(value)?.split(';').first.trim();
    if (text == null) return null;
    final slash = text.indexOf('/');
    if (slash <= 0 || slash == text.length - 1) return null;
    return DioMediaType(text.substring(0, slash), text.substring(slash + 1));
  }

  Future<void> toast(String checkinId) async {
    await _dio.post<dynamic>(
      'api/v1/checkins/$checkinId/toast',
      options: _options(),
    );
  }

  Future<void> untoast(String checkinId) => _guard(() async {
    await _dio.delete<dynamic>(
      'api/v1/checkins/$checkinId/toast',
      options: _options(),
    );
  });

  Future<CheckinDetail> getCheckin(String id) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/checkins/$id',
      options: _options(),
    );
    return CheckinDetail.fromJson(
      (_data(response) as Map).cast<String, dynamic>(),
    );
  });

  Future<List<Comment>> listComments(String checkinId) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/checkins/$checkinId/comments',
      options: _options(),
    );
    return _rows(_data(response))
        .whereType<Map>()
        .map((row) => Comment.fromJson(row.cast<String, dynamic>()))
        .toList();
  });

  Future<Comment> createComment(String checkinId, String body) => _guard(
    () async {
      final response = await _dio.post<dynamic>(
        'api/v1/checkins/$checkinId/comments',
        data: {'body': body},
        options: _options(),
      );
      return Comment.fromJson((_data(response) as Map).cast<String, dynamic>());
    },
  );

  Future<void> deleteComment(String commentId) => _guard(() async {
    await _dio.delete<dynamic>(
      'api/v1/comments/$commentId',
      options: _options(),
    );
  });

  Future<void> deleteCheckin(String id) => _guard(() async {
    await _dio.delete<dynamic>('api/v1/checkins/$id', options: _options());
  });

  Future<UserProfile> userProfile(String handle) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/users/$handle',
      options: _options(),
    );
    return UserProfile.fromJson(
      (_data(response) as Map).cast<String, dynamic>(),
    );
  });

  Future<void> follow(String handle) => _guard(() async {
    await _dio.post<dynamic>(
      'api/v1/users/$handle/follow',
      options: _options(),
    );
  });

  Future<void> unfollow(String handle) => _guard(() async {
    await _dio.delete<dynamic>(
      'api/v1/users/$handle/follow',
      options: _options(),
    );
  });

  Future<void> acceptFriendRequest(String id) => _guard(() async {
    await _dio.post<dynamic>(
      'api/v1/friends/requests/$id/accept',
      options: _options(),
    );
  });

  Future<void> removeFriend(String id) => _guard(() async {
    await _dio.delete<dynamic>('api/v1/friends/$id', options: _options());
  });

  Future<void> blockUser(String handle) => _guard(() async {
    await _dio.post<dynamic>('api/v1/users/$handle/block', options: _options());
  });

  Future<void> unblockUser(String handle) => _guard(() async {
    await _dio.delete<dynamic>(
      'api/v1/users/$handle/block',
      options: _options(),
    );
  });

  Future<List<NotificationItem>> notifications({int limit = 30}) => _guard(
    () async {
      final response = await _dio.get<dynamic>(
        'api/v1/notifications',
        queryParameters: {'limit': limit},
        options: _options(),
      );
      return _rows(_data(response))
          .whereType<Map>()
          .map((row) => NotificationItem.fromJson(row.cast<String, dynamic>()))
          .toList();
    },
  );

  Future<void> markNotificationsRead() => _guard(() async {
    await _dio.post<dynamic>('api/v1/notifications/read', options: _options());
  });

  Future<List<SavedVenue>> savedVenues() => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/venues/saved',
      options: _options(),
    );
    return _rows(_data(response))
        .whereType<Map>()
        .map((row) => SavedVenue.fromJson(row.cast<String, dynamic>()))
        .toList();
  });

  Future<List<ListSummary>> myLists() => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/me/lists',
      options: _options(),
    );
    return _rows(_data(response))
        .whereType<Map>()
        .map((row) => ListSummary.fromJson(row.cast<String, dynamic>()))
        .toList();
  });

  Future<ListDetail> getList(String id) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/lists/$id',
      options: _options(),
    );
    return ListDetail.fromJson(
      (_data(response) as Map).cast<String, dynamic>(),
    );
  });

  Future<Insights> insights({int days = 30}) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/stats/me/insights',
      queryParameters: {'range': days},
      options: _options(),
    );
    return Insights.fromJson((_data(response) as Map).cast<String, dynamic>());
  });

  Future<List<Twin>> tasteTwins({int limit = 10}) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/taste/twins',
      queryParameters: {'limit': limit},
      options: _options(),
    );
    return _rows(_data(response))
        .whereType<Map>()
        .map((row) => Twin.fromJson(row.cast<String, dynamic>()))
        .toList();
  });

  Future<List<Recommendation>> tasteRecommendations({int limit = 10}) =>
      _guard(() async {
        final response = await _dio.get<dynamic>(
          'api/v1/taste/recommendations',
          queryParameters: {'limit': limit},
          options: _options(),
        );
        return _rows(_data(response))
            .whereType<Map>()
            .map((row) => Recommendation.fromJson(row.cast<String, dynamic>()))
            .toList();
      });

  Future<List<BadgeStatus>> badges() => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/badges',
      options: _options(),
    );
    return _rows(_data(response))
        .whereType<Map>()
        .map((row) => BadgeStatus.fromJson(row.cast<String, dynamic>()))
        .toList();
  });

  Future<Passport> passport() => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/passport',
      options: _options(),
    );
    return Passport.fromJson((_data(response) as Map).cast<String, dynamic>());
  });

  Future<List<MixRecipe>> myMixes() => _guard(() async {
    final response = await _dio.get<dynamic>('api/v1/mix', options: _options());
    return _rows(_data(response))
        .whereType<Map>()
        .map((row) => MixRecipe.fromJson(row.cast<String, dynamic>()))
        .toList();
  });

  Future<MixRecipe> getMix(String idOrSlug) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/mix/${Uri.encodeComponent(idOrSlug)}',
      options: _options(),
    );
    return MixRecipe.fromJson((_data(response) as Map).cast<String, dynamic>());
  });

  Future<MixRecipe> createMix({
    required String name,
    String notes = '',
    String visibility = 'public',
    required List<MixComponent> components,
  }) => _guard(() async {
    final response = await _dio.post<dynamic>(
      'api/v1/mix',
      data: {
        'name': name,
        'notes': notes,
        'visibility': visibility,
        'components': components
            .map((component) => component.toJson())
            .toList(),
      },
      options: _options(),
    );
    return MixRecipe.fromJson((_data(response) as Map).cast<String, dynamic>());
  });

  Future<VenueDetail> venue(int id) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/venues/$id',
      options: _options(),
    );
    return VenueDetail.fromJson(
      (_data(response) as Map).cast<String, dynamic>(),
    );
  });

  Future<List<VenueMedia>> venueMedia(int id) => _guard(() async {
    final response = await _dio.get<dynamic>(
      'api/v1/venues/$id/media',
      options: _options(),
    );
    return _rows(_data(response))
        .whereType<Map>()
        .map((row) => VenueMedia.fromJson(row.cast<String, dynamic>()))
        .toList();
  });

  Future<void> saveVenue(int id) => _guard(() async {
    await _dio.post<dynamic>('api/v1/venues/$id/save', options: _options());
  });

  Future<void> unsaveVenue(int id) => _guard(() async {
    await _dio.delete<dynamic>('api/v1/venues/$id/save', options: _options());
  });

  Future<List<Venue>> venues(String query) async {
    final response = await _dio.get<dynamic>(
      'api/v1/venues/search',
      queryParameters: {'q': query},
      options: _options(),
    );
    final rows = _data(response);
    return (rows as List)
        .map((e) => Venue.fromJson((e as Map).cast<String, dynamic>()))
        .toList();
  }

  static ApiException describe(Object error) {
    if (error is ApiException) return error;
    if (error is DioException) {
      final body = error.response?.data;
      if (body is Map && body['error'] is Map) {
        return ApiException(
          '${body['error']['message']}',
          statusCode: error.response?.statusCode,
        );
      }
      if (error.type == DioExceptionType.connectionError ||
          error.type == DioExceptionType.connectionTimeout) {
        return ApiException(
          'Cannot reach the server. Check the address and try again.',
        );
      }
      return ApiException(
        'The server did not answer (${error.response?.statusCode ?? 'network'})',
      );
    }
    return ApiException('Something went wrong');
  }
}
