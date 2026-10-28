import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'auth/auth_controller.dart';
import 'offline/checkin_outbox.dart';
import 'screens/home_shell.dart';
import 'screens/onboarding_screen.dart';
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
  bool _signedIn = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _watchConnectivity();
    _watchAuth();
  }

  /// Loads the saved theme once the account is available. Nothing here blocks
  /// the first paint; an offline or unconfigured server keeps system mode.
  void _watchAuth() {
    ref.listenManual<AuthState>(
      authProvider,
      (previous, next) {
        if (next.signedIn) {
          if (!_signedIn) {
            _signedIn = true;
            unawaited(_loadTheme());
          }
          return;
        }
        if (_signedIn) {
          _signedIn = false;
          ref.read(themeModeProvider.notifier).apply(ThemeMode.system);
        }
      },
      fireImmediately: true,
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
      _connectivity = Connectivity().onConnectivityChanged.listen(
        (results) {
          if (results.any((result) => result != ConnectivityResult.none)) {
            _flushOutbox();
          }
        },
        onError: (_) {},
      );
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
    if (state == AppLifecycleState.resumed) _flushOutbox();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _connectivity?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    final themeMode = ref.watch(themeModeProvider);
    return MaterialApp(
      title: 'Sodapop',
      debugShowCheckedModeBanner: false,
      theme: sodaTheme(Brightness.light),
      darkTheme: sodaTheme(Brightness.dark),
      themeMode: themeMode,
      home: auth.loading
          ? const Scaffold(body: Center(child: CircularProgressIndicator(color: Soda.lime)))
          : auth.signedIn
              ? const HomeShell()
              : const OnboardingScreen(),
    );
  }
}
