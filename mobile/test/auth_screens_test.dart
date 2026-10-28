import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/password_reset_screen.dart';
import 'package:sodapop/src/screens/register_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _FakeAuthApi extends ApiClient {
  _FakeAuthApi({
    this.result = const RegistrationResult(status: 'verification_sent'),
    this.registerError,
    this.resetError,
  });

  final RegistrationResult result;
  final Object? registerError;
  final Object? resetError;

  final List<Map<String, String>> registrations = [];
  final List<String> resends = [];
  final List<String> resets = [];

  @override
  Future<RegistrationResult> register({
    required String email,
    required String handle,
    required String displayName,
    required String password,
  }) async {
    if (registerError != null) throw registerError!;
    registrations.add({
      'email': email,
      'handle': handle,
      'display_name': displayName,
      'password': password,
    });
    return result;
  }

  @override
  Future<void> resendVerification(String email) async {
    resends.add(email);
  }

  @override
  Future<void> requestPasswordReset(String email) async {
    if (resetError != null) throw resetError!;
    resets.add(email);
  }
}

Widget _app(_FakeAuthApi api, Widget home) {
  return ProviderScope(
    overrides: [apiProvider.overrideWithValue(api)],
    child: MaterialApp(theme: sodaTheme(Brightness.light), home: home),
  );
}

