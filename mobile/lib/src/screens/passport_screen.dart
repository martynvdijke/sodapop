import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'badges_screen.dart';
import 'drink_detail_screen.dart';

/// Opens a drink from a passport row. Injected by widget tests so navigation
/// can be checked without pushing a real route.
typedef PassportDrinkOpener =
    void Function(BuildContext context, String slug, String name);

/// Lifetime stats, the stamp collection and earned badges. The full badge list
/// lives behind the "See all badges" link and adds progress for unearned stamps.
class PassportScreen extends ConsumerStatefulWidget {
  const PassportScreen({super.key, this.onOpenDrink});

  final PassportDrinkOpener? onOpenDrink;

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const PassportScreen()));
  }

  @override
  ConsumerState<PassportScreen> createState() => _PassportScreenState();
}

class _PassportScreenState extends ConsumerState<PassportScreen> {
  Passport? _passport;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final passport = await ref.read(apiProvider).passport();
      if (!mounted) return;
      setState(() {
        _passport = passport;
        _loading = false;
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
    return Scaffold(
      appBar: AppBar(title: const Text('Passport')),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Opening passport…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load your passport',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    final passport = _passport!;
    final recent = passport.recent.take(8).toList();
    final theme = Theme.of(context);
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          Text(
            'Every check-in is a stamp.',
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            'Drinks tried, toasts received, and badges earned.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 20),
          _stats(passport.stats),
          const SizedBox(height: 14),
          _collected(passport.totals),
          const SizedBox(height: 28),
          SodaSectionTitle(
            title: 'Earned badges',
            trailing: passport.badges.isEmpty
                ? null
                : '${passport.badges.length}',
          ),
          const SizedBox(height: 10),
          if (passport.badges.isEmpty)
            const SodaStatusView(
              dense: true,
              icon: Icons.workspace_premium_outlined,
              title: 'No badges yet',
              message: 'Check in a few sodas and stamps start showing up.',
            )
          else
            _badgeGrid(passport.badges),
          const SizedBox(height: 8),
          Center(
            child: TextButton.icon(
              onPressed: () => BadgesScreen.open(context),
              icon: const Icon(Icons.arrow_forward_rounded, size: 18),
              label: const Text('See all badges'),
            ),
          ),
          const SizedBox(height: 24),
          SodaSectionTitle(
            title: 'Styles collected',
            trailing: passport.styles.isEmpty
                ? null
                : '${passport.styles.length}',
          ),
          const SizedBox(height: 10),
          _styles(passport.styles),
          const SizedBox(height: 26),
          SodaSectionTitle(
            title: 'Venue kinds',
            trailing: passport.venueKinds.isEmpty
                ? null
                : '${passport.venueKinds.length}',
          ),
          const SizedBox(height: 10),
          _venueKinds(passport.venueKinds),
          const SizedBox(height: 26),
          SodaSectionTitle(
            title: 'Countries',
            trailing: passport.countries.isEmpty
                ? null
                : '${passport.countries.length}',
          ),
          const SizedBox(height: 10),
          _countries(passport.countries),
          const SizedBox(height: 26),
          SodaSectionTitle(
            title: 'Recent pours',
            trailing: recent.isEmpty ? null : '${recent.length}',
          ),
          const SizedBox(height: 10),
          _recent(recent),
        ],
      ),
    );
  }

