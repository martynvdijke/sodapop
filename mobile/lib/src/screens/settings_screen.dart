import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../push/push_service.dart';
import '../theme/soda_theme.dart';
import '../theme/theme_mode.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'legal_screen.dart';

/// Account settings: the private profile, preferences, security, browser
/// sessions and the people the member has blocked.
class SettingsScreen extends ConsumerStatefulWidget {
  const SettingsScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const SettingsScreen()));
  }

  @override
  ConsumerState<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends ConsumerState<SettingsScreen> {
  static final _clock = RegExp(r'^([01]\d|2[0-3]):[0-5]\d$');

  final _displayName = TextEditingController();
  final _bio = TextEditingController();
  final _bedtime = TextEditingController();

  AccountSettings? _settings;
  List<SettingsSession> _sessions = const [];
  List<BlockedUser> _blocked = const [];
  bool _loading = true;
  String? _error;

  bool _savingProfile = false;
  bool _savingAppearance = false;
  bool _savingNotifications = false;
  bool _savingPrivacy = false;
  bool _savingBedtime = false;
  bool _signingOutAll = false;
  bool _exporting = false;
  int? _busySession;
  String? _unblockingHandle;
  String? _bedtimeError;

  @override
  void initState() {
    super.initState();
    _displayName.addListener(_onProfileChanged);
    _bio.addListener(_onProfileChanged);
    _bedtime.addListener(_onBedtimeChanged);
    _load();
  }

  @override
  void dispose() {
    _displayName.dispose();
    _bio.dispose();
    _bedtime.dispose();
    super.dispose();
  }

  void _onProfileChanged() {
    if (mounted) setState(() {});
  }

  void _onBedtimeChanged() {
    if (mounted) setState(() => _bedtimeError = null);
  }

  bool get _profileDirty {
    final profile = _settings?.profile;
    if (profile == null) return false;
    return _displayName.text.trim() != profile.displayName.trim() ||
        _bio.text.trim() != profile.bio.trim();
  }

  Future<void> _load({bool showSpinner = true, bool reseed = true}) async {
    if (showSpinner) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final api = ref.read(apiProvider);
      final results = await Future.wait<Object>([
        api.settings(),
        api.listSessions(),
        api.blockedUsers(),
      ]);
      if (!mounted) return;
      final settings = results[0] as AccountSettings;
      ref
          .read(themeModeProvider.notifier)
          .applyPreference(settings.prefs.theme);
      if (reseed) {
        _displayName.text = settings.profile.displayName;
        _bio.text = settings.profile.bio;
        _bedtime.text = settings.prefs.caffeineBedtime;
      }
      setState(() {
        _settings = settings;
        _sessions = results[1] as List<SettingsSession>;
        _blocked = results[2] as List<BlockedUser>;
        _loading = false;
        _error = null;
        if (reseed) _bedtimeError = null;
      });
    } catch (error) {
      if (!mounted) return;
      final message = ApiClient.describe(error).message;
      if (!showSpinner && _settings != null) {
        setState(() => _loading = false);
        _notify(message);
        return;
      }
      setState(() {
        _loading = false;
        _error = message;
      });
    }
  }

  void _notify(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  /// Applies a partial settings update. The previous values stay on screen
  /// until the server confirms the change, so a failed call rolls back by
  /// simply not touching local state.
  Future<bool> _apply(
    Future<AccountSettings> Function(ApiClient api) run,
  ) async {
    try {
      final updated = await run(ref.read(apiProvider));
      if (!mounted) return false;
      setState(() => _settings = updated);
      return true;
    } catch (error) {
      if (!mounted) return false;
      _notify(ApiClient.describe(error).message);
      return false;
    }
  }

  Future<void> _saveProfile() async {
    final settings = _settings;
    if (settings == null || _savingProfile) return;
    final name = _displayName.text.trim();
    final bio = _bio.text.trim();
    final nameChanged = name != settings.profile.displayName.trim();
    final bioChanged = bio != settings.profile.bio.trim();
    if (!nameChanged && !bioChanged) return;

    setState(() => _savingProfile = true);
    final saved = await _apply(
      (api) => api.updateSettings(
        displayName: nameChanged ? name : null,
        bio: bioChanged ? bio : null,
      ),
    );
    if (!mounted) return;
    if (saved) {
      _displayName.text = _settings?.profile.displayName ?? name;
      _bio.text = _settings?.profile.bio ?? bio;
      FocusManager.instance.primaryFocus?.unfocus();
    }
    setState(() => _savingProfile = false);
    if (saved) _notify('Profile saved.');
  }

  Future<void> _saveTheme(String value) async {
    if (_savingAppearance || _settings?.prefs.theme == value) return;
    setState(() => _savingAppearance = true);
    final saved = await _apply((api) => api.updateSettings(theme: value));
    if (!mounted) return;
    if (saved) {
      ref.read(themeModeProvider.notifier).applyPreference(value);
    }
    setState(() => _savingAppearance = false);
  }

  Future<void> _saveUnits(String value) async {
    if (_savingAppearance || _settings?.prefs.units == value) return;
    setState(() => _savingAppearance = true);
    await _apply((api) => api.updateSettings(units: value));
    if (mounted) setState(() => _savingAppearance = false);
  }

  Future<void> _saveNotifications({
    bool? emailWeekly,
    bool? emailFriendReq,
    bool? pushEnabled,
  }) async {
    if (_savingNotifications) return;
    setState(() => _savingNotifications = true);
    await _apply(
      (api) => api.updateSettings(
        emailWeekly: emailWeekly,
        emailFriendReq: emailFriendReq,
        pushEnabled: pushEnabled,
      ),
    );
    if (mounted) setState(() => _savingNotifications = false);
  }

  /// Push is an opt-in, so the Android prompt follows this switch instead of
  /// sign-in. A refusal leaves the switch off rather than registering a device
  /// that can never be notified.
  Future<void> _togglePush(bool value) async {
    if (value) {
      final granted = await PushService.instance.requestPermission();
      if (!granted) {
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text(
              'Android is blocking notifications for Sodapop. Allow them in '
              'system settings, then try again.',
            ),
          ),
        );
        return;
      }
    }
    await _saveNotifications(pushEnabled: value);
  }

  Future<void> _savePrivacy({
    bool? publicProfile,
    bool? analyticsOptOut,
    String? defaultVisibility,
  }) async {
    if (_savingPrivacy) return;
    setState(() => _savingPrivacy = true);
    await _apply(
      (api) => api.updateSettings(
        publicProfile: publicProfile,
        analyticsOptOut: analyticsOptOut,
        defaultVisibility: defaultVisibility,
      ),
    );
    if (mounted) setState(() => _savingPrivacy = false);
  }

  Future<void> _saveBedtime() async {
    if (_savingBedtime) return;
    final value = _bedtime.text.trim();
    if (value.isNotEmpty && !_clock.hasMatch(value)) {
      setState(() => _bedtimeError = 'Use 24-hour time, like 21:30.');
      return;
    }

    setState(() {
      _savingBedtime = true;
      _bedtimeError = null;
    });
    final saved = await _apply(
      (api) => api.updateSettings(caffeineBedtime: value),
    );
    if (!mounted) return;
    if (saved) {
      _bedtime.text = _settings?.prefs.caffeineBedtime ?? value;
      FocusManager.instance.primaryFocus?.unfocus();
    }
    setState(() => _savingBedtime = false);
    if (saved) _notify(value.isEmpty ? 'Bedtime cleared.' : 'Bedtime saved.');
  }

  Future<void> _changePassword() async {
    final changed = await showDialog<bool>(
      context: context,
      builder: (_) => _PasswordDialog(api: ref.read(apiProvider)),
    );
    if (changed == true) _notify('Password updated.');
  }

  Future<bool> _confirm({
    required String title,
    required String message,
    required String action,
  }) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text(title),
        content: Text(message),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text(action),
          ),
        ],
      ),
    );
    return confirmed == true && mounted;
  }

  Future<bool> _reloadSessions() async {
    try {
      final rows = await ref.read(apiProvider).listSessions();
      if (!mounted) return false;
      setState(() => _sessions = rows);
      return true;
    } catch (error) {
      if (!mounted) return false;
      _notify(ApiClient.describe(error).message);
      return false;
    }
  }

  Future<void> _revokeSession(SettingsSession session) async {
    if (_busySession != null || _signingOutAll) return;
    final confirmed = await _confirm(
      title: 'Revoke this session?',
      message: session.current
          ? 'This browser session ends now. You may need to sign in again.'
          : 'That browser has to sign in again.',
      action: 'Revoke',
    );
    if (!confirmed) return;

    setState(() => _busySession = session.id);
    try {
      await ref.read(apiProvider).revokeSession(session.id);
      if (!mounted) return;
      if (await _reloadSessions()) _notify('Session revoked.');
    } catch (error) {
      if (!mounted) return;
      _notify(ApiClient.describe(error).message);
    } finally {
      if (mounted) setState(() => _busySession = null);
    }
  }

  Future<void> _signOutOtherSessions() async {
    if (_signingOutAll || _busySession != null) return;
    final confirmed = await _confirm(
      title: 'Sign out other sessions?',
      message:
          'Every browser session ends, including any open on this device. The personal token on this phone keeps working.',
      action: 'Sign out',
    );
    if (!confirmed) return;

    setState(() => _signingOutAll = true);
    try {
      await ref.read(apiProvider).revokeAllSessions();
      if (!mounted) return;
      if (await _reloadSessions()) _notify('All browser sessions ended.');
    } catch (error) {
      if (!mounted) return;
      _notify(ApiClient.describe(error).message);
    } finally {
      if (mounted) setState(() => _signingOutAll = false);
    }
  }

  Future<bool> _reloadBlocked() async {
    try {
      final rows = await ref.read(apiProvider).blockedUsers();
      if (!mounted) return false;
      setState(() => _blocked = rows);
      return true;
    } catch (error) {
      if (!mounted) return false;
      _notify(ApiClient.describe(error).message);
      return false;
    }
  }

  Future<void> _unblock(BlockedUser user) async {
    if (_unblockingHandle != null) return;
    final confirmed = await _confirm(
      title: 'Unblock @${user.handle}?',
      message:
          'Their check-ins can appear in your feed again, and they can follow you.',
      action: 'Unblock',
    );
    if (!confirmed) return;

    setState(() => _unblockingHandle = user.handle);
    try {
      await ref.read(apiProvider).unblockUser(user.handle);
      if (!mounted) return;
      if (await _reloadBlocked()) _notify('@${user.handle} unblocked.');
    } catch (error) {
      if (!mounted) return;
      _notify(ApiClient.describe(error).message);
    } finally {
      if (mounted) setState(() => _unblockingHandle = null);
    }
  }

  Future<void> _exportAccount() async {
    if (_exporting) return;
    setState(() => _exporting = true);
    try {
      final document = await ref.read(apiProvider).exportAccount();
      if (!mounted) return;
      setState(() => _exporting = false);
      await _showExport(document);
    } catch (error) {
      if (!mounted) return;
      setState(() => _exporting = false);
      await _showExportError(ApiClient.describe(error).message);
    }
  }

  Future<void> _showExport(String document) async {
    final theme = Theme.of(context);
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: theme.colorScheme.surface,
      clipBehavior: Clip.antiAlias,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (_) => _ExportSheet(
        document: document,
        size: _formatBytes(utf8.encode(document).length),
      ),
    );
  }

  Future<void> _showExportError(String message) async {
    final retry = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Download failed'),
        content: Text(message),
        actions: [
          TextButton(
            key: const Key('export-error-close'),
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Close'),
          ),
          FilledButton(
            key: const Key('export-error-retry'),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Try again'),
          ),
        ],
      ),
    );
    if (retry == true && mounted) await _exportAccount();
  }

  Future<void> _closeAccount() async {
    final profile = _settings?.profile;
    if (profile == null || profile.handle.isEmpty) return;
    final closed = await showDialog<bool>(
      context: context,
      builder: (_) => _CloseAccountDialog(
        api: ref.read(apiProvider),
        handle: profile.handle,
      ),
    );
    if (closed != true || !mounted) return;
    await ref.read(authProvider.notifier).signOut();
    if (!mounted) return;
    Navigator.of(context).popUntil((route) => route.isFirst);
  }

  static String _formatBytes(int bytes) {
    if (bytes < 1024) return '$bytes B';
    final kb = bytes / 1024;
    if (kb < 1024) return '${kb.toStringAsFixed(kb >= 100 ? 0 : 1)} KB';
    return '${(kb / 1024).toStringAsFixed(1)} MB';
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Settings'),
        actions: [
          IconButton(
            onPressed: _loading
                ? null
                : () => _load(showSpinner: false, reseed: false),
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: SafeArea(
        top: false,
        child: RefreshIndicator(
          color: Soda.lime,
          onRefresh: () => _load(showSpinner: false, reseed: false),
          child: _body(),
        ),
      ),
    );
  }

  Widget _body() {
    final settings = _settings;
    if (settings == null) {
      if (_loading) {
        return const SodaLoadingView(label: 'Loading settings…');
      }
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Settings did not load',
        message: _error ?? 'Try again in a moment.',
        actionLabel: 'Try again',
        onAction: _load,
      );
    }

    final theme = Theme.of(context);
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
      children: [
        _profileSection(theme, settings.profile),
        const SizedBox(height: 24),
        _appearanceSection(theme, settings.prefs),
        const SizedBox(height: 24),
        _notificationsSection(theme, settings.prefs),
        const SizedBox(height: 24),
        _privacySection(theme, settings.prefs),
        const SizedBox(height: 24),
        _caffeineSection(theme, settings.prefs),
        const SizedBox(height: 24),
        _securitySection(theme),
        const SizedBox(height: 24),
        _sessionsSection(theme),
        const SizedBox(height: 24),
        _blockedSection(theme),
        const SizedBox(height: 24),
        _accountSection(theme, settings),
      ],
    );
  }

  // ---------------------------------------------------------------- sections

  Widget _profileSection(ThemeData theme, SettingsProfile profile) {
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Profile'),
        const SizedBox(height: 10),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                _identityBlock(theme, profile),
                const SizedBox(height: 8),
                Text(
                  'Handle and email identify the account and cannot be edited here.',
                  style: hint,
                ),
                const SizedBox(height: 16),
                TextField(
                  key: const Key('settings-display-name'),
                  controller: _displayName,
                  maxLength: 80,
                  enabled: !_savingProfile,
                  textCapitalization: TextCapitalization.words,
                  textInputAction: TextInputAction.next,
                  decoration: const InputDecoration(
                    labelText: 'Display name',
                    hintText: 'Your name',
                  ),
                ),
                const SizedBox(height: 12),
                TextField(
                  key: const Key('settings-bio'),
                  controller: _bio,
                  maxLength: 500,
                  minLines: 3,
                  maxLines: 5,
                  enabled: !_savingProfile,
                  textCapitalization: TextCapitalization.sentences,
                  decoration: const InputDecoration(
                    labelText: 'Bio',
                    hintText: 'What do you look for in a soda?',
                    alignLabelWithHint: true,
                  ),
                ),
                const SizedBox(height: 14),
                FilledButton(
                  key: const Key('settings-save-profile'),
                  onPressed: _profileDirty && !_savingProfile
                      ? _saveProfile
                      : null,
                  child: Text(_savingProfile ? 'Saving…' : 'Save profile'),
                ),
                _busyBar(_savingProfile),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _identityBlock(ThemeData theme, SettingsProfile profile) {
    final strong = theme.textTheme.bodyMedium?.copyWith(
      fontWeight: FontWeight.w700,
    );
    final iconColor = theme.colorScheme.onSurfaceVariant;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.alternate_email_rounded, size: 18, color: iconColor),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  '@${profile.handle}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: strong,
                ),
              ),
              const SizedBox(width: 8),
              _Badge(label: _roleLabel(profile.role)),
            ],
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              Icon(Icons.mail_outline_rounded, size: 18, color: iconColor),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  profile.email.isEmpty ? 'No email on file' : profile.email,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodyMedium,
                ),
              ),
              const SizedBox(width: 8),
              _Badge(
                label: profile.emailVerified ? 'Verified' : 'Unverified',
                tone: profile.emailVerified ? _BadgeTone.good : _BadgeTone.warn,
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _appearanceSection(ThemeData theme, SettingsPrefs prefs) {
    final label = theme.textTheme.labelLarge?.copyWith(
      fontWeight: FontWeight.w700,
    );
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );
    final themeValue = const ['system', 'light', 'dark'].contains(prefs.theme)
        ? prefs.theme
        : 'system';
    final unitsValue = const ['metric', 'imperial'].contains(prefs.units)
        ? prefs.units
        : 'metric';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Appearance'),
        const SizedBox(height: 10),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text('Theme', style: label),
                const SizedBox(height: 8),
                AbsorbPointer(
                  absorbing: _savingAppearance,
                  child: SodaSegmented<String>(
                    options: const [
                      ('system', 'System'),
                      ('light', 'Light'),
                      ('dark', 'Dark'),
                    ],
                    selected: themeValue,
                    onChanged: _saveTheme,
                  ),
                ),
                const SizedBox(height: 6),
                Text('System follows your phone.', style: hint),
                const SizedBox(height: 18),
                Text('Units', style: label),
                const SizedBox(height: 8),
                AbsorbPointer(
                  absorbing: _savingAppearance,
                  child: SodaSegmented<String>(
                    options: const [
                      ('metric', 'Metric'),
                      ('imperial', 'Imperial'),
                    ],
                    selected: unitsValue,
                    onChanged: _saveUnits,
                  ),
                ),
                const SizedBox(height: 6),
                Text('Used for bottle and can sizes.', style: hint),
                _busyBar(_savingAppearance),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _notificationsSection(ThemeData theme, SettingsPrefs prefs) {
    final busy = _savingNotifications;
    final pushConfigured = PushService.instance.enabled;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Notifications'),
        const SizedBox(height: 10),
        Card(
          child: Column(
            children: [
              _switchRow(
                key: const Key('settings-email-weekly'),
                icon: Icons.mark_email_read_outlined,
                title: 'Weekly email digest',
                subtitle: "One summary of friends' check-ins each week.",
                value: prefs.emailWeekly,
                onChanged: busy
                    ? null
                    : (value) => _saveNotifications(emailWeekly: value),
              ),
              const Divider(height: 1, indent: 16, endIndent: 16),
              _switchRow(
                key: const Key('settings-email-friend-request'),
                icon: Icons.person_add_alt_1_outlined,
                title: 'Friend request emails',
                subtitle: 'An email when someone asks to be friends.',
                value: prefs.emailFriendReq,
                onChanged: busy
                    ? null
                    : (value) => _saveNotifications(emailFriendReq: value),
              ),
              const Divider(height: 1, indent: 16, endIndent: 16),
              _switchRow(
                key: const Key('settings-push'),
                icon: Icons.notifications_active_outlined,
                title: 'Push notifications',
                subtitle: pushConfigured
                    ? 'Toasts, friend requests and badges on this phone.'
                    : 'Push is not configured for this build.',
                value: pushConfigured && prefs.pushEnabled,
                onChanged: busy || !pushConfigured
                    ? null
                    : (value) => _togglePush(value),
              ),
              if (busy)
                Padding(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 14),
                  child: _busyBar(true),
                ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _privacySection(ThemeData theme, SettingsPrefs prefs) {
    final busy = _savingPrivacy;
    final label = theme.textTheme.labelLarge?.copyWith(
      fontWeight: FontWeight.w700,
    );
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );
    final visibility =
        const ['private', 'friends', 'public'].contains(prefs.defaultVisibility)
        ? prefs.defaultVisibility
        : 'public';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Privacy'),
        const SizedBox(height: 10),
        Card(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _switchRow(
                key: const Key('settings-public-profile'),
                icon: Icons.public_rounded,
                title: 'Public profile',
                subtitle:
                    'Anyone with your link sees your handle, bio and public check-ins.',
                value: prefs.publicProfile,
                onChanged: busy
                    ? null
                    : (value) => _savePrivacy(publicProfile: value),
              ),
              const Divider(height: 1, indent: 16, endIndent: 16),
              _switchRow(
                key: const Key('settings-analytics-opt-out'),
                icon: Icons.analytics_outlined,
                title: 'Opt out of analytics',
                subtitle: 'Your visits are left out of site analytics.',
                value: prefs.analyticsOptOut,
                onChanged: busy
                    ? null
                    : (value) => _savePrivacy(analyticsOptOut: value),
              ),
              const Divider(height: 1, indent: 16, endIndent: 16),
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 14, 16, 16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text('New check-ins', style: label),
                    const SizedBox(height: 8),
                    AbsorbPointer(
                      absorbing: busy,
                      child: SodaSegmented<String>(
                        options: const [
                          ('private', 'Private'),
                          ('friends', 'Friends'),
                          ('public', 'Public'),
                        ],
                        selected: visibility,
                        onChanged: (value) =>
                            _savePrivacy(defaultVisibility: value),
                      ),
                    ),
                    const SizedBox(height: 6),
                    Text('You can change this on each check-in.', style: hint),
                    _busyBar(busy),
                  ],
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _caffeineSection(ThemeData theme, SettingsPrefs prefs) {
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Caffeine'),
        const SizedBox(height: 10),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                TextField(
                  key: const Key('settings-bedtime'),
                  controller: _bedtime,
                  keyboardType: TextInputType.datetime,
                  textInputAction: TextInputAction.done,
                  maxLength: 5,
                  enabled: !_savingBedtime,
                  inputFormatters: [
                    FilteringTextInputFormatter.allow(RegExp(r'[0-9:]')),
                  ],
                  onSubmitted: (_) => _saveBedtime(),
                  decoration: InputDecoration(
                    labelText: 'Bedtime',
                    hintText: '21:30',
                    counterText: '',
                    helperText:
                        'Used to show caffeine against the time you sleep.',
                    errorText: _bedtimeError,
                    suffixIcon: _bedtime.text.isEmpty
                        ? null
                        : IconButton(
                            onPressed: _savingBedtime
                                ? null
                                : () {
                                    _bedtime.clear();
                                    setState(() => _bedtimeError = null);
                                  },
                            tooltip: 'Clear bedtime',
                            icon: const Icon(Icons.close_rounded),
                          ),
                  ),
                ),
                const SizedBox(height: 6),
                Text('24-hour time, for example 21:30.', style: hint),
                const SizedBox(height: 14),
                FilledButton(
                  key: const Key('settings-save-bedtime'),
                  onPressed: _savingBedtime ? null : _saveBedtime,
                  child: Text(_savingBedtime ? 'Saving…' : 'Save bedtime'),
                ),
                _busyBar(_savingBedtime),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _securitySection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Security'),
        const SizedBox(height: 10),
        Card(
          child: ListTile(
            key: const Key('settings-change-password'),
            contentPadding: const EdgeInsets.fromLTRB(16, 6, 12, 6),
            leading: Icon(
              Icons.lock_outline_rounded,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            title: const Text('Change password'),
            subtitle: const Text('At least 10 characters.'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: _changePassword,
          ),
        ),
      ],
    );
  }

  Widget _sessionsSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaSectionTitle(
          title: 'Sessions',
          trailing: _sessions.isEmpty
              ? null
              : sodaCount(_sessions.length, 'browser'),
        ),
        const SizedBox(height: 10),
        Card(
          child: Column(
            children: [
              if (_sessions.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(horizontal: 8),
                  child: SodaStatusView(
                    dense: true,
                    icon: Icons.devices_other_rounded,
                    title: 'No browser sessions',
                    message: 'Sessions appear when you sign in on the web.',
                  ),
                )
              else
                for (var index = 0; index < _sessions.length; index++) ...[
                  if (index > 0)
                    const Divider(height: 1, indent: 16, endIndent: 16),
                  _sessionRow(theme, _sessions[index]),
                ],
              if (_sessions.isNotEmpty) ...[
                const Divider(height: 1, indent: 16, endIndent: 16),
                Padding(
                  padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
                  child: SizedBox(
                    width: double.infinity,
                    child: TextButton.icon(
                      key: const Key('settings-sign-out-all'),
                      onPressed: _signingOutAll ? null : _signOutOtherSessions,
                      icon: _signingOutAll
                          ? const SizedBox(
                              width: 16,
                              height: 16,
                              child: CircularProgressIndicator(
                                strokeWidth: 2,
                                color: Soda.lime,
                              ),
                            )
                          : const Icon(Icons.logout_rounded),
                      label: const Text('Sign out other sessions'),
                    ),
                  ),
                ),
              ],
            ],
          ),
        ),
      ],
    );
  }

  Widget _sessionRow(ThemeData theme, SettingsSession session) {
    final busy = _busySession == session.id;
    final parts = <String>[];
    if (session.ip.isNotEmpty) parts.add(session.ip);
    if (session.userAgent.isNotEmpty) parts.add(session.userAgent);
    final when = <String>[];
    if (session.createdAt.isNotEmpty) {
      when.add('Signed in ${sodaDateTime(session.createdAt)}');
    }
    if (session.lastSeenAt.isNotEmpty) {
      when.add('Last seen ${sodaDateTime(session.lastSeenAt)}');
    }
    final metaStyle = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );

    return ListTile(
      contentPadding: const EdgeInsets.fromLTRB(16, 6, 8, 6),
      title: Row(
        children: [
          Expanded(
            child: Text(
              _deviceName(session.userAgent),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.titleSmall?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
          ),
          if (session.current) ...[
            const SizedBox(width: 8),
            const _Badge(label: 'This device', tone: _BadgeTone.good),
          ],
        ],
      ),
      subtitle: Padding(
        padding: const EdgeInsets.only(top: 4),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (parts.isNotEmpty)
              Text(
                parts.join(' · '),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: metaStyle,
              ),
            if (when.isNotEmpty)
              Text(
                when.join(' · '),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: metaStyle,
              ),
          ],
        ),
      ),
      trailing: TextButton(
        onPressed: _busySession == null && !_signingOutAll
            ? () => _revokeSession(session)
            : null,
        child: busy
            ? const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: Soda.lime,
                ),
              )
            : const Text('Revoke'),
      ),
    );
  }

  Widget _blockedSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaSectionTitle(
          title: 'Blocked people',
          trailing: _blocked.isEmpty
              ? null
              : sodaCount(_blocked.length, 'account'),
        ),
        const SizedBox(height: 10),
        Card(
          child: Column(
            children: [
              if (_blocked.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(horizontal: 8),
                  child: SodaStatusView(
                    dense: true,
                    icon: Icons.block_rounded,
                    title: 'No blocked accounts',
                    message:
                        'People you block stay out of your feed and cannot follow you.',
                  ),
                )
              else
                for (var index = 0; index < _blocked.length; index++) ...[
                  if (index > 0)
                    const Divider(height: 1, indent: 16, endIndent: 16),
                  _blockedRow(theme, _blocked[index]),
                ],
            ],
          ),
        ),
      ],
    );
  }

  Widget _blockedRow(ThemeData theme, BlockedUser user) {
    final busy = _unblockingHandle == user.handle;
    final name = user.displayName.trim().isEmpty
        ? user.handle
        : user.displayName;
    return ListTile(
      contentPadding: const EdgeInsets.fromLTRB(16, 4, 8, 4),
      leading: CircleAvatar(
        radius: 18,
        backgroundColor: theme.colorScheme.primary.withValues(alpha: 0.3),
        child: Text(
          name.isEmpty ? '?' : name.characters.first.toUpperCase(),
          style: TextStyle(
            color: theme.colorScheme.onSurface,
            fontWeight: FontWeight.w800,
          ),
        ),
      ),
      title: Text(
        name,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: theme.textTheme.titleSmall?.copyWith(
          fontWeight: FontWeight.w700,
        ),
      ),
      subtitle: Text('@${user.handle}'),
      trailing: TextButton(
        key: Key('unblock-${user.handle}'),
        onPressed: _unblockingHandle == null ? () => _unblock(user) : null,
        child: busy
            ? const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: Soda.lime,
                ),
              )
            : const Text('Unblock'),
      ),
    );
  }

  Widget _accountSection(ThemeData theme, AccountSettings settings) {
    final count = theme.textTheme.titleMedium?.copyWith(
      fontWeight: FontWeight.w800,
      fontFeatures: const [FontFeature.tabularFigures()],
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Account'),
        const SizedBox(height: 10),
        Card(
          child: Column(
            children: [
              ListTile(
                leading: const Icon(Icons.key_outlined),
                title: const Text('Personal API tokens'),
                subtitle: const Text('Used by the Android app and scripts.'),
                trailing: Text('${settings.tokenCount}', style: count),
              ),
              const Divider(height: 1),
              ListTile(
                leading: const Icon(Icons.block_outlined),
                title: const Text('Blocked people'),
                subtitle: const Text('Accounts you have blocked.'),
                trailing: Text('${_blocked.length}', style: count),
              ),
              const Divider(height: 1),
              ListTile(
                key: const Key('settings-export'),
                contentPadding: const EdgeInsets.fromLTRB(16, 4, 12, 4),
                leading: Icon(
                  Icons.download_outlined,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
                title: const Text('Download my data'),
                subtitle: const Text(
                  'A JSON copy of your profile, check-ins, lists and settings.',
                ),
                trailing: _exporting
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Soda.lime,
                        ),
                      )
                    : const Icon(Icons.chevron_right_rounded),
                onTap: _exporting ? null : _exportAccount,
              ),
              const Divider(height: 1),
              ListTile(
                key: const Key('settings-close-account'),
                contentPadding: const EdgeInsets.fromLTRB(16, 4, 12, 4),
                leading: Icon(
                  Icons.person_off_outlined,
                  color: theme.colorScheme.error,
                ),
                title: Text(
                  'Close account',
                  style: TextStyle(
                    color: theme.colorScheme.error,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                subtitle: const Text(
                  'Erase your profile and sign out everywhere.',
                ),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _closeAccount,
              ),
            ],
          ),
        ),
        const SizedBox(height: 22),
        const SodaSectionTitle(title: 'About'),
        const SizedBox(height: 10),
        Card(
          child: Column(
            children: [
              _legalTile(
                key: const Key('settings-privacy'),
                title: 'Privacy policy',
                subtitle: 'What this server stores and why.',
                path: '/privacy',
              ),
              const Divider(height: 1),
              _legalTile(
                key: const Key('settings-terms'),
                title: 'Terms of use',
                subtitle: 'The rules for using this server.',
                path: '/terms',
              ),
            ],
          ),
        ),
      ],
    );
  }

  /// Opens the server's own policy page, so the text always matches the
  /// instance the account actually lives on.
  Widget _legalTile({
    required Key key,
    required String title,
    required String subtitle,
    required String path,
  }) {
    final server = ref.read(apiProvider).server;
    return ListTile(
      key: key,
      leading: const Icon(Icons.gavel_outlined),
      title: Text(title),
      subtitle: Text(subtitle),
      trailing: const Icon(Icons.open_in_new_rounded, size: 18),
      enabled: server != null,
      onTap: server == null
          ? null
          : () => LegalScreen.open(context, title: title, url: '$server$path'),
    );
  }

  // ------------------------------------------------------------------ pieces

  Widget _switchRow({
    required Key key,
    required IconData icon,
    required String title,
    required String subtitle,
    required bool value,
    required ValueChanged<bool>? onChanged,
  }) {
    final theme = Theme.of(context);
    return SwitchListTile(
      key: key,
      contentPadding: const EdgeInsets.fromLTRB(16, 2, 8, 2),
      secondary: Icon(icon, color: theme.colorScheme.onSurfaceVariant),
      title: Text(
        title,
        style: theme.textTheme.bodyLarge?.copyWith(fontWeight: FontWeight.w600),
      ),
      subtitle: Text(subtitle),
      value: value,
      onChanged: onChanged,
    );
  }

  Widget _busyBar(bool busy) {
    if (!busy) return const SizedBox.shrink();
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(2),
        child: LinearProgressIndicator(
          minHeight: 3,
          color: scheme.primary,
          backgroundColor: scheme.primary.withValues(alpha: 0.15),
        ),
      ),
    );
  }

  static String _roleLabel(String role) {
    return role.toLowerCase() == 'admin' ? 'Admin' : 'Member';
  }

  static String _deviceName(String agent) {
    final value = agent.toLowerCase();
    if (value.contains('iphone')) return 'iPhone';
    if (value.contains('ipad')) return 'iPad';
    if (value.contains('android')) return 'Android device';
    if (value.contains('windows')) return 'Windows browser';
    if (value.contains('macintosh') || value.contains('mac os')) {
      return 'Mac browser';
    }
    if (value.contains('linux')) return 'Linux browser';
    final trimmed = agent.trim();
    return trimmed.isEmpty ? 'Unknown device' : 'Browser session';
  }
}

