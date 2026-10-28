import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/theme/theme_mode.dart';

class _FakeThemeApi extends ApiClient {
  _FakeThemeApi({this.theme = 'system', this.fail = false});

  final String theme;
  final bool fail;
  int calls = 0;

  @override
  Future<AccountSettings> settings() async {
    calls++;
    if (fail) throw ApiException('Cannot reach the server.');
    return AccountSettings(prefs: SettingsPrefs(theme: theme));
  }
}

void main() {
  group('preference mapping', () {
    test('maps the three stored values', () {
      expect(themeModeFromPreference('system'), ThemeMode.system);
      expect(themeModeFromPreference('light'), ThemeMode.light);
      expect(themeModeFromPreference('dark'), ThemeMode.dark);
    });

    test('falls back to system for unknown or missing values', () {
      expect(themeModeFromPreference('sepia'), ThemeMode.system);
      expect(themeModeFromPreference(''), ThemeMode.system);
      expect(themeModeFromPreference(null), ThemeMode.system);
    });

    test('maps back to the stored values', () {
      expect(themeModeToPreference(ThemeMode.system), 'system');
      expect(themeModeToPreference(ThemeMode.light), 'light');
      expect(themeModeToPreference(ThemeMode.dark), 'dark');
      for (final mode in ThemeMode.values) {
        expect(themeModeFromPreference(themeModeToPreference(mode)), mode);
      }
    });
  });

  group('theme mode provider', () {
    test('defaults to system', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);

      expect(container.read(themeModeProvider), ThemeMode.system);
    });

    test('applies preference strings', () {
      final container = ProviderContainer();
      addTearDown(container.dispose);
      final controller = container.read(themeModeProvider.notifier);

      controller.applyPreference('dark');
      expect(container.read(themeModeProvider), ThemeMode.dark);

      controller.applyPreference('sepia');
      expect(container.read(themeModeProvider), ThemeMode.system);
    });

    test('a loaded dark preference updates the provider', () async {
      final api = _FakeThemeApi(theme: 'dark');
      final container = ProviderContainer();
      addTearDown(container.dispose);

      final loaded = await loadSavedThemeMode(api);
      expect(loaded, ThemeMode.dark);
      expect(api.calls, 1);

      container.read(themeModeProvider.notifier).apply(loaded!);
      expect(container.read(themeModeProvider), ThemeMode.dark);
    });

    test('a failed load leaves the provider on system', () async {
      final api = _FakeThemeApi(theme: 'dark', fail: true);
      final container = ProviderContainer();
      addTearDown(container.dispose);

      expect(await loadSavedThemeMode(api), isNull);
      expect(container.read(themeModeProvider), ThemeMode.system);
    });
  });
}