  Widget _stats(ProfileStats stats) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          children: [
            Row(
              children: [
                Expanded(child: _stat('${stats.checkins}', 'Check-ins')),
                _divider(theme),
                Expanded(child: _stat('${stats.drinks}', 'Drinks')),
              ],
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(child: _stat('${stats.toasts}', 'Toasts')),
                _divider(theme),
                Expanded(child: _stat('${stats.badges}', 'Badges')),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _stat(String value, String label) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          value,
          style: theme.textTheme.headlineSmall?.copyWith(
            fontWeight: FontWeight.w800,
          ),
        ),
        const SizedBox(height: 2),
        Text(
          label,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }

  Widget _divider(ThemeData theme) {
    return Container(
      width: 1,
      height: 42,
      margin: const EdgeInsets.symmetric(horizontal: 8),
      color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
    );
  }

  /// The stamp counters. A lime panel keeps them visually separate from the
  /// paper stats card above, and check-ins live in one place only.
  Widget _collected(PassportTotals totals) {
    final theme = Theme.of(context);
    final ink = theme.colorScheme.onPrimary;
    return Container(
      padding: const EdgeInsets.fromLTRB(18, 16, 18, 16),
      decoration: BoxDecoration(
        color: theme.colorScheme.primary,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.approval_rounded, size: 18, color: ink),
              const SizedBox(width: 8),
              Text(
                'Collected',
                style: theme.textTheme.labelLarge?.copyWith(
                  color: ink,
                  fontWeight: FontWeight.w800,
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: _collectedStat(
                  '${totals.styles}',
                  'Styles',
                  ink,
                  valueKey: const Key('stamp-total-styles'),
                ),
              ),
              _collectedDivider(ink),
              Expanded(
                child: _collectedStat(
                  '${totals.venueKinds}',
                  'Venue kinds',
                  ink,
                  valueKey: const Key('stamp-total-venue-kinds'),
                ),
              ),
              _collectedDivider(ink),
              Expanded(
                child: _collectedStat(
                  '${totals.countries}',
                  'Countries',
                  ink,
                  valueKey: const Key('stamp-total-countries'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            'The styles, places and countries you have collected so far.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: ink.withValues(alpha: 0.72),
            ),
          ),
        ],
      ),
    );
  }

  Widget _collectedStat(
    String value,
    String label,
    Color ink, {
    Key? valueKey,
  }) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          value,
          key: valueKey,
          style: theme.textTheme.headlineSmall?.copyWith(
            color: ink,
            fontWeight: FontWeight.w800,
          ),
        ),
        const SizedBox(height: 2),
        Text(
          label,
          style: theme.textTheme.bodySmall?.copyWith(
            color: ink.withValues(alpha: 0.78),
          ),
        ),
      ],
    );
  }

  Widget _collectedDivider(Color ink) {
    return Container(
      width: 1,
      height: 40,
      margin: const EdgeInsets.symmetric(horizontal: 10),
      color: ink.withValues(alpha: 0.2),
    );
  }

  Widget _styles(List<PassportStyle> styles) {
    if (styles.isEmpty) {
      return _emptyLine(
        'No style stamps yet. Your first check-in starts this list.',
      );
    }
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Column(
          children: [
            for (var index = 0; index < styles.length; index++) ...[
              if (index > 0)
                const Divider(height: 1, indent: 16, endIndent: 16),
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 12,
                ),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        styles[index].name.isEmpty
                            ? 'Unknown style'
                            : styles[index].name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Text(
                      sodaCount(styles[index].count, 'pour'),
                      style: theme.textTheme.bodySmall?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    if (styles[index].lastAt.isNotEmpty) ...[
                      const SizedBox(width: 12),
                      SizedBox(
                        width: 84,
                        child: Row(
                          mainAxisAlignment: MainAxisAlignment.end,
                          children: [
                            Icon(
                              Icons.history_rounded,
                              size: 13,
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                            const SizedBox(width: 4),
                            Flexible(
                              child: Text(
                                sodaDate(styles[index].lastAt),
                                textAlign: TextAlign.right,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: theme.textTheme.bodySmall?.copyWith(
                                  color: theme.colorScheme.onSurfaceVariant,
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _venueKinds(List<PassportVenueKind> kinds) {
    if (kinds.isEmpty) {
      return _emptyLine(
        'No venue stamps yet. Kinds of place appear when you check in somewhere.',
      );
    }
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final kind in kinds)
          _stampChip(
            icon: Icons.place_outlined,
            label: _venueKindLabel(kind.kind),
            count: kind.count,
          ),
      ],
    );
  }

  static String _venueKindLabel(String kind) {
    final label = sodaVenueKind(kind.trim());
    return label.isEmpty ? 'Unknown' : label;
  }

  Widget _countries(List<PassportCountry> countries) {
    if (countries.isEmpty) {
      return _emptyLine(
        'No country stamps yet. Add a venue with a country when you check in.',
      );
    }
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final country in countries)
          _stampChip(
            icon: Icons.public_rounded,
            label: _countryLabel(country.country),
            count: country.count,
          ),
      ],
    );
  }

  Widget _stampChip({
    required IconData icon,
    required String label,
    required int count,
  }) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 7, 7, 7),
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.12),
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 15, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.labelLarge?.copyWith(
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
          const SizedBox(width: 8),
          Container(
            constraints: const BoxConstraints(minWidth: 24),
            padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
            decoration: BoxDecoration(
              color: theme.colorScheme.primary,
              borderRadius: BorderRadius.circular(999),
            ),
            child: Text(
              '$count',
              textAlign: TextAlign.center,
              style: theme.textTheme.labelSmall?.copyWith(
                color: theme.colorScheme.onPrimary,
                fontWeight: FontWeight.w800,
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _recent(List<PassportRecent> pours) {
    if (pours.isEmpty) {
      return _emptyLine(
        'No pours yet. Your last check-ins will be listed here.',
      );
    }
    return Card(
      child: ClipRRect(
        borderRadius: BorderRadius.circular(16),
        child: Column(
          children: [
            for (var index = 0; index < pours.length; index++) ...[
              if (index > 0)
                const Divider(height: 1, indent: 16, endIndent: 16),
              _recentRow(pours[index]),
            ],
          ],
        ),
      ),
    );
  }

  Widget _recentRow(PassportRecent pour) {
    final theme = Theme.of(context);
    final tappable = pour.slug.isNotEmpty;
    final subtitle = [
      pour.style,
      if (pour.createdAt.isNotEmpty) sodaDate(pour.createdAt),
    ].where((part) => part.isNotEmpty).join(' · ');
    return InkWell(
      onTap: tappable
          ? () => _openDrink(pour.slug, pour.name.isEmpty ? 'Soda' : pour.name)
          : null,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
        child: Row(
          children: [
            Container(
              width: 34,
              height: 34,
              alignment: Alignment.center,
              decoration: BoxDecoration(
                color: theme.colorScheme.primary.withValues(alpha: 0.35),
                shape: BoxShape.circle,
              ),
              child: Icon(
                Icons.local_drink_outlined,
                size: 18,
                color: theme.colorScheme.onSurface,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    pour.name.isEmpty ? 'Soda' : pour.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.titleSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  if (subtitle.isNotEmpty) ...[
                    const SizedBox(height: 2),
                    Text(
                      subtitle,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 10),
            _ratingLabel(pour.rating),
            if (tappable) ...[
              const SizedBox(width: 2),
              Icon(
                Icons.chevron_right_rounded,
                size: 18,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _ratingLabel(int? quarters) {
    final theme = Theme.of(context);
    if (quarters == null) {
      return Text(
        'Not rated',
        style: theme.textTheme.labelSmall?.copyWith(
          color: theme.colorScheme.onSurfaceVariant,
        ),
      );
    }
    return Semantics(
      container: true,
      label: 'Rated ${_quarterStars(quarters)} out of 5',
      child: ExcludeSemantics(
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.star_rounded, size: 16, color: Soda.tangerine),
            const SizedBox(width: 3),
            Text(
              _quarterStars(quarters),
              style: theme.textTheme.labelMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _emptyLine(String text) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
        ),
      ),
      child: Row(
        children: [
          Icon(
            Icons.approval_rounded,
            size: 18,
            color: theme.colorScheme.onSurfaceVariant,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              text,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }

  void _openDrink(String slug, String name) {
    if (slug.isEmpty) return;
    final opener = widget.onOpenDrink;
    if (opener != null) {
      opener(context, slug, name);
      return;
    }
    DrinkDetailScreen.open(context, slug, name: name);
  }

  /// "4", "4.25", "3.5" from a 0–20 quarter-star value.
  static String _quarterStars(int quarters) {
    final value = quarters.clamp(0, 20);
    final whole = value ~/ 4;
    final quartersLeft = value % 4;
    if (quartersLeft == 0) return '$whole';
    final fraction = (quartersLeft * 25).toString().padLeft(2, '0');
    return '$whole.${fraction.endsWith('0') ? fraction.substring(0, 1) : fraction}';
  }

  /// Two-letter values are shown as a country code; anything else is kept as
  /// the server sent it.
  static String _countryLabel(String country) {
    final value = country.trim();
    if (value.isEmpty) return 'Unknown';
    if (value.length == 2) return value.toUpperCase();
    return value;
  }

  Widget _badgeGrid(List<ProfileBadge> badges) {
    return LayoutBuilder(
      builder: (context, constraints) {
        const gap = 10.0;
        final width = (constraints.maxWidth - gap) / 2;
        return Wrap(
          spacing: gap,
          runSpacing: gap,
          children: [
            for (final badge in badges)
              SizedBox(width: width, height: 150, child: _badgeTile(badge)),
          ],
        );
      },
    );
  }

  Widget _badgeTile(ProfileBadge badge) {
    final theme = Theme.of(context);
    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              Icons.workspace_premium_outlined,
              size: 22,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            const SizedBox(height: 6),
            Text(
              badge.name,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.titleSmall?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            if (badge.description.isNotEmpty) ...[
              const SizedBox(height: 4),
              Expanded(
                child: Text(
                  badge.description,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            ] else
              const Spacer(),
            if (badge.awardedAt.isNotEmpty)
              Text(
                'Earned ${sodaDate(badge.awardedAt)}',
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
          ],
        ),
      ),
    );
  }
}
