import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/push/push_service.dart';

class _FakeMessaging implements PushMessaging {
  _FakeMessaging();

  String? token = 'token-1';
  bool permissionRequested = false;
  int permissionCalls = 0;
  int invalidations = 0;

  final StreamController<String> _refresh = StreamController<String>.broadcast();
  final StreamController<void> _messages = StreamController<void>.broadcast();

  @override
  Future<void> requestPermission() async {
    permissionRequested = true;
    permissionCalls++;
  }

  @override
  Future<String?> getToken() async => token;

  @override
  Stream<String> get onTokenRefresh => _refresh.stream;

  @override
  Stream<void> get onMessage => _messages.stream;

  @override
  Future<void> invalidateToken() async {
    invalidations++;
  }

  void refresh(String next) => _refresh.add(next);
  void message() => _messages.add(null);

  Future<void> close() async {
    await _refresh.close();
    await _messages.close();
  }
}

class _RecordingApi extends ApiClient {
  final List<String> registered = [];
  final List<String> unregistered = [];
  bool failRegister = false;
  bool failUnregister = false;

  @override
  Future<void> registerPushDevice(
    String token, {
    String platform = 'android',
  }) async {
    if (failRegister) throw ApiException('Cannot reach the server.');
    registered.add('$platform:$token');
  }

  @override
  Future<void> unregisterPushDevice(String token) async {
    if (failUnregister) throw ApiException('Not found');
    unregistered.add(token);
  }
}

Future<void> _settle() => Future<void>.delayed(Duration.zero);

void main() {
  group('FirebasePushConfig.fromDefines', () {
    test('is null when every define is missing', () {
      expect(
        FirebasePushConfig.fromDefines(
          apiKey: '',
          appId: '',
          messagingSenderId: '',
          projectId: '',
        ),
        isNull,
      );
    });

    test('is null when a required define is blank', () {
      expect(
        FirebasePushConfig.fromDefines(
          apiKey: ' key ',
          appId: 'app',
          messagingSenderId: '  ',
          projectId: 'project',
        ),
        isNull,
      );
    });

    test('builds a trimmed config and treats the bucket as optional', () {
      final config = FirebasePushConfig.fromDefines(
        apiKey: ' key ',
        appId: ' app ',
        messagingSenderId: ' 123 ',
        projectId: ' project ',
        storageBucket: ' ',
      );

      expect(config, isNotNull);
      expect(config!.apiKey, 'key');
      expect(config.appId, 'app');
      expect(config.messagingSenderId, '123');
      expect(config.projectId, 'project');
      expect(config.hasStorageBucket, isFalse);
    });

    test('keeps an optional bucket when provided', () {
      final config = FirebasePushConfig.fromDefines(
        apiKey: 'key',
        appId: 'app',
        messagingSenderId: '123',
        projectId: 'project',
        storageBucket: 'project.appspot.com',
      );

      expect(config!.hasStorageBucket, isTrue);
      expect(config.storageBucket, 'project.appspot.com');
    });
  });

  group('PushRegistrar', () {
    late _FakeMessaging messaging;
    late _RecordingApi api;
    late PushRegistrar registrar;

    setUp(() {
      messaging = _FakeMessaging();
      api = _RecordingApi();
      registrar = PushRegistrar(messaging);
    });

    tearDown(() async {
      await registrar.dispose();
      await messaging.close();
    });

    test('requests permission and registers the device token', () async {
      await registrar.signIn(api);

      expect(messaging.permissionRequested, isTrue);
      expect(api.registered, ['android:token-1']);
      expect(registrar.lastToken, 'token-1');
    });

    test('does nothing when the token is empty', () async {
      messaging.token = '  ';
      await registrar.signIn(api);

      expect(api.registered, isEmpty);
      expect(registrar.lastToken, isNull);
    });

    test('re-registers after a token refresh', () async {
      await registrar.signIn(api);
      messaging.refresh('token-2');
      await _settle();

      expect(api.registered, ['android:token-1', 'android:token-2']);
      expect(registrar.lastToken, 'token-2');
    });

    test('logs a failed registration and retries on the next sign-in', () async {
      api.failRegister = true;
      await registrar.signIn(api);

      expect(api.registered, isEmpty);
      expect(registrar.lastToken, 'token-1');

      api.failRegister = false;
      await registrar.signIn(api);

      expect(api.registered, ['android:token-1']);
    });

    test('deletes the last token on sign-out before going inactive', () async {
      await registrar.signIn(api);
      await registrar.signOut(api);

      expect(api.unregistered, ['token-1']);
      expect(messaging.invalidations, 1);
      expect(registrar.lastToken, isNull);
    });

    test('invalidates the Firebase token after a failed server delete', () async {
      api.failUnregister = true;
      await registrar.signIn(api);

      await expectLater(registrar.signOut(api), completes);

      expect(messaging.invalidations, 1);
      expect(registrar.lastToken, isNull);
    });

    test('does not invalidate the Firebase token without a token', () async {
      messaging.token = '';
      await registrar.signIn(api);

      await registrar.signOut(api);

      expect(api.unregistered, isEmpty);
      expect(messaging.invalidations, 0);
    });

    test('ignores token refreshes after sign-out', () async {
      await registrar.signIn(api);
      await registrar.signOut(api);

      messaging.refresh('token-3');
      await _settle();

      expect(api.registered, ['android:token-1']);
    });

    test('never throws when the unregister call fails', () async {
      api.failUnregister = true;
      await registrar.signIn(api);

      await expectLater(registrar.signOut(api), completes);
      expect(messaging.invalidations, 1);
      expect(registrar.lastToken, isNull);
    });

    test('forwards foreground messages', () async {
      final events = <void>[];
      final sub = registrar.messages.listen(events.add);
      addTearDown(sub.cancel);

      await registrar.signIn(api);
      messaging.message();
      await _settle();

      expect(events, hasLength(1));
    });
  });

  group('PushService', () {
    test('is disabled and silent without a registrar', () async {
      final service = PushService();
      final api = _RecordingApi();

      expect(service.enabled, isFalse);
      await service.signIn(api);
      await service.signOut(api);

      expect(api.registered, isEmpty);
      expect(api.unregistered, isEmpty);
    });

    test('forwards messages from an attached registrar', () async {
      final messaging = _FakeMessaging();
      final registrar = PushRegistrar(messaging);
      final service = PushService(registrar: registrar);
      addTearDown(() async {
        await registrar.dispose();
        await messaging.close();
      });

      final events = <void>[];
      final sub = service.onMessage.listen(events.add);
      addTearDown(sub.cancel);

      await registrar.signIn(_RecordingApi());
      messaging.message();
      await _settle();

      expect(service.enabled, isTrue);
      expect(events, hasLength(1));
    });
  });
}
