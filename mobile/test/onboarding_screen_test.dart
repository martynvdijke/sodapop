import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/auth/server_defaults.dart';
import 'package:sodapop/src/screens/onboarding_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _FakeAuth extends AuthController {
  _FakeAuth(this.initial);

  final AuthState initial;
  int connects = 0;
  String? lastProbed;
  String? lastConnected;

  @override
  AuthState build() => initial;

  @override
  Future<bool> connect(String rawUrl) async {
    connects++;
    lastProbed = rawUrl;
    lastConnected = rawUrl;
    state = state.copyWith(serverUrl: rawUrl, busy: false, clearError: true);
    return true;
  }
}

Widget _app(AuthController auth) => ProviderScope(
  overrides: [authProvider.overrideWith(() => auth)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const OnboardingScreen(),
  ),
);

Future<void> _pump(WidgetTester tester, _FakeAuth auth) async {
  tester.view.physicalSize = const Size(420, 1200);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(_app(auth));
  await tester.pumpAndSettle();
}

String _serverText(WidgetTester tester) =>
    tester.widget<TextField>(find.byKey(const Key('onboarding-server'))).controller!.text;

void main() {
  testWidgets('first run prefills the hosted default and probes it', (tester) async {
    final auth = _FakeAuth(const AuthState(loading: false));
    await _pump(tester, auth);

    expect(_serverText(tester), defaultServerUrl);
    expect(
      find.text('Use the default instance below, or enter another self-hosted Sodapop server.'),
      findsOneWidget,
    );
    expect(auth.connects, 1);
    expect(auth.lastProbed, defaultServerUrl);
  });

  testWidgets('a saved server URL wins and is not replaced', (tester) async {
    final auth = _FakeAuth(
      const AuthState(
        loading: false,
        serverUrl: 'https://soda.example',
        hasSavedServer: true,
      ),
    );
    await _pump(tester, auth);

    expect(_serverText(tester), 'https://soda.example');
    expect(auth.connects, 0);
    expect(auth.lastProbed, isNull);
  });
}
