import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'checkin_detail_screen.dart';
import 'friends_screen.dart';
import 'insights_screen.dart';
import 'lists_screen.dart';
import 'mix_screen.dart';
import 'notifications_screen.dart';
import 'passport_screen.dart';
import 'saved_posts_screen.dart';
import 'saved_venues_screen.dart';
import 'settings_screen.dart';
import 'taste_screen.dart';
import 'venues_screen.dart';

class ProfileScreen extends ConsumerStatefulWidget {
  const ProfileScreen({super.key});

  @override
  ConsumerState<ProfileScreen> createState() => _ProfileScreenState();
}

class _ProfileScreenState extends ConsumerState<ProfileScreen> {
  MyStats? _stats;
  List<CheckinSummary> _recent = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load({bool showSpinner = true}) async {
    if (showSpinner) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final api = ref.read(apiProvider);
      final results = await Future.wait<Object>([
        api.myStats(),
        api.myCheckins(limit: 5),
      ]);
      if (!mounted) return;
      setState(() {
        _stats = results[0] as MyStats;
        _recent = results[1] as List<CheckinSummary>;
        _loading = false;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    final me = auth.me;
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('You')),
      body: RefreshIndicator(
        color: Soda.lime,
        onRefresh: () => _load(showSpinner: false),
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
          children: [
            _header(theme, me),
            const SizedBox(height: 20),
            _statsSection(theme),
            const SizedBox(height: 26),
            _recentSection(theme),
            const SizedBox(height: 28),
            Text(
              'Activity',
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w800,
              ),
            ),
            const SizedBox(height: 10),
            _activityGrid(),
            const SizedBox(height: 28),
            Text(
              'Account',
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w800,
              ),
            ),
            const SizedBox(height: 10),
            _accountCard(auth),
            const SizedBox(height: 24),
            FilledButton.tonal(
              onPressed: _confirmSignOut,
              child: const Text('Sign out'),
            ),
            const SizedBox(height: 28),
            Text(
              'Sodapop is a quiet, barcode-first soda log. There is no global feed and no public catalog API.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }

  Widget _header(ThemeData theme, Me? me) {
    final source = (me?.displayName.trim().isNotEmpty ?? false)
        ? me!.displayName
        : (me?.handle ?? '');
    return Row(
      children: [
        CircleAvatar(
          radius: 34,
          backgroundColor: theme.colorScheme.primary,
          child: Text(
            source.isEmpty ? '?' : source.characters.first.toUpperCase(),
            style: const TextStyle(
              color: Soda.forest,
              fontSize: 28,
              fontWeight: FontWeight.w800,
            ),
          ),
        ),
        const SizedBox(width: 16),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                me?.displayName ?? '',
                style: theme.textTheme.headlineSmall?.copyWith(
                  fontWeight: FontWeight.w800,
                ),
              ),
              Text(
                '@${me?.handle ?? ''}',
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _statsSection(ThemeData theme) {
    if (_loading) {
      return const Card(
        child: SodaLoadingView(dense: true, label: 'Loading stats…'),
      );
    }
    if (_error != null) {
      return Card(
        child: SodaStatusView(
          dense: true,
          icon: Icons.cloud_off_rounded,
          title: 'Stats did not load',
          message: _error,
          actionLabel: 'Try again',
          onAction: _load,
        ),
      );
    }

    final stats = _stats;
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 14),
        child: Row(
          children: [
            _stat(theme, _count(stats?.checkins), 'Check-ins'),
            _divider(theme),
            _stat(theme, _count(stats?.uniqueDrinks), 'Drinks'),
            _divider(theme),
            _stat(
              theme,
              _average(stats?.avgRating),
              'Average',
              semantics: stats != null && stats.avgRating != null
                  ? 'Average rating ${_average(stats.avgRating)} out of 5'
                  : null,
            ),
            _divider(theme),
            _stat(theme, _count(stats?.badges), 'Badges'),
          ],
        ),
      ),
    );
  }

  Widget _stat(
    ThemeData theme,
    String value,
    String label, {
    String? semantics,
  }) {
    final column = Column(
      children: [
        Text(
          value,
          maxLines: 1,
          style: theme.textTheme.headlineSmall?.copyWith(
            fontWeight: FontWeight.w800,
            fontFeatures: const [FontFeature.tabularFigures()],
          ),
        ),
        const SizedBox(height: 2),
        Text(
          label,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
    return Expanded(
      child: semantics == null
          ? column
          : Semantics(
              container: true,
              label: semantics,
              child: ExcludeSemantics(child: column),
            ),
    );
  }

  Widget _divider(ThemeData theme) {
    return Container(
      width: 1,
      height: 34,
      color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
    );
  }

  Widget _recentSection(ThemeData theme) {
    final count = _recent.length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaSectionTitle(
          title: 'Recent check-ins',
          trailing: _loading || _error != null || count == 0
              ? null
              : sodaCount(count, 'check-in'),
        ),
        const SizedBox(height: 10),
        if (_loading)
          const SodaLoadingView(dense: true, label: 'Loading check-ins…')
        else if (_error != null)
          SodaStatusView(
            dense: true,
            icon: Icons.cloud_off_rounded,
            title: 'Check-ins did not load',
            message: _error,
            actionLabel: 'Try again',
            onAction: _load,
          )
        else if (_recent.isEmpty)
          const SodaStatusView(
            dense: true,
            icon: Icons.local_drink_outlined,
            title: 'No check-ins yet',
            message: 'Scan a soda to start your log.',
          )
        else
          for (var index = 0; index < _recent.length; index++) ...[
            if (index > 0) const SizedBox(height: 10),
            _recentCard(theme, _recent[index]),
          ],
      ],
    );
  }

  Widget _recentCard(ThemeData theme, CheckinSummary checkin) {
    final name = checkin.drinkName.isEmpty ? 'Soda' : checkin.drinkName;
    final quarters = checkin.rating ?? 0;
    final rated = quarters > 0;
    final date = sodaDate(checkin.createdAt);
    final metaStyle = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => CheckinDetailScreen.open(context, checkin.id),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            children: [
              _thumb(theme, checkin),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text.rich(
                      TextSpan(
                        children: [
                          TextSpan(
                            text: rated
                                ? ratingLabel(quarters)
                                : 'Not rated',
                            style: rated
                                ? const TextStyle(
                                    color: Soda.tangerine,
                                    fontWeight: FontWeight.w700,
                                  )
                                : null,
                          ),
                          if (checkin.venueName.isNotEmpty)
                            TextSpan(
                              text: ' · ${checkin.venueName}',
                            ),
                        ],
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: metaStyle,
                    ),
                    if (date.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(date, style: metaStyle),
                    ],
                  ],
                ),
              ),
              Icon(
                Icons.chevron_right_rounded,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _thumb(ThemeData theme, CheckinSummary checkin) {
    const size = 54.0;
    final placeholder = Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Icon(
        Icons.local_drink_outlined,
        size: 22,
        color: theme.colorScheme.onSurfaceVariant,
      ),
    );

    final api = ref.read(apiProvider);
    final url = _thumbUrl(api, checkin.photoUrl);
    if (url == null || !api.configured) return placeholder;

    return ClipRRect(
      borderRadius: BorderRadius.circular(12),
      child: Image.network(
        url,
        width: size,
        height: size,
        fit: BoxFit.cover,
        headers: api.imageHeaders,
        semanticLabel: 'Photo of ${checkin.drinkName}',
        errorBuilder: (_, _, _) => placeholder,
      ),
    );
  }

  static String? _thumbUrl(ApiClient api, String raw) {
    final value = raw.trim();
    if (value.isEmpty) return null;
    if (value.startsWith('http://') || value.startsWith('https://')) {
      return value;
    }
    if (value.startsWith('/')) return '${api.server ?? ''}$value';
    return api.mediaUrl(value);
  }

  Widget _activityGrid() {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(8),
        child: LayoutBuilder(
          builder: (context, constraints) {
            const gap = 8.0;
            final width = (constraints.maxWidth - gap) / 2;
            final items = [
              (
                Icons.notifications_none_rounded,
                'Notifications',
                () => NotificationsScreen.open(context),
              ),
              (
                Icons.people_alt_outlined,
                'Friends',
                () => FriendsScreen.open(context),
              ),
              (
                Icons.insights_outlined,
                'Insights',
                () => InsightsScreen.open(context),
              ),
              (
                Icons.people_outline_rounded,
                'Taste',
                () => TasteScreen.open(context),
              ),
              (
                Icons.explore_outlined,
                'Passport',
                () => PassportScreen.open(context),
              ),
              (
                Icons.queue_music_rounded,
                'Lists',
                () => ListsScreen.open(context),
              ),
              (
                Icons.science_outlined,
                'Mix',
                () => MixScreen.open(context),
              ),
              (
                Icons.place_outlined,
                'Venues',
                () => VenuesScreen.open(context),
              ),
              (
                Icons.bookmark_border_rounded,
                'Saved venues',
                () => SavedVenuesScreen.open(context),
              ),
              (
                Icons.bookmarks_outlined,
                'Saved posts',
                () => SavedPostsScreen.open(context),
              ),
              (
                Icons.settings_outlined,
                'Settings',
                () => SettingsScreen.open(context),
              ),
            ];
            return Wrap(
              spacing: gap,
              runSpacing: gap,
              children: [
                for (final item in items)
                  SizedBox(
                    width: width,
                    child: _ActivityTile(
                      icon: item.$1,
                      label: item.$2,
                      onTap: item.$3,
                    ),
                  ),
              ],
            );
          },
        ),
      ),
    );
  }

  Widget _accountCard(AuthState auth) {
    return Card(
      child: Column(
        children: [
          ListTile(
            leading: const Icon(Icons.dns_outlined),
            title: const Text('Server'),
            subtitle: Text(auth.serverUrl),
          ),
          const Divider(height: 1),
          ListTile(
            leading: const Icon(Icons.key_outlined),
            title: const Text('Personal API token'),
            subtitle: const Text('Stored in Android encrypted storage'),
            trailing: const Icon(Icons.check_circle, color: Soda.lime),
          ),
          const Divider(height: 1),
          ListTile(
            leading: const Icon(Icons.mail_outline_rounded),
            title: const Text('Email'),
            subtitle: Text(auth.me?.email ?? ''),
          ),
        ],
      ),
    );
  }

  Future<void> _confirmSignOut() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Sign out?'),
        content: const Text(
          'The token is deleted from this device. Your check-ins stay on the server.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Sign out'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    await ref.read(authProvider.notifier).signOut();
  }

  static String _count(int? value) => value == null ? '—' : '$value';

  /// Average rating arrives in quarter-star units (0–20), the same scale as a
  /// single check-in, so it is shown in whole stars.
  static String _average(double? quarters) {
    if (quarters == null || quarters <= 0) return '—';
    final rounded = (quarters / 4 * 10).round() / 10;
    return rounded == rounded.roundToDouble()
        ? rounded.toStringAsFixed(0)
        : rounded.toStringAsFixed(1);
  }
}

class _ActivityTile extends StatelessWidget {
  const _ActivityTile({
    required this.icon,
    required this.label,
    required this.onTap,
  });

  final IconData icon;
  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: SizedBox(
          height: 56,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10),
            child: Row(
              children: [
                Icon(icon, size: 20),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    label,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodySmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
