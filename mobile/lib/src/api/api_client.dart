import 'dart:math';

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
  ApiClient() {
    _dio = Dio(
      BaseOptions(
        connectTimeout: const Duration(seconds: 10),
        receiveTimeout: const Duration(seconds: 25),
        headers: {'Accept': 'application/json', 'User-Agent': 'sodapop-android'},
      ),
    );
  }

  late final Dio _dio;
  String? _server;
  String? _token;

  bool get configured => _server != null;
  String? get server => _server;

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
    String hex(int bytes) =>
        List.generate(bytes, (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0')).join();
    return '00-${hex(16)}-${hex(8)}-01';
  }

  dynamic _data(Response<dynamic> response) {
    final body = response.data;
    if (body is Map<String, dynamic>) return body['data'];
    throw ApiException('Unexpected response from the server', statusCode: response.statusCode);
  }

  Future<AuthStatus> oidcStatus() async {
    final response = await _dio.get<dynamic>('api/v1/auth/oidc/status', options: _options(auth: false));
    return AuthStatus.fromJson((_data(response) as Map).cast<String, dynamic>());
  }

  Future<Me> me({String? tokenOverride}) async {
    final old = _token;
    if (tokenOverride != null) _token = tokenOverride;
    try {
      final response = await _dio.get<dynamic>('api/v1/me', options: _options());
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
      options: Options(headers: {
        'Cookie': cookie,
        'X-CSRF-Token': csrf,
        'Accept': 'application/json',
      }),
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
    return (rows as List).map((e) => FeedItem.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<List<Drink>> search(String query, {String? style}) async {
    final response = await _dio.get<dynamic>(
      'api/v1/search',
      queryParameters: {'q': query, 'style': ?style},
      options: _options(),
    );
    final rows = _data(response);
    return (rows as List).map((e) => Drink.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  Future<BarcodeResult> barcode(String gtin) async {
    final response = await _dio.get<dynamic>('api/v1/barcodes/$gtin', options: _options());
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

  Future<void> createCheckin({
    required int drinkId,
    int? rating,
    String note = '',
    int? venueId,
    String? idempotencyKey,
  }) async {
    final payload = <String, dynamic>{
      'drink_id': drinkId,
      'venue_id': ?venueId,
    };
    if (rating != null) payload['rating'] = rating / 4;
    if (note.trim().isNotEmpty) payload['note'] = note.trim();
    await _dio.post<dynamic>(
      'api/v1/checkins',
      data: payload,
      options: _options(
        idempotencyKey: idempotencyKey ?? newIdempotencyKey(),
      ),
    );
  }

  Future<void> toast(String checkinId) async {
    await _dio.post<dynamic>('api/v1/checkins/$checkinId/toast', options: _options());
  }

  Future<List<Venue>> venues(String query) async {
    final response = await _dio.get<dynamic>(
      'api/v1/venues/search',
      queryParameters: {'q': query},
      options: _options(),
    );
    final rows = _data(response);
    return (rows as List).map((e) => Venue.fromJson((e as Map).cast<String, dynamic>())).toList();
  }

  static ApiException describe(Object error) {
    if (error is ApiException) return error;
    if (error is DioException) {
      final body = error.response?.data;
      if (body is Map && body['error'] is Map) {
        return ApiException('${body['error']['message']}', statusCode: error.response?.statusCode);
      }
      if (error.type == DioExceptionType.connectionError ||
          error.type == DioExceptionType.connectionTimeout) {
        return ApiException('Cannot reach the server. Check the address and try again.');
      }
      return ApiException('The server did not answer (${error.response?.statusCode ?? 'network'})');
    }
    return ApiException('Something went wrong');
  }
}
