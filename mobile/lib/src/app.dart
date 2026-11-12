import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'auth/auth_controller.dart';
import 'api/notification_stream.dart';
import 'offline/checkin_outbox.dart';
import 'push/push_service.dart';
import 'screens/checkin_detail_screen.dart';
import 'screens/home_shell.dart';
import 'screens/notifications_screen.dart';
import 'screens/onboarding_screen.dart';
import 'screens/public_shell.dart';
import 'telemetry/telemetry_bootstrap.dart';
import 'theme/soda_theme.dart';
import 'theme/theme_mode.dart';

class SodapopApp extends ConsumerStatefulWidget {
  const SodapopApp({super.key});

  @override
  ConsumerState<SodapopApp> createState() => _SodapopAppState();
}

class _SodapopAppState extends ConsumerState<SodapopApp>
    with WidgetsBindingObserver {
  StreamSubscription<List<ConnectivityResult>>? _connectivity;
  StreamSubscription<String>? _pushOpenSub;
  final GlobalKey<NavigatorState> _navigatorKey = GlobalKey<NavigatorState>();
  bool _signedIn = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _watchConnectivity();
    _watchAuth();
    _watchPushOpens();
  }

  /// Loads the saved theme once the account is available. Nothing here blocks
  /// the first paint; an offline or unconfigured server keeps system mode.
  void _watchAuth() {
    ref.listenManual<AuthState>(authProvider, (previous, next) {
      if (next.signedIn) {
        if (!_signedIn) {
          _signedIn = true;
          unawaited(_loadTheme());
        }
        _startNotificationStream(next);
        return;
      }
      if (_signedIn) {
        _signedIn = false;
        NotificationCenter.instance.stop();
        ref.read(themeModeProvider.notifier).apply(ThemeMode.system);
      }
    }, fireImmediately: true);
  }

  /// The live stream is token-scoped, so it follows the signed-in account and
  /// stops the moment the account changes.
  void _startNotificationStream(AuthState auth) {
    final server = auth.serverUrl;
    if (server.isEmpty || auth.token.isEmpty) return;
    NotificationCenter.instance.start(serverUrl: server, token: auth.token);
  }

  /// A tapped notification can arrive while the app is running or be the
  /// reason it started; both land on the same href router.
  void _watchPushOpens() {
    _pushOpenSub = PushService.instance.onOpen.listen(_openPushLink);
    unawaited(
      PushService.instance.initialLink().then((link) {
        if (link == null || !mounted) return;
        WidgetsBinding.instance.addPostFrameCallback((_) => _openPushLink(link));
      }),
    );
  }

  /// Push payloads carry the same relative hrefs as the inbox. Check-ins open
  /// directly; anything else lands on the inbox, which deep-links per row.
  void _openPushLink(String href) {
    if (!mounted || !ref.read(authProvider).signedIn) return;
    final navigator = _navigatorKey.currentState;
    if (navigator == null) return;
    final path = Uri.tryParse(href)?.path ?? '';
    final segments = path.split('/').where((part) => part.isNotEmpty).toList();
    if (segments.length == 2 && segments.first == 'checkins') {
      unawaited(
        navigator.push(
          MaterialPageRoute<void>(
            builder: (_) => CheckinDetailScreen(checkinId: segments[1]),
          ),
        ),
      );
      return;
    }
    unawaited(
      navigator.push(
        MaterialPageRoute<void>(builder: (_) => const NotificationsScreen()),
      ),
    );
  }

  Future<void> _loadTheme() async {
    final mode = await loadSavedThemeMode(ref.read(apiProvider));
    if (!mounted || mode == null) return;
    if (!ref.read(authProvider).signedIn) return;
    ref.read(themeModeProvider.notifier).apply(mode);
  }

  void _watchConnectivity() {
    try {
      _connectivity = Connectivity().onConnectivityChanged.listen((results) {
        if (results.any((result) => result != ConnectivityResult.none)) {
          _flushOutbox();
        }
      }, onError: (_) {});
    } catch (_) {
      // Connectivity is best effort; the app still works without the plugin.
    }
  }

  Future<void> _flushOutbox() async {
    try {
      final api = ref.read(apiProvider);
      if (!api.configured) return;
      await CheckinOutbox().flush(api);
    } catch (_) {
      // The feed shows pending check-ins; a failed flush is retried later.
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _flushOutbox();
      // FCM covers the background; the live stream only earns its keep while
      // the app is open.
      final auth = ref.read(authProvider);
      if (auth.signedIn) _startNotificationStream(auth);
      return;
    }
    if (state == AppLifecycleState.paused) {
      NotificationCenter.instance.stop();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _connectivity?.cancel();
    _pushOpenSub?.cancel();
    NotificationCenter.instance.stop();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    final themeMode = ref.watch(themeModeProvider);
    return TelemetryBootstrap(
      child: MaterialApp(
        title: 'Sodapop',
        navigatorKey: _navigatorKey,
        debugShowCheckedModeBanner: false,
        theme: sodaTheme(Brightness.light),
        darkTheme: sodaTheme(Brightness.dark),
        themeMode: themeMode,
        home: auth.loading
            ? const Scaffold(
                body: Center(child: CircularProgressIndicator(color: Soda.lime)),
              )
            : auth.signedIn
            ? const HomeShell()
            : auth.preview
            ? const PublicShell()
            : const OnboardingScreen(),
      ),
    );
  }
}