enum _BadgeTone { good, warn, quiet }

class _Badge extends StatelessWidget {
  const _Badge({required this.label, this.tone = _BadgeTone.quiet});

  final String label;
  final _BadgeTone tone;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final (background, foreground) = switch (tone) {
      _BadgeTone.good => (
        scheme.primary.withValues(alpha: 0.32),
        scheme.onSurface,
      ),
      _BadgeTone.warn => (
        Soda.tangerine.withValues(alpha: 0.16),
        Soda.tangerine,
      ),
      _BadgeTone.quiet => (
        scheme.onSurface.withValues(alpha: 0.06),
        scheme.onSurfaceVariant,
      ),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelSmall?.copyWith(
          fontWeight: FontWeight.w700,
          color: foreground,
        ),
      ),
    );
  }
}

/// Change-password form. Pop with `true` after the server accepts the change.
class _PasswordDialog extends StatefulWidget {
  const _PasswordDialog({required this.api});

  final ApiClient api;

  @override
  State<_PasswordDialog> createState() => _PasswordDialogState();
}

class _PasswordDialogState extends State<_PasswordDialog> {
  final _current = TextEditingController();
  final _next = TextEditingController();
  final _confirm = TextEditingController();
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _current.dispose();
    _next.dispose();
    _confirm.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_saving) return;
    final current = _current.text;
    final next = _next.text;
    final confirm = _confirm.text;
    if (current.isEmpty) {
      setState(() => _error = 'Enter your current password.');
      return;
    }
    if (next.length < 10) {
      setState(() => _error = 'Use at least 10 characters.');
      return;
    }
    if (next != confirm) {
      setState(() => _error = 'The new passwords do not match.');
      return;
    }

    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.api.changePassword(current, next);
      if (!mounted) return;
      _current.clear();
      _next.clear();
      _confirm.clear();
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = _passwordMessage(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: const Text('Change password'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextField(
              key: const Key('password-current'),
              controller: _current,
              obscureText: true,
              enabled: !_saving,
              autofillHints: const [AutofillHints.password],
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(labelText: 'Current password'),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('password-new'),
              controller: _next,
              obscureText: true,
              enabled: !_saving,
              autofillHints: const [AutofillHints.newPassword],
              textInputAction: TextInputAction.next,
              decoration: const InputDecoration(
                labelText: 'New password',
                helperText: 'At least 10 characters.',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              key: const Key('password-confirm'),
              controller: _confirm,
              obscureText: true,
              enabled: !_saving,
              autofillHints: const [AutofillHints.newPassword],
              textInputAction: TextInputAction.done,
              onSubmitted: (_) => _submit(),
              decoration: const InputDecoration(
                labelText: 'Confirm new password',
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 14),
              Text(
                _error!,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.error,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.pop(context, false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: _saving ? null : _submit,
          child: Text(_saving ? 'Saving…' : 'Update password'),
        ),
      ],
    );
  }
}

String _passwordMessage(Object error) {
  final message = ApiClient.describe(error).message;
  final text = message.toLowerCase();
  if (text.contains('invalid_password') ||
      text.contains('current password is incorrect')) {
    return 'That current password is not right.';
  }
  if (text.contains('weak_password') || text.contains('at least 10')) {
    return 'Use at least 10 characters.';
  }
  if (text.contains('no_password') ||
      text.contains('does not use a password')) {
    return 'This account signs in with a provider, not a password.';
  }
  return message;
}

/// The export confirmation sheet. Copying keeps the sheet open and swaps the
/// button label, so the member sees the copy landed before closing.
class _ExportSheet extends StatefulWidget {
  const _ExportSheet({required this.document, required this.size});

  final String document;
  final String size;

  @override
  State<_ExportSheet> createState() => _ExportSheetState();
}

class _ExportSheetState extends State<_ExportSheet> {
  bool _copied = false;

  Future<void> _copy() async {
    await Clipboard.setData(ClipboardData(text: widget.document));
    if (!mounted) return;
    setState(() => _copied = true);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return SingleChildScrollView(
      padding: const EdgeInsets.fromLTRB(20, 12, 20, 20),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Center(
            child: Container(
              width: 36,
              height: 4,
              decoration: BoxDecoration(
                color: scheme.onSurface.withValues(alpha: 0.18),
                borderRadius: BorderRadius.circular(999),
              ),
            ),
          ),
          const SizedBox(height: 18),
          Row(
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: scheme.primary.withValues(alpha: 0.32),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(
                  Icons.download_done_rounded,
                  color: scheme.onSurface,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  'Your data export',
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            'A JSON document with your profile, check-ins, lists and settings. Copy it and keep it somewhere safe.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: scheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 16),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            decoration: BoxDecoration(
              color: scheme.onSurface.withValues(alpha: 0.04),
              borderRadius: BorderRadius.circular(12),
            ),
            child: Row(
              children: [
                Icon(
                  Icons.description_outlined,
                  size: 18,
                  color: scheme.onSurfaceVariant,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    'sodapop-export.json',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                Text(
                  widget.size,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 18),
          FilledButton.icon(
            key: const Key('export-copy'),
            onPressed: _copy,
            icon: Icon(_copied ? Icons.check_rounded : Icons.copy_rounded),
            label: Text(_copied ? 'Copied' : 'Copy JSON'),
          ),
          const SizedBox(height: 8),
          TextButton(
            key: const Key('export-close'),
            onPressed: () => Navigator.pop(context),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }
}

/// Two-step account closure: an explanation list and an exact-handle check
/// before the destructive call. The dialog stays open after a failure and
/// keeps whatever the member typed.
class _CloseAccountDialog extends StatefulWidget {
  const _CloseAccountDialog({required this.api, required this.handle});

  final ApiClient api;
  final String handle;

  @override
  State<_CloseAccountDialog> createState() => _CloseAccountDialogState();
}

class _CloseAccountDialogState extends State<_CloseAccountDialog> {
  static const _effects = [
    'Your email, handle, name, bio, avatar and password are erased.',
    'Every session and API token is revoked, including this phone.',
    'Your check-ins and comments stay, shown as a deleted user.',
    'Your handle becomes available again.',
    'This cannot be undone.',
  ];

  final _confirm = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _confirm.dispose();
    super.dispose();
  }

  bool get _matches {
    final text = _confirm.text.trim();
    return text == widget.handle || text == '@${widget.handle}';
  }

  Future<void> _submit() async {
    if (_busy || !_matches) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.api.deleteAccount(widget.handle);
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = _closeMessage(error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: scheme.onSurfaceVariant,
    );
    return AlertDialog(
      title: const Text('Close your account?'),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Closing the account changes these things:', style: hint),
            const SizedBox(height: 12),
            for (final effect in _effects)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Padding(
                      padding: const EdgeInsets.only(top: 7),
                      child: Container(
                        width: 6,
                        height: 6,
                        decoration: BoxDecoration(
                          color: scheme.error.withValues(alpha: 0.7),
                          shape: BoxShape.circle,
                        ),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(effect, style: theme.textTheme.bodyMedium),
                    ),
                  ],
                ),
              ),
            const SizedBox(height: 10),
            Text(
              'Type @${widget.handle} to confirm.',
              style: theme.textTheme.bodyMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 8),
            TextField(
              key: const Key('close-account-confirm'),
              controller: _confirm,
              enabled: !_busy,
              autofocus: true,
              autocorrect: false,
              enableSuggestions: false,
              textCapitalization: TextCapitalization.none,
              textInputAction: TextInputAction.done,
              onChanged: (_) => setState(() => _error = null),
              onSubmitted: (_) => _submit(),
              decoration: InputDecoration(
                labelText: 'Handle',
                hintText: widget.handle,
                errorText: _error,
              ),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          key: const Key('close-account-cancel'),
          onPressed: _busy ? null : () => Navigator.pop(context, false),
          child: const Text('Keep account'),
        ),
        FilledButton(
          key: const Key('close-account-submit'),
          style: FilledButton.styleFrom(
            backgroundColor: scheme.error,
            foregroundColor: scheme.onError,
          ),
          onPressed: _matches && !_busy ? _submit : null,
          child: Text(_busy ? 'Closing…' : 'Close account'),
        ),
      ],
    );
  }
}

String _closeMessage(Object error) {
  final described = ApiClient.describe(error);
  final text = described.message.toLowerCase();
  if (described.statusCode == 409 ||
      text.contains('last_admin') ||
      text.contains('only admin')) {
    return 'This is the only admin account. Promote another admin before closing it.';
  }
  return described.message;
}
