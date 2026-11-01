import 'package:flutter/material.dart';
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
    role: 'admin',
  ),
  prefs: SettingsPrefs(
    theme: 'light',
    units: 'metric',
    emailWeekly: true,
    emailFriendReq: false,
    pushEnabled: false,
    publicProfile: true,
    analyticsOptOut: false,
    caffeineBedtime: '21:30',
    defaultVisibility: 'friends',
  ),
  tokenCount: 2,
  blockedCount: 1,
);

const _sessionRows = [
  SettingsSession(
    id: 4,
    ip: '203.0.113.4',
    userAgent: 'Mozilla/5.0 (Windows NT 10.0) Firefox/130',
    createdAt: '2026-10-20T10:00:00Z',
    lastSeenAt: '2026-10-27T18:30:00Z',
  ),
  SettingsSession(
    id: 9,
    ip: '198.51.100.8',
    userAgent: 'Mozilla/5.0 (Linux; Android 15)',
    createdAt: '2026-10-25T09:00:00Z',
    lastSeenAt: '2026-10-28T07:00:00Z',
    current: true,
  ),
];

const _blockedRows = [
  BlockedUser(
    id: 'b1',
    handle: 'bob',
    displayName: 'Bob Fizz',
    blockedAt: '2026-10-01T00:00:00Z',
  ),
];

class _FakeSettingsApi extends ApiClient {
  _FakeSettingsApi({
    AccountSettings settings = _settings,
    List<SettingsSession> sessionRows = _sessionRows,
    List<BlockedUser> blockedRows = _blockedRows,
  }) : current = settings,
       sessionRows = List.of(sessionRows),
       blockedRows = List.of(blockedRows);

  AccountSettings current;
  List<SettingsSession> sessionRows;
  List<BlockedUser> blockedRows;

  final List<Map<String, Object?>> updates = [];
  final List<int> revoked = [];
  final List<(String, String)> passwordCalls = [];
  final List<String> unblocked = [];
  int revokeAllCalls = 0;
  int settingsCalls = 0;
  bool settingsFails = false;
  bool updateFails = false;
  ApiException? passwordError;

  @override
  Future<AccountSettings> settings() async {
    settingsCalls++;
    if (settingsFails) throw ApiException('Cannot reach the server.');
    return current;
  }

  @override
  Future<AccountSettings> updateSettings({
    String? displayName,
    String? bio,
    String? theme,
    String? units,
    bool? emailWeekly,
    bool? emailFriendReq,
    bool? pushEnabled,
    bool? publicProfile,
    bool? analyticsOptOut,
    String? caffeineBedtime,
    String? defaultVisibility,
  }) async {
    final fields = <String, Object?>{
      'display_name': ?displayName,
      'bio': ?bio,
      'theme': ?theme,
      'units': ?units,
      'email_weekly': ?emailWeekly,
      'email_friend_req': ?emailFriendReq,
      'push_enabled': ?pushEnabled,
      'public_profile': ?publicProfile,
      'analytics_opt_out': ?analyticsOptOut,
      'caffeine_bedtime': ?caffeineBedtime,
      'default_visibility': ?defaultVisibility,
    };
    updates.add(fields);
    if (updateFails) throw ApiException('Could not save.');

    final profile = current.profile;
    final prefs = current.prefs;
    current = AccountSettings(
      profile: SettingsProfile(
        handle: profile.handle,
        displayName: displayName ?? profile.displayName,
        bio: bio ?? profile.bio,
        email: profile.email,
        emailVerified: profile.emailVerified,
        role: profile.role,
      ),
      prefs: SettingsPrefs(
        theme: theme ?? prefs.theme,
        units: units ?? prefs.units,
        emailWeekly: emailWeekly ?? prefs.emailWeekly,
        emailFriendReq: emailFriendReq ?? prefs.emailFriendReq,
        pushEnabled: pushEnabled ?? prefs.pushEnabled,
        publicProfile: publicProfile ?? prefs.publicProfile,
        analyticsOptOut: analyticsOptOut ?? prefs.analyticsOptOut,
        caffeineBedtime: caffeineBedtime ?? prefs.caffeineBedtime,
        defaultVisibility: defaultVisibility ?? prefs.defaultVisibility,
      ),
      sessions: current.sessions,
      tokenCount: current.tokenCount,
      blockedCount: current.blockedCount,
    );
    return current;
  }

