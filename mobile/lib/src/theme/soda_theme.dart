import 'package:flutter/material.dart';

/// Sodapop's fresh lime palette: lime is a fill, never body ink.
abstract final class Soda {
  static const lime = Color(0xFFB4E32C);
  static const limePressed = Color(0xFF9FD11A);
  static const forest = Color(0xFF16241A);
  static const cream = Color(0xFFF2F4E6);
  static const paper = Color(0xFFFBFBF2);
  static const night = Color(0xFF0C130E);
  static const nightRaised = Color(0xFF151E17);
  static const tangerine = Color(0xFFE85D26);
  static const muted = Color(0xFF55613D);
  static const nightMuted = Color(0xFFA8B49A);
}

ThemeData sodaTheme(Brightness brightness) {
  final dark = brightness == Brightness.dark;
  final scheme = ColorScheme(
    brightness: brightness,
    primary: limeFor(brightness),
    onPrimary: dark ? Soda.night : Soda.forest,
    secondary: Soda.tangerine,
    onSecondary: Colors.white,
    error: dark ? const Color(0xFFFF7A6E) : const Color(0xFFB3261E),
    onError: Colors.white,
    surface: dark ? Soda.nightRaised : Soda.paper,
    onSurface: dark ? const Color(0xFFEEF2E0) : Soda.forest,
  );
  final base = ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: dark ? Soda.night : Soda.cream,
  );
  return base.copyWith(
    appBarTheme: AppBarTheme(
      backgroundColor: dark ? Soda.night : Soda.cream,
      foregroundColor: scheme.onSurface,
      elevation: 0,
      centerTitle: false,
      titleTextStyle: TextStyle(
        color: scheme.onSurface,
        fontSize: 22,
        fontWeight: FontWeight.w700,
        letterSpacing: -0.4,
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: scheme.primary,
        foregroundColor: scheme.onPrimary,
        minimumSize: const Size(44, 52),
        textStyle: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: dark ? Soda.nightRaised : Soda.paper,
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: BorderSide(color: dark ? const Color(0xFF31402F) : const Color(0xFFD8DCC2)),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: BorderSide(color: dark ? const Color(0xFF31402F) : const Color(0xFFD8DCC2)),
      ),
    ),
    cardTheme: CardThemeData(
      color: dark ? Soda.nightRaised : Soda.paper,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: BorderSide(color: dark ? const Color(0xFF31402F) : const Color(0xFFD8DCC2)),
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: dark ? Soda.nightRaised : Soda.paper,
      indicatorColor: scheme.primary.withValues(alpha: 0.35),
      labelTextStyle: WidgetStatePropertyAll(
        TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: scheme.onSurface),
      ),
    ),
  );
}

Color limeFor(Brightness brightness) =>
    brightness == Brightness.dark ? const Color(0xFFB9E63A) : Soda.lime;
