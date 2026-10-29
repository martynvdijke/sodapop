import 'dart:async';
import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../push/push_service.dart';
import '../utils/url.dart';

final apiProvider = Provider<ApiClient>((ref) => ApiClient());

final authProvider = NotifierProvider<AuthController, AuthState>(AuthController.new);

class AuthState {
  const AuthState({
    this.loading = true,
    this.busy = false,
    this.serverUrl = '',
    this.token = '',
    this.me,
    this.oidcEnabled = false,
    this.passwordFallback = true,
    this.error,
  });

  final bool loading;
  final bool busy;
  final String serverUrl;
  final String token;
  final Me? me;
  final bool oidcEnabled;
  final bool passwordFallback;
  final String? error;

  bool get signedIn => token.isNotEmpty && me != null;

  AuthState copyWith({
    bool? loading,
    bool? busy,
    String? serverUrl,
    String? token,
    Me? me,
    bool? oidcEnabled,
    bool? passwordFallback,
    String? error,
    bool clearError = false,
  }) =>
      AuthState(
        loading: loading ?? this.loading,
        busy: busy ?? this.busy,
        serverUrl: serverUrl ?? this.serverUrl,
        token: token ?? this.token,
        me: me ?? this.me,
        oidcEnabled: oidcEnabled ?? this.oidcEnabled,
        passwordFallback: passwordFallback ?? this.passwordFallback,
        error: clearError ? null : (error ?? this.error),
      );
}

class AuthController extends Notifier<AuthState> {
  static const _storage = FlutterSecureStorage();
  static const _serverKey = 'sodapop.server';
  static const _tokenKey = 'sodapop.token';

  @override
  AuthState build() {
    Future.microtask(_restore);
    return const AuthState();
  }

  Future<void> _restore() async {
    final server = await _storage.read(key: _serverKey);
    final token = await _storage.read(key: _tokenKey);
    if (server == null || token == null || token.isEmpty) {
      state = const AuthState(loading: false);
      return;
    }
    final api = ref.read(apiProvider);
    api.configure(server, token: token);
    try {
      final me = await api.me();
      final status = await api.oidcStatus();
      state = AuthState(
        loading: false,
        serverUrl: server,
        token: token,
        me: me,
        oidcEnabled: status.oidcEnabled,
        passwordFallback: status.passwordFallback,
      );
      unawaited(PushService.instance.signIn(api));
    } catch (_) {
      state = AuthState(loading: false, serverUrl: server);
    }
  }

  /// Validates a server origin and probes its authentication capabilities.
  Future<bool> connect(String rawUrl) async {
    final server = normalizeServerUrl(rawUrl);
    if (server == null) {
      state = state.copyWith(
        busy: false,
        error: 'Enter a full http:// or https:// address, without a path.',
      );
      return false;
    }
    state = state.copyWith(busy: true, clearError: true);
    final api = ref.read(apiProvider);
    api.configure(server);
    try {
      final status = await api.oidcStatus();
      state = state.copyWith(
        busy: false,
        serverUrl: server,
        oidcEnabled: status.oidcEnabled,
        passwordFallback: status.passwordFallback,
        clearError: true,
      );
      return true;
    } catch (error) {
      state = state.copyWith(busy: false, error: ApiClient.describe(error).message);
      return false;
    }
  }

  Future<bool> signInWithToken(String rawToken) async {
    final token = rawToken.trim();
    if (!token.startsWith('sd_') || token.length < 20) {
      state = state.copyWith(error: 'Personal tokens start with sd_');
      return false;
    }
    if (state.serverUrl.isEmpty) {
      state = state.copyWith(error: 'Connect to a server first');
      return false;
    }
    state = state.copyWith(busy: true, clearError: true);
    final api = ref.read(apiProvider);
    api.configure(state.serverUrl);
    api.setToken(token);
    try {
      final me = await api.me(tokenOverride: token);
      await _storage.write(key: _serverKey, value: state.serverUrl);
      await _storage.write(key: _tokenKey, value: token);
      api.setToken(token);
      state = state.copyWith(busy: false, token: token, me: me, clearError: true);
      unawaited(PushService.instance.signIn(api));
      return true;
    } catch (error) {
      api.setToken(null);
      state = state.copyWith(busy: false, error: ApiClient.describe(error).message);
      return false;
    }
  }

  Future<bool> signInWithPassword(String login, String password) async {
    state = state.copyWith(busy: true, clearError: true);
    try {
      final token = await ref.read(apiProvider).passwordToken(login, password);
      return await signInWithToken(token);
    } catch (error) {
      state = state.copyWith(busy: false, error: ApiClient.describe(error).message);
      return false;
    }
  }

  Future<bool> completeOidc(String cookie, String csrf) async {
    state = state.copyWith(busy: true, clearError: true);
    try {
      final token = await ref.read(apiProvider).exchangeBrowserSession(
            cookie: cookie,
            csrf: csrf,
            serverUrl: state.serverUrl,
          );
      return await signInWithToken(token);
    } catch (error) {
      state = state.copyWith(busy: false, error: ApiClient.describe(error).message);
      return false;
    }
  }

  Future<void> signOut() async {
    final api = ref.read(apiProvider);
    // Start the best-effort device removal while the bearer token is still
    // available; sign-out never waits for the network.
    unawaited(PushService.instance.signOut(api));
    await _storage.delete(key: _tokenKey);
    api.setToken(null);
    state = AuthState(
      loading: false,
      serverUrl: state.serverUrl,
      oidcEnabled: state.oidcEnabled,
      passwordFallback: state.passwordFallback,
    );
  }

  void clearError() => state = state.copyWith(clearError: true);

  static String csrfFromJson(String payload) {
    final decoded = jsonDecode(payload);
    if (decoded is Map<String, dynamic> && decoded['csrf'] is String) {
      return decoded['csrf'] as String;
    }
    return '';
  }
}
