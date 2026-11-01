import 'dart:async';

import 'package:flutter/foundation.dart';

import '../api/api_client.dart';

/// Firebase client configuration supplied at build time through
/// `--dart-define`. The values come from the Firebase console; no
/// `google-services.json` is needed.
class FirebasePushConfig {
  const FirebasePushConfig({
    required this.apiKey,
    required this.appId,
    required this.messagingSenderId,
    required this.projectId,
    this.storageBucket = '',
  });

  final String apiKey;
  final String appId;
  final String messagingSenderId;
  final String projectId;
  final String storageBucket;

  bool get hasStorageBucket => storageBucket.isNotEmpty;

  /// Returns `null` when the required defines are missing or blank, which is
  /// the signal to skip Firebase entirely. [storageBucket] is optional.
  static FirebasePushConfig? fromDefines({
    String apiKey = const String.fromEnvironment('FCM_API_KEY'),
    String appId = const String.fromEnvironment('FCM_APP_ID'),
    String messagingSenderId = const String.fromEnvironment(
      'FCM_MESSAGING_SENDER_ID',
    ),
    String projectId = const String.fromEnvironment('FCM_PROJECT_ID'),
    String storageBucket = const String.fromEnvironment('FCM_STORAGE_BUCKET'),
  }) {
    final key = apiKey.trim();
    final app = appId.trim();
    final sender = messagingSenderId.trim();
    final project = projectId.trim();
    if (key.isEmpty || app.isEmpty || sender.isEmpty || project.isEmpty) {
      return null;
    }
    return FirebasePushConfig(
      apiKey: key,
      appId: app,
      messagingSenderId: sender,
      projectId: project,
      storageBucket: storageBucket.trim(),
    );
  }
}

/// The slice of `FirebaseMessaging` the registration flow needs. Keeping it
/// behind this interface means widget and unit tests never touch the plugin.
abstract class PushMessaging {
  /// Shows the Android notification prompt. Returns whether notifications are
  /// now allowed. Called only from a deliberate opt-in, never on sign-in:
  /// Play expects the prompt to follow a user action.
  Future<bool> requestPermission();

  /// Whether notifications are already allowed, without prompting.
  Future<bool> hasPermission();

  Future<String?> getToken();
  Stream<String> get onTokenRefresh;
  Stream<void> get onMessage;

  /// Invalidates the device registration at Firebase so Google stops
  /// delivering to the old token even if the server DELETE was lost.
  Future<void> invalidateToken();
}

/// Registers this device's FCM token with the signed-in account and removes it
/// again on sign-out. One failure is logged and left for the next sign-in or
/// token refresh; nothing retries in a loop.
class PushRegistrar {
  PushRegistrar(this._messaging);

  final PushMessaging _messaging;
  final StreamController<void> _messages = StreamController<void>.broadcast();

  StreamSubscription<String>? _refresh;
  StreamSubscription<void>? _messageSub;
  ApiClient? _api;
  String? _lastToken;
  bool _active = false;

  /// Foreground message events, forwarded to the UI.
  Stream<void> get messages => _messages.stream;

  /// The token most recently sent to the server, used for sign-out cleanup.
  String? get lastToken => _lastToken;

  Future<void> signIn(ApiClient api) async {
    _api = api;
    _active = true;
    _listen();
    await _register(api);
  }

  void _listen() {
    _refresh ??= _messaging.onTokenRefresh.listen(_onTokenRefresh);
    _messageSub ??= _messaging.onMessage.listen((_) {
      if (!_messages.isClosed) _messages.add(null);
    });
  }

  void _onTokenRefresh(String token) {
    final api = _api;
    if (!_active || api == null) return;
    // Fire and forget: a failed refresh is retried on the next refresh or
    // sign-in, never in a tight loop.
    unawaited(_save(api, token));
  }

  /// Prompts for notification permission and reports the result. Exposed so
  /// the settings toggle can ask only when someone turns push on.
  Future<bool> requestPermission() async {
    try {
      return await _messaging.requestPermission();
    } catch (error) {
      _warn(error);
      return false;
    }
  }

  Future<void> _register(ApiClient api) async {
    try {
      // Sign-in must never raise a permission dialog, and a device that has not
      // allowed notifications should not be registered: the token would only
      // accumulate dead rows on the server.
      if (!await _messaging.hasPermission()) return;
      final token = (await _messaging.getToken())?.trim();
      if (token == null || token.isEmpty) return;
      await _save(api, token);
    } catch (error) {
      _warn(error);
    }
  }

  Future<void> _save(ApiClient api, String token) async {
    // Remember the token even when the request fails so sign-out can still
    // ask the server to forget it (a 404 is harmless).
    _lastToken = token;
    try {
      await api.registerPushDevice(token);
    } catch (error) {
      _warn(error);
    }
  }

  /// Best effort: the caller starts this before clearing credentials so the
  /// request still carries the Bearer token, and never waits for it. The
  /// Firebase registration is invalidated even when the server call fails, so
  /// the old account's pushes stop at Google.
  Future<void> signOut(ApiClient api) async {
    _active = false;
    final token = _lastToken;
    _lastToken = null;
    _api = null;
    if (token == null) return;
    try {
      await api.unregisterPushDevice(token);
    } catch (error) {
      _warn(error);
    }
    try {
      await _messaging.invalidateToken();
    } catch (error) {
      _warn(error);
    }
  }

  void _warn(Object error) {
    debugPrint('Sodapop push: ${ApiClient.describe(error).message}');
  }

  Future<void> dispose() async {
    _active = false;
    await _refresh?.cancel();
    await _messageSub?.cancel();
    await _messages.close();
  }
}

/// App-wide handle to native push. It stays a no-op until Firebase messaging
/// is initialised, so an app built without FCM defines behaves exactly like
/// before.
class PushService {
  PushService({PushRegistrar? registrar}) {
    if (registrar != null) attach(registrar);
  }

  static final PushService instance = PushService();

  final StreamController<void> _messages = StreamController<void>.broadcast();
  PushRegistrar? _registrar;
  StreamSubscription<void>? _forward;

  bool get enabled => _registrar != null;

  /// Foreground push events. The stream exists even when push is disabled.
  Stream<void> get onMessage => _messages.stream;

  void attach(PushRegistrar registrar) {
    _registrar = registrar;
    _forward ??= registrar.messages.listen((_) {
      if (!_messages.isClosed) _messages.add(null);
    });
  }

  Future<void> signIn(ApiClient api) async {
    await _registrar?.signIn(api);
  }

  /// Asks Android for notification permission. Returns false when push is not
  /// configured in this build, so callers can keep the switch off.
  Future<bool> requestPermission() async {
    final registrar = _registrar;
    if (registrar == null) return false;
    return registrar.requestPermission();
  }

  Future<void> signOut(ApiClient api) async {
    await _registrar?.signOut(api);
  }
}
