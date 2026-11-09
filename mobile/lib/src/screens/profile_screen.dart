import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/checkin_filter_sheet.dart';
import '../widgets/pinned_drink_card.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'checkin_detail_screen.dart';
import 'drink_detail_screen.dart';
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
  List<CheckinSummary> _checkins = const [];
  CheckinFilter _filter = const CheckinFilter();
  bool _loading = true;
  bool _checkinsLoading = true;
  bool _loadingMore = false;
  bool _hasNext = false;
  int _nextPage = 2;
  String? _error;
  String? _checkinsError;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<CheckinPage> _fetchCheckins(int page) {
    final api = ref.read(apiProvider);
    return api.myCheckins(
      limit: 10,
      page: page,
      from: _filter.from,
      to: _filter.to,
      ratingMin: _filter.ratingMin,
      ratingMax: _filter.ratingMax,
      minLikes: _filter.minLikes,
      hasPhoto: _filter.hasPhoto ? true : null,
      visibility: _filter.visibility,
      sort: _filter.sort,
    );
  }

  Future<void> _load({bool showSpinner = true}) async {
    if (showSpinner) {
      setState(() {
        _loading = true;
        _checkinsLoading = true;
        _error = null;
        _checkinsError = null;
      });
    }
    try {
      final results = await Future.wait<Object>([
        ref.read(apiProvider).myStats(),
        _fetchCheckins(1),
      ]);
      if (!mounted) return;
      final page = results[1] as CheckinPage;
      setState(() {
        _stats = results[0] as MyStats;
        _checkins = List.of(page);
        _hasNext = page.hasNext;
        _nextPage = page.page + 1;
        _loading = false;
        _checkinsLoading = false;
        _error = null;
        _checkinsError = null;
      });
    } catch (error) {
      if (!mounted) return;
      final message = ApiClient.describe(error).message;
      setState(() {
        _loading = false;
        _checkinsLoading = false;
        _error = message;
        _checkinsError = message;
      });
    }
  }

  /// Filter changes only reload the check-in list; the stats card stays put.
  Future<void> _loadCheckins() async {
    setState(() {
      _checkinsLoading = true;
      _checkinsError = null;
    });
    try {
      final page = await _fetchCheckins(1);
      if (!mounted) return;
      setState(() {
        _checkins = List.of(page);
        _hasNext = page.hasNext;
        _nextPage = page.page + 1;
        _checkinsLoading = false;
        _checkinsError = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _checkinsLoading = false;
        _checkinsError = ApiClient.describe(error).message;
      });
    }
  }

  Future<void> _loadMore() async {
    if (_loadingMore || !_hasNext) return;
    setState(() => _loadingMore = true);
    try {
      final page = await _fetchCheckins(_nextPage);
      if (!mounted) return;
      setState(() {
        _checkins = [..._checkins, ...page];
        _hasNext = page.hasNext;
        _nextPage = page.page + 1;
        _loadingMore = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _loadingMore = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _openFilters() async {
    final result = await showCheckinFilterSheet(context, initial: _filter);
    if (result == null || !mounted) return;
    setState(() => _filter = result);
    await _loadCheckins();
  }

  Future<void> _clearFilters() async {
    if (!_filter.isActive) return;
    setState(() => _filter = const CheckinFilter());
    await _loadCheckins();
  }

  List<(String, CheckinFilter)> _activeChips() {
    if (!_filter.isActive) return const [];
    return [
      if (_filter.range != 'all') (_filter.rangeLabel, _filter.withoutRange()),
      if (_filter.ratingMin != null || _filter.ratingMax != null)
        (_filter.ratingChip, _filter.withoutRating()),
      if (_filter.minLikes != null)
        (_filter.likesChip, _filter.withoutMinLikes()),
      if (_filter.hasPhoto) (_filter.photoChip, _filter.withoutPhoto()),
      if (_filter.visibility != null)
        (_filter.visibilityChip, _filter.withoutVisibility()),
      if (_filter.sort != 'newest') (_filter.sortChip, _filter.withoutSort()),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    final me = auth.me;
    final theme = Theme.of(context);
    final api = ref.read(apiProvider);
    final favourite = me?.favouriteDrink;
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
            const SizedBox(height: 18),
            if (favourite != null)
              PinnedDrinkCard(
                drink: favourite,
                imageHeaders: api.imageHeaders,
                baseUrl: api.server,
                onTap: () => DrinkDetailScreen.open(
                  context,
                  favourite.slug,
                  name: favourite.name,
                ),
              )
            else
              const PinnedDrinkEmpty(
                message: 'Pin a soda from its page to show it here.',
              ),
            const SizedBox(height: 20),
            _statsSection(theme),
            const SizedBox(height: 26),
            _checkinsSection(theme),
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
        SodaAvatar(
          initial: source,
          size: 68,
          imageUrl: me?.avatarUrl,
          headers: ref.read(apiProvider).imageHeaders,
          baseUrl: ref.read(apiProvider).server,
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

  Widget _checkinsSection(ThemeData theme) {
    final count = _checkins.length;
    final chips = _activeChips();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: SodaSectionTitle(
                title: 'Check-ins',
                trailing: _checkinsLoading || _checkinsError != null
                    ? null
                    : '${sodaCount(count, 'check-in')}${_hasNext ? '+' : ''}',
              ),
            ),
            IconButton(
              key: const Key('profile-checkin-filter'),
              tooltip: 'Filter check-ins',
              onPressed: _openFilters,
              icon: Badge.count(
                count: _filter.activeCount,
                isLabelVisible: _filter.isActive,
                backgroundColor: Soda.tangerine,
                child: const Icon(Icons.tune_rounded),
              ),
            ),
          ],
        ),
        if (chips.isNotEmpty) ...[
          const SizedBox(height: 2),
          Wrap(
            spacing: 8,
            runSpacing: 6,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              for (final (label, next) in chips)
                InputChip(
                  label: Text(label),
                  onDeleted: () async {
                    setState(() => _filter = next);
                    await _loadCheckins();
                  },
                  deleteIconColor: theme.colorScheme.onSurfaceVariant,
                ),
              TextButton(
                key: const Key('profile-clear-filters'),
                onPressed: _clearFilters,
                child: const Text('Clear all'),
              ),
            ],
          ),
        ],
        const SizedBox(height: 10),
        if (_checkinsLoading)
          const SodaLoadingView(dense: true, label: 'Loading check-ins…')
        else if (_checkinsError != null)
          SodaStatusView(
            dense: true,
            icon: Icons.cloud_off_rounded,
            title: 'Check-ins did not load',
            message: _checkinsError,
            actionLabel: 'Try again',
            onAction: _loadCheckins,
          )
        else if (_checkins.isEmpty && _filter.isActive)
          SodaStatusView(
            dense: true,
            icon: Icons.filter_alt_off_outlined,
            title: 'No check-ins match these filters',
            message: 'Try wider dates or clear a filter.',
            actionLabel: 'Clear filters',
            onAction: _clearFilters,
          )
        else if (_checkins.isEmpty)
          const SodaStatusView(
            dense: true,
            icon: Icons.local_drink_outlined,
            title: 'No check-ins yet',
            message: 'Scan a soda to start your log.',
          )
        else ...[
          for (var index = 0; index < _checkins.length; index++) ...[
            if (index > 0) const SizedBox(height: 10),
            _recentCard(theme, _checkins[index]),
          ],
          if (_hasNext) ...[
            const SizedBox(height: 12),
            Center(
              child: OutlinedButton(
                key: const Key('profile-load-more'),
                onPressed: _loadingMore ? null : _loadMore,
                child: Text(_loadingMore ? 'Loading…' : 'Load more'),
              ),
            ),
          ],
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
                            text: rated ? ratingLabel(quarters) : 'Not rated',
                            style: rated
                                ? const TextStyle(
                                    color: Soda.tangerine,
                                    fontWeight: FontWeight.w700,
                                  )
                                : null,
                          ),
                          if (checkin.venueName.isNotEmpty)
                            TextSpan(text: ' · ${checkin.venueName}'),
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
                    if (sodaPrepMethod(checkin.prepMethod).isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        checkin.prepNote.isEmpty
                            ? sodaPrepMethod(checkin.prepMethod)
                            : '${sodaPrepMethod(checkin.prepMethod)} · ${checkin.prepNote}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: metaStyle,
                      ),
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
              (Icons.science_outlined, 'Mix', () => MixScreen.open(context)),
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