Future<void> _pump(WidgetTester tester, Widget app) async {
  tester.view.physicalSize = const Size(400, 1000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(app);
  await tester.pumpAndSettle();
}

Future<void> _fillRegister(
  WidgetTester tester, {
  String handle = '@ada_lovelace',
  String email = 'ada@example.com',
  String password = 'fizzywater1',
}) async {
  await tester.enterText(
    find.byKey(const Key('register-display-name')),
    'Ada Lovelace',
  );
  await tester.enterText(find.byKey(const Key('register-handle')), handle);
  await tester.enterText(find.byKey(const Key('register-email')), email);
  await tester.enterText(find.byKey(const Key('register-password')), password);
  await tester.enterText(find.byKey(const Key('register-confirm')), password);
}

Future<void> _submitRegister(WidgetTester tester) async {
  await tester.ensureVisible(find.byKey(const Key('register-submit')));
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const Key('register-submit')));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('register validation blocks the request', (tester) async {
    final api = _FakeAuthApi();
    await _pump(tester, _app(api, const RegisterScreen()));

    await _fillRegister(tester, email: 'not-an-email', password: 'short');
    await _submitRegister(tester);

    expect(api.registrations, isEmpty);
    expect(find.text('Enter a valid email address.'), findsOneWidget);
    expect(find.text('Use at least 10 characters.'), findsOneWidget);
    expect(find.text('Create your account'), findsOneWidget);
  });

  testWidgets('a successful registration shows the inbox state', (
    tester,
  ) async {
    final api = _FakeAuthApi(
      result: const RegistrationResult(status: 'verification_sent'),
    );
    bool? reported;

    await _pump(
      tester,
      ProviderScope(
        overrides: [apiProvider.overrideWithValue(api)],
        child: MaterialApp(
          theme: sodaTheme(Brightness.light),
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: TextButton(
                  onPressed: () async {
                    reported = await RegisterScreen.open(context);
                  },
                  child: const Text('Open register'),
                ),
              ),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Open register'));
    await tester.pumpAndSettle();

    await _fillRegister(tester);
    await _submitRegister(tester);

    expect(api.registrations, hasLength(1));
    expect(api.registrations.single['email'], 'ada@example.com');
    expect(api.registrations.single['handle'], 'ada_lovelace');
    expect(api.registrations.single['display_name'], 'Ada Lovelace');
    expect(find.text('Check your inbox'), findsOneWidget);
    expect(find.text('ada@example.com'), findsOneWidget);
    expect(find.textContaining('verification link'), findsOneWidget);

    await tester.ensureVisible(
      find.byKey(const Key('register-back-to-sign-in')),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('register-back-to-sign-in')));
    await tester.pumpAndSettle();

    expect(reported, isTrue);
    expect(find.text('Open register'), findsOneWidget);
  });

  testWidgets('resending an email starts a cooldown', (tester) async {
    final api = _FakeAuthApi();
    await _pump(tester, _app(api, const RegisterScreen()));

    await _fillRegister(tester);
    await _submitRegister(tester);

    await tester.ensureVisible(find.byKey(const Key('register-resend')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('register-resend')));
    await tester.pump();

    expect(api.resends, ['ada@example.com']);
    expect(
      find.text(
        'If that address needs verification, a new email is on its way.',
      ),
      findsOneWidget,
    );
    expect(
      tester
          .widget<OutlinedButton>(find.byKey(const Key('register-resend')))
          .onPressed,
      isNull,
    );
    expect(find.text('Resend in 60s'), findsOneWidget);

    await tester.tap(
      find.byKey(const Key('register-resend')),
      warnIfMissed: false,
    );
    await tester.pump();
    expect(api.resends, hasLength(1));

    await tester.pump(const Duration(seconds: 61));

    expect(
      tester
          .widget<OutlinedButton>(find.byKey(const Key('register-resend')))
          .onPressed,
      isNotNull,
    );
    expect(find.text('Resend email'), findsOneWidget);
  });

  testWidgets('a conflict maps to one neutral message', (tester) async {
    final api = _FakeAuthApi(
      registerError: ApiException(
        'that handle or email is already in use',
        statusCode: 409,
      ),
    );
    await _pump(tester, _app(api, const RegisterScreen()));

    await _fillRegister(tester);
    await _submitRegister(tester);

    expect(
      find.text('That handle or email is already in use.'),
      findsOneWidget,
    );
    expect(find.byKey(const Key('register-submit')), findsOneWidget);
    expect(find.text('Check your inbox'), findsNothing);
  });

  testWidgets('the first account confirmation is shown', (tester) async {
    final api = _FakeAuthApi(
      result: const RegistrationResult(status: 'created'),
    );
    await _pump(tester, _app(api, const RegisterScreen()));

    await _fillRegister(tester);
    await _submitRegister(tester);

    expect(find.text('Account created'), findsOneWidget);
    expect(find.byKey(const Key('register-continue')), findsOneWidget);
  });

  testWidgets('reset confirms neutrally and allows another address', (
    tester,
  ) async {
    final api = _FakeAuthApi();
    await _pump(tester, _app(api, const PasswordResetScreen()));

    await tester.enterText(
      find.byKey(const Key('reset-email')),
      'ada@example.com',
    );
    await tester.tap(find.byKey(const Key('reset-submit')));
    await tester.pumpAndSettle();

    expect(api.resets, ['ada@example.com']);
    expect(
      find.text('If that address has an account, a reset link is on its way.'),
      findsOneWidget,
    );
    expect(find.textContaining('web page'), findsOneWidget);

    await tester.tap(find.byKey(const Key('reset-retry')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('reset-email')), findsOneWidget);

    await tester.enterText(
      find.byKey(const Key('reset-email')),
      'grace@example.com',
    );
    await tester.tap(find.byKey(const Key('reset-submit')));
    await tester.pumpAndSettle();

    expect(api.resets, ['ada@example.com', 'grace@example.com']);
  });

  testWidgets('the auth screens fit a narrow phone', (tester) async {
    tester.view.physicalSize = const Size(320, 640);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(_app(_FakeAuthApi(), const RegisterScreen()));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(_app(_FakeAuthApi(), const PasswordResetScreen()));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });

  testWidgets('a network failure keeps the neutral reset confirmation', (
    tester,
  ) async {
    final api = _FakeAuthApi(
      resetError: ApiException(
        'Cannot reach the server. Check the address and try again.',
      ),
    );
    await _pump(tester, _app(api, const PasswordResetScreen()));

    await tester.enterText(
      find.byKey(const Key('reset-email')),
      'ada@example.com',
    );
    await tester.tap(find.byKey(const Key('reset-submit')));
    await tester.pumpAndSettle();

    expect(api.resets, isEmpty);
    expect(
      find.text('If that address has an account, a reset link is on its way.'),
      findsOneWidget,
    );
    expect(
      find.text(
        'The request did not go through just now. You can try again in a moment.',
      ),
      findsOneWidget,
    );
    expect(find.textContaining('Cannot reach the server'), findsNothing);
  });
}
