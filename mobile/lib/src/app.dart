import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'auth/auth_controller.dart';
import 'screens/home_shell.dart';
import 'screens/onboarding_screen.dart';
import 'theme/soda_theme.dart';

class SodapopApp extends ConsumerWidget {
  const SodapopApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authProvider);
    return MaterialApp(
      title: 'Sodapop',
      debugShowCheckedModeBanner: false,
      theme: sodaTheme(Brightness.light),
      darkTheme: sodaTheme(Brightness.dark),
      themeMode: ThemeMode.system,
      home: auth.loading
          ? const Scaffold(body: Center(child: CircularProgressIndicator(color: Soda.lime)))
          : auth.signedIn
              ? const HomeShell()
              : const OnboardingScreen(),
    );
  }
}
