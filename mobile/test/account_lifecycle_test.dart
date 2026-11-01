import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/settings_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _settings = AccountSettings(
  profile: SettingsProfile(
    handle: 'ada',
    displayName: 'Ada Lovelace',
    bio: 'Cherry cola, always.',
    email: 'ada@example.com',
    emailVerified: true,
    role: 'member',
  ),
  prefs: SettingsPrefs(theme: 'light'),
  tokenCount: 1,
);

class _FakeAccountApi extends ApiClient {
  String exportDocument = '{"a":1}';
  bool exportFails = false;
  ApiException? deleteError;
  int exportCalls = 0;
  final List<String> deleted = [];

  @override
  Future<AccountSettings> settings() async => _settings;

  @override
  Future<List<SettingsSession>> listSessions() async => const [];

  @override
  Future<List<BlockedUser>> blockedUsers() async => const [];

  @override
  Future<String> exportAccount() async {
    exportCalls++;
    if (exportFails) throw ApiException('Cannot reach the server.');
    return exportDocument;
  }

  @override
  Future<void> deleteAccount(String confirm) async {
    final error = deleteError;
    if (error != null) throw error;
    deleted.add(confirm);
  }
}

class _FakeAuth extends AuthController {
  int signOuts = 0;

  @override
  AuthState build() => const AuthState(
    loading: false,
    serverUrl: 'https://soda.example',
    token: 'sd_test_token_1234567890',
    me: Me(
      id: 'u1',
      handle: 'ada',
      displayName: 'Ada Lovelace',
      email: 'ada@example.com',
    ),
  );

  @override
  Future<void> signOut() async {
    signOuts++;
    state = const AuthState(loading: false, serverUrl: 'https://soda.example');
  }
}

Widget _app(ApiClient api, AuthController auth) => ProviderScope(
  overrides: [
    apiProvider.overrideWithValue(api),
    authProvider.overrideWith(() => auth),
  ],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const SettingsScreen(),
  ),
);

Future<(_FakeAccountApi, _FakeAuth)> _pump(
  WidgetTester tester, {
  _FakeAccountApi? api,
  _FakeAuth? auth,
}) async {
  tester.view.physicalSize = const Size(500, 3200);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  final fakeApi = api ?? _FakeAccountApi();
  final fakeAuth = auth ?? _FakeAuth();
  await tester.pumpWidget(_app(fakeApi, fakeAuth));
  await tester.pumpAndSettle();
  return (fakeApi, fakeAuth);
}

Future<void> _openExport(WidgetTester tester) async {
  final tile = find.byKey(const Key('settings-export'));
  await tester.ensureVisible(tile);
  await tester.tap(tile);
  await tester.pumpAndSettle();
}

Future<void> _openClose(WidgetTester tester) async {
  final tile = find.byKey(const Key('settings-close-account'));
  await tester.ensureVisible(tile);
  await tester.tap(tile);
  await tester.pumpAndSettle();
}

FilledButton _submitButton(WidgetTester tester) =>
    tester.widget<FilledButton>(find.byKey(const Key('close-account-submit')));

void main() {
  testWidgets('export shows the document size and copies the raw JSON', (
    tester,
  ) async {
    final copied = <String>[];
    final messenger = tester.binding.defaultBinaryMessenger;
    messenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') {
        copied.add((call.arguments as Map)['text'] as String);
      }
      return null;
    });
    addTearDown(
      () => messenger.setMockMethodCallHandler(SystemChannels.platform, null),
    );

    final (api, _) = await _pump(tester);
    await _openExport(tester);

    expect(api.exportCalls, 1);
    expect(find.text('Your data export'), findsOneWidget);
    expect(find.text('7 B'), findsOneWidget);
    expect(find.text('sodapop-export.json'), findsOneWidget);

    await tester.tap(find.byKey(const Key('export-copy')));
    await tester.pumpAndSettle();

    expect(copied, ['{"a":1}']);
    expect(find.text('Copied'), findsOneWidget);

    await tester.tap(find.byKey(const Key('export-close')));
    await tester.pumpAndSettle();
    expect(find.text('Your data export'), findsNothing);
  });

  testWidgets('a failed export shows the error and retries', (tester) async {
    final api = _FakeAccountApi()..exportFails = true;
    await _pump(tester, api: api);
    await _openExport(tester);

    expect(find.text('Download failed'), findsOneWidget);
    expect(find.text('Cannot reach the server.'), findsOneWidget);
    expect(find.text('Your data export'), findsNothing);

    api.exportFails = false;
    await tester.tap(find.byKey(const Key('export-error-retry')));
    await tester.pumpAndSettle();

    expect(api.exportCalls, 2);
    expect(find.text('Your data export'), findsOneWidget);
    expect(find.text('7 B'), findsOneWidget);
  });

  testWidgets('close account stays disabled until the exact handle is typed', (
    tester,
  ) async {
    final (api, _) = await _pump(tester);
    await _openClose(tester);

    expect(find.text('Close your account?'), findsOneWidget);
    expect(find.text('This cannot be undone.'), findsOneWidget);
    expect(find.text('Your handle becomes available again.'), findsOneWidget);
    expect(_submitButton(tester).onPressed, isNull);

    await tester.enterText(
      find.byKey(const Key('close-account-confirm')),
      'ad',
    );
    await tester.pump();
    expect(_submitButton(tester).onPressed, isNull);

    await tester.enterText(
      find.byKey(const Key('close-account-confirm')),
      'ada',
    );
    await tester.pump();
    expect(_submitButton(tester).onPressed, isNotNull);

    expect(api.deleted, isEmpty);
  });

  testWidgets('a wrong handle keeps the close button disabled', (tester) async {
    final (api, _) = await _pump(tester);
    await _openClose(tester);

    for (final value in ['bob', 'Ada', 'adaa', '@bob']) {
      await tester.enterText(
        find.byKey(const Key('close-account-confirm')),
        value,
      );
      await tester.pump();
      expect(
        _submitButton(tester).onPressed,
        isNull,
        reason: '"$value" must not enable deletion',
      );
    }

    await tester.enterText(
      find.byKey(const Key('close-account-confirm')),
      '@ada',
    );
    await tester.pump();
    expect(_submitButton(tester).onPressed, isNotNull);
    expect(api.deleted, isEmpty);
  });

  testWidgets('closing the account calls deleteAccount and signs out', (
    tester,
  ) async {
    final (api, auth) = await _pump(tester);
    await _openClose(tester);

    await tester.enterText(
      find.byKey(const Key('close-account-confirm')),
      'ada',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('close-account-submit')));
    await tester.pumpAndSettle();

    expect(api.deleted, ['ada']);
    expect(auth.signOuts, 1);
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('Close account'), findsOneWidget);
  });

  testWidgets('409 last_admin keeps the dialog open with the message', (
    tester,
  ) async {
    final api = _FakeAccountApi()
      ..deleteError = ApiException('last_admin', statusCode: 409);
    final auth = _FakeAuth();
    await _pump(tester, api: api, auth: auth);
    await _openClose(tester);

    await tester.enterText(
      find.byKey(const Key('close-account-confirm')),
      'ada',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('close-account-submit')));
    await tester.pumpAndSettle();

    expect(
      find.text(
        'This is the only admin account. Promote another admin before closing it.',
      ),
      findsOneWidget,
    );
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(
      tester
          .widget<TextField>(find.byKey(const Key('close-account-confirm')))
          .controller
          ?.text,
      'ada',
    );
    expect(api.deleted, isEmpty);
    expect(auth.signOuts, 0);
  });
}