  @override
  Future<List<SettingsSession>> listSessions() async => List.of(sessionRows);

  @override
  Future<void> revokeSession(int id) async {
    revoked.add(id);
    sessionRows = sessionRows.where((row) => row.id != id).toList();
  }

  @override
  Future<void> revokeAllSessions() async {
    revokeAllCalls++;
    sessionRows = [];
  }

  @override
  Future<void> changePassword(
    String currentPassword,
    String newPassword,
  ) async {
    passwordCalls.add((currentPassword, newPassword));
    final error = passwordError;
    if (error != null) throw error;
  }

  @override
  Future<List<BlockedUser>> blockedUsers() async => List.of(blockedRows);

  @override
  Future<void> unblockUser(String handle) async {
    unblocked.add(handle);
    blockedRows = blockedRows.where((user) => user.handle != handle).toList();
  }
}

Widget _app(ApiClient api) => ProviderScope(
  overrides: [apiProvider.overrideWithValue(api)],
  child: MaterialApp(
    theme: sodaTheme(Brightness.light),
    home: const SettingsScreen(),
  ),
);

Future<_FakeSettingsApi> _pump(
  WidgetTester tester, {
  _FakeSettingsApi? api,
}) async {
  tester.view.physicalSize = const Size(500, 3000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  final fake = api ?? _FakeSettingsApi();
  await tester.pumpWidget(_app(fake));
  await tester.pumpAndSettle();
  return fake;
}

String _fieldText(WidgetTester tester, String key) {
  final field = tester.widget<TextField>(find.byKey(Key(key)));
  return field.controller?.text ?? '';
}

void main() {
  testWidgets('settings render every section with current values', (
    tester,
  ) async {
    await _pump(tester);

    expect(find.text('Profile'), findsOneWidget);
    expect(find.text('Appearance'), findsOneWidget);
    expect(find.text('Notifications'), findsOneWidget);
    expect(find.text('Privacy'), findsOneWidget);
    expect(find.text('Caffeine'), findsOneWidget);
    expect(find.text('Security'), findsOneWidget);
    expect(find.text('Sessions'), findsOneWidget);
    expect(find.text('Blocked people'), findsNWidgets(2));
    expect(find.text('Account'), findsOneWidget);

    expect(_fieldText(tester, 'settings-display-name'), 'Ada Lovelace');
    expect(_fieldText(tester, 'settings-bio'), 'Cherry cola, always.');
    expect(find.text('@ada'), findsOneWidget);
    expect(find.text('ada@example.com'), findsOneWidget);
    expect(find.text('Verified'), findsOneWidget);
    expect(find.text('Admin'), findsOneWidget);

    expect(find.text('Weekly email digest'), findsOneWidget);
    expect(find.text('Public profile'), findsOneWidget);
    expect(_fieldText(tester, 'settings-bedtime'), '21:30');

    expect(find.text('Change password'), findsOneWidget);
    expect(find.text('Windows browser'), findsOneWidget);
    expect(find.text('This device'), findsOneWidget);
    expect(find.text('Sign out other sessions'), findsOneWidget);
    expect(find.text('Bob Fizz'), findsOneWidget);
    expect(find.text('@bob'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('profile save sends only the fields that changed', (
    tester,
  ) async {
    final api = await _pump(tester);

    await tester.enterText(
      find.byKey(const Key('settings-display-name')),
      'Ada L.',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('settings-save-profile')));
    await tester.pumpAndSettle();

    expect(api.updates, hasLength(1));
    expect(api.updates.single, {'display_name': 'Ada L.'});
    expect(find.text('Profile saved.'), findsOneWidget);
    expect(_fieldText(tester, 'settings-display-name'), 'Ada L.');
  });

  testWidgets('theme selector applies immediately through updateSettings', (
    tester,
  ) async {
    final api = await _pump(tester);

    await tester.tap(find.text('Dark'));
    await tester.pumpAndSettle();

    expect(api.updates, hasLength(1));
    expect(api.updates.single, {'theme': 'dark'});
    expect(api.current.prefs.theme, 'dark');
    expect(tester.takeException(), isNull);
  });

  testWidgets('password dialog validates and maps server errors', (
    tester,
  ) async {
    final api = await _pump(tester);

    await tester.tap(find.byKey(const Key('settings-change-password')));
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);

    await tester.enterText(
      find.byKey(const Key('password-current')),
      'old-pass-1',
    );
    await tester.enterText(find.byKey(const Key('password-new')), 'short');
    await tester.enterText(find.byKey(const Key('password-confirm')), 'short');
    await tester.tap(find.text('Update password'));
    await tester.pumpAndSettle();

    expect(find.text('Use at least 10 characters.'), findsOneWidget);
    expect(api.passwordCalls, isEmpty);

    await tester.enterText(
      find.byKey(const Key('password-new')),
      'long-enough-1',
    );
    await tester.enterText(
      find.byKey(const Key('password-confirm')),
      'long-enough-1',
    );
    api.passwordError = ApiException(
      'current password is incorrect',
      statusCode: 400,
    );
    await tester.tap(find.text('Update password'));
    await tester.pumpAndSettle();

    expect(find.text('That current password is not right.'), findsOneWidget);
    expect(api.passwordCalls, hasLength(1));

    api.passwordError = null;
    await tester.tap(find.text('Update password'));
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('Password updated.'), findsOneWidget);
  });

  testWidgets('revoking a session refreshes the list', (tester) async {
    final api = await _pump(tester);

    expect(find.text('Windows browser'), findsOneWidget);
    await tester.tap(find.text('Revoke').first);
    await tester.pumpAndSettle();

    await tester.tap(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.text('Revoke'),
      ),
    );
    await tester.pumpAndSettle();

    expect(api.revoked, [4]);
    expect(find.text('Windows browser'), findsNothing);
    expect(find.text('Android device'), findsOneWidget);
    expect(find.text('Session revoked.'), findsOneWidget);
  });

  testWidgets('unblocking refreshes the blocked list', (tester) async {
    final api = await _pump(tester);

    await tester.tap(find.byKey(const Key('unblock-bob')));
    await tester.pumpAndSettle();
    await tester.tap(
      find.descendant(
        of: find.byType(AlertDialog),
        matching: find.text('Unblock'),
      ),
    );
    await tester.pumpAndSettle();

    expect(api.unblocked, ['bob']);
    expect(find.text('No blocked accounts'), findsOneWidget);
    expect(find.text('@bob'), findsNothing);
    expect(find.text('@bob unblocked.'), findsOneWidget);
  });

  testWidgets('a failed preference change keeps the previous value', (
    tester,
  ) async {
    final api = await _pump(tester);
    api.updateFails = true;

    // Push is disabled in a build without Firebase config, so drive the same
    // optimistic-update path through the weekly email switch instead.
    final toggle = find.byKey(const Key('settings-email-weekly'));
    expect(tester.widget<SwitchListTile>(toggle).value, isTrue);

    await tester.tap(toggle);
    await tester.pumpAndSettle();

    expect(tester.widget<SwitchListTile>(toggle).value, isTrue);
    expect(api.updates.single, {'email_weekly': false});
    expect(find.text('Could not save.'), findsOneWidget);
  });

  testWidgets('a failed load shows an error state that retries', (
    tester,
  ) async {
    final api = _FakeSettingsApi()..settingsFails = true;
    await _pump(tester, api: api);

    expect(find.text('Settings did not load'), findsOneWidget);
    expect(find.text('Try again'), findsOneWidget);

    api.settingsFails = false;
    await tester.tap(find.text('Try again'));
    await tester.pumpAndSettle();

    expect(find.text('Profile'), findsOneWidget);
    expect(_fieldText(tester, 'settings-display-name'), 'Ada Lovelace');
    expect(api.settingsCalls, 2);
  });
}
