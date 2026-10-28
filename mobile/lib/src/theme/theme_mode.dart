import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';

/// The app-wide theme mode.
///
/// The server preference is the source of truth. The app reads it once after
/// sign-in and the settings screen keeps it in step while the member changes
/// their theme.
final themeModeProvider =
    NotifierProvider<ThemeModeController, ThemeMode>(ThemeModeController.new);

class ThemeModeController extends Notifier<ThemeMode> {
  @override
  ThemeMode build() => ThemeMode.system;

  void apply(ThemeMode mode) => state = mode;

  void applyPreference(String? preference) =>
      state = themeModeFromPreference(preference);
}

/// Maps a stored `system`, `light` or `dark` preference to [ThemeMode].
/// Missing or unknown values fall back to following the device.
ThemeMode themeModeFromPreference(String? value) {
  switch (value?.trim().toLowerCase()) {
    case 'light':
      return ThemeMode.light;
    case 'dark':
      return ThemeMode.dark;
    default:
      return ThemeMode.system;
  }
}

/// Maps [ThemeMode] back to the value the settings API stores.
String themeModeToPreference(ThemeMode mode) {
  switch (mode) {
    case ThemeMode.light:
      return 'light';
    case ThemeMode.dark:
      return 'dark';
    case ThemeMode.system:
      return 'system';
  }
}

/// Reads the saved theme preference once.
///
/// Returns null when the settings cannot be loaded, so the caller keeps the
/// current mode. The app never waits on this before its first paint.
Future<ThemeMode?> loadSavedThemeMode(ApiClient api) async {
  try {
    final settings = await api.settings();
    return themeModeFromPreference(settings.prefs.theme);
  } catch (_) {
    return null;
  }
}
