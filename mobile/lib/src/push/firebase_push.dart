import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';

import 'push_service.dart';

/// Background isolate entry point. Notification messages are rendered by the
/// system tray; data payloads still need a registered handler or they are
/// dropped, so this intentionally does nothing else.
@pragma('vm:entry-point')
Future<void> sodapopBackgroundPushHandler(RemoteMessage message) async {}

/// Initialises Firebase when all FCM defines are present. Missing config or a
/// failed initialisation leaves push disabled without affecting the app.
///
/// This Dart-only path keeps delivery working while the process is alive. A
/// push that cold-starts a force-stopped app reaches the native FCM service
/// before this runs, so guaranteed cold-start delivery needs a build-time
/// `google-services.json` (see docs/mobile.md).
Future<PushService> initialisePush() async {
  final service = PushService.instance;
  final config = FirebasePushConfig.fromDefines();
  if (config == null) return service;
  try {
    await Firebase.initializeApp(options: _options(config));
    FirebaseMessaging.onBackgroundMessage(sodapopBackgroundPushHandler);
    service.attach(
      PushRegistrar(_FirebasePushMessaging(FirebaseMessaging.instance)),
    );
  } catch (error) {
    debugPrint('Sodapop push disabled: $error');
  }
  return service;
}

FirebaseOptions _options(FirebasePushConfig config) => FirebaseOptions(
  apiKey: config.apiKey,
  appId: config.appId,
  messagingSenderId: config.messagingSenderId,
  projectId: config.projectId,
  storageBucket: config.hasStorageBucket ? config.storageBucket : null,
);

class _FirebasePushMessaging implements PushMessaging {
  _FirebasePushMessaging(this._messaging);

  final FirebaseMessaging _messaging;

  @override
  Future<bool> requestPermission() async {
    final settings = await _messaging.requestPermission();
    return _granted(settings.authorizationStatus);
  }

  @override
  Future<bool> hasPermission() async {
    final settings = await _messaging.getNotificationSettings();
    return _granted(settings.authorizationStatus);
  }

  static bool _granted(AuthorizationStatus status) =>
      status == AuthorizationStatus.authorized ||
      status == AuthorizationStatus.provisional;

  @override
  Future<String?> getToken() =>
      // firebase_messaging 17 deprecates this in favour of register(), but the
      // backend registers FCM tokens, so keep the token API and its refresh
      // stream.
      // ignore: deprecated_member_use
      _messaging.getToken();

  @override
  Stream<String> get onTokenRefresh =>
      // ignore: deprecated_member_use
      _messaging.onTokenRefresh;

  @override
  Stream<void> get onMessage => FirebaseMessaging.onMessage.map((_) {});

  @override
  Future<void> invalidateToken() => _messaging.unregister();
}
