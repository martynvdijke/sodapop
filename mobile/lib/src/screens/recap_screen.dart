import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

/// One year of the member's own history, presented as a recap rather than a
/// settings list: a year selector, the totals, what and where they drank, how
/// they rated, how that moved, the flavour fingerprint and the badges the year
/// earned.
///
/// The data is the same `/api/v1/me/recap/{year}` aggregate the web page uses.
/// It is owner-scoped, so there is no handle here and no way to read another
/// member's year.
class RecapScreen extends ConsumerStatefulWidget {
  const RecapScreen({super.key, this.initialYear});

  /// Year to open on. Defaults to the current year, which is what the profile
  /// row links to.
  final int? initialYear;

  static Future<void> open(BuildContext context, {int? year}) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => RecapScreen(initialYear: year)),
    );
  }

  @override
  ConsumerState<RecapScreen> createState() => _RecapScreenState();
}

class _RecapScreenState extends ConsumerState<RecapScreen> {
  /// Every recap already fetched, so flipping between years is instant and the
  /// discovery probe never repeats a request.
  final Map<int, YearRecap> _recaps = {};

  late int _year;
  List<int> _years = const [];
  bool _loading = true;
  String? _error;

  int get _currentYear => DateTime.now().year;

  @override
  void initState() {
    super.initState();
    final current = _currentYear;
    final initial = (widget.initialYear ?? current).clamp(2000, current);
    _year = initial;
    _years = <int>{current, initial}.toList()..sort((a, b) => b - a);
    unawaited(_load());
  }

  Future<void> _load({bool showSpinner = true}) async {
    if (showSpinner) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final recap = await ref.read(apiProvider).recap(_year);
      if (!mounted) return;
      _recaps[_year] = recap;
      setState(() {
        _loading = false;
        _error = null;
        _years = _yearsFrom(recap);
      });
    } catch (error) {
      if (!mounted) return;
      final message = ApiClient.describe(error).message;
      // A failed refresh keeps the recap that is already on screen and says so
      // in passing, the same way the profile keeps its stats card.
      if (!showSpinner && _recaps.containsKey(_year)) {
        setState(() => _loading = false);
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(message)));
        return;
      }
      setState(() {
        _loading = false;
        _error = message;
      });
    }
  }

  /// The year chips: every year the payload says has data, plus the current
  /// year and whichever year is on screen, newest first. The current year stays
  /// even when it is empty so it still reads as "so far" rather than vanishing.
  List<int> _yearsFrom(YearRecap recap) {
    final years = <int>{...recap.yearsWithData, _currentYear, _year}.toList();
    years.sort((a, b) => b.compareTo(a));
    return years;
  }

  Future<void> _selectYear(int year) async {
    if (year == _year) return;
    final cached = _recaps[year];
    setState(() {
      _year = year;
      _error = null;
      _loading = cached == null;
    });
    if (cached != null) return;
    await _load();
  }

  /// Pull-to-refresh drops the selected year so the recap is rebuilt rather
  /// than replayed from the in-memory copy.
  Future<void> _refresh() {
    _recaps.remove(_year);
    return _load(showSpinner: false);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Year in Soda')),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Building your year…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load your recap',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }

    final theme = Theme.of(context);
    final recap = _recaps[_year];
    final hasData = recap?.hasData ?? false;
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _refresh,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(20, 4, 20, 36),
        children: [
          _yearPicker(theme),
          const SizedBox(height: 16),
          if (!hasData)
            _emptyYear(theme)
          else ...[
            _hero(theme, recap!),
            if (recap.sparse) ...[
              const SizedBox(height: 10),
              Text(
                'A thin year. Everything logged is still counted here.',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            const SizedBox(height: 14),
            _totals(theme, recap),
            const SizedBox(height: 26),
            _drank(theme, recap),
            const SizedBox(height: 26),
            _where(theme, recap),
            const SizedBox(height: 26),
            _rated(theme, recap),
            const SizedBox(height: 26),
            _moved(theme, recap),
            const SizedBox(height: 26),
            _fingerprint(theme, recap),
            const SizedBox(height: 26),
            _badgesFor(theme, recap),
            const SizedBox(height: 24),
            Text(
              'Your recap is yours alone. Every number here comes from your '
              'own check-ins.',
              textAlign: TextAlign.center,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ],
      ),
    );
  }

  /// The year chips. Only years with something in them are offered, plus the
  /// current year whatever it holds, so an empty year still reads as "so far"
  /// rather than as a bug.
  Widget _yearPicker(ThemeData theme) {
    return SizedBox(
      height: 44,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        itemCount: _years.length,
        separatorBuilder: (_, _) => const SizedBox(width: 8),
        itemBuilder: (context, index) {
          final year = _years[index];
          final selected = year == _year;
          return Semantics(
            button: true,
            selected: selected,
            label: selected ? '$year, selected' : 'Show $year',
            child: ExcludeSemantics(
              child: Material(
                color: selected
                    ? theme.colorScheme.primary
                    : Colors.transparent,
                shape: StadiumBorder(
                  side: BorderSide(
                    color: selected
                        ? Colors.transparent
                        : theme.colorScheme.onSurface.withValues(alpha: 0.16),
                  ),
                ),
                child: InkWell(
                  onTap: () => _selectYear(year),
                  customBorder: const StadiumBorder(),
                  child: Container(
                    alignment: Alignment.center,
                    padding: const EdgeInsets.symmetric(horizontal: 18),
                    child: Text(
                      '$year',
                      style: theme.textTheme.labelLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                        color: selected
                            ? theme.colorScheme.onPrimary
                            : theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          );
        },
      ),
    );
  }

  /// The lime panel: the one loud moment on the page, carrying the year and
  /// the count that made it a year worth remembering.
  Widget _hero(ThemeData theme, YearRecap recap) {
    final ink = theme.colorScheme.onPrimary;
    final inProgress = recap.year == _currentYear;
    return Container(
      padding: const EdgeInsets.fromLTRB(20, 16, 20, 18),
      decoration: BoxDecoration(
        color: theme.colorScheme.primary,
        borderRadius: BorderRadius.circular(22),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.auto_awesome_rounded, size: 16, color: ink),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'YEAR IN SODA',
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: ink.withValues(alpha: 0.8),
                    fontWeight: FontWeight.w800,
                    letterSpacing: 1.4,
                  ),
                ),
              ),
              if (inProgress)
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 10,
                    vertical: 4,
                  ),
                  decoration: BoxDecoration(
                    color: ink.withValues(alpha: 0.14),
                    borderRadius: BorderRadius.circular(999),
                  ),
                  child: Text(
                    'So far',
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: ink,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
            ],
          ),
          const SizedBox(height: 6),
          Semantics(
            header: true,
            child: Text(
              '${recap.year}',
              style: theme.textTheme.displayMedium?.copyWith(
                color: ink,
                fontWeight: FontWeight.w800,
                letterSpacing: -2,
                height: 1,
              ),
            ),
          ),
          const SizedBox(height: 6),
          Text(
            sodaCount(recap.totals.checkins, 'check-in'),
            style: theme.textTheme.titleMedium?.copyWith(
              color: ink.withValues(alpha: 0.9),
              fontWeight: FontWeight.w700,
            ),
          ),
        ],
      ),
    );
  }

  /// The counters the hero line does not carry. Zero counters are dropped so
  /// the grid never reads as a wall of noughts.
  Widget _totals(ThemeData theme, YearRecap recap) {
    final totals = recap.totals;
    final stats = <(String, String)>[
      ('${totals.uniqueDrinks}', 'drinks'),
      ('${totals.styles}', 'styles'),
      if (totals.newStyles > 0) ('${totals.newStyles}', 'new styles'),
      if (totals.venues > 0) ('${totals.venues}', 'venues'),
      if (totals.cities > 0) ('${totals.cities}', 'cities'),
      if (totals.countries > 0) ('${totals.countries}', 'countries'),
      if (totals.longestStreak > 1) ('${totals.longestStreak}', 'day streak'),
    ];
    if (stats.isEmpty) return const SizedBox.shrink();
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 16),
        child: LayoutBuilder(
          builder: (context, constraints) {
            final width = constraints.maxWidth / 3;
            return Wrap(
              runSpacing: 16,
              children: [
                for (final (value, label) in stats)
                  SizedBox(width: width, child: _statCell(theme, value, label)),
              ],
            );
          },
        ),
      ),
    );
  }

  Widget _statCell(ThemeData theme, String value, String label) {
    return Semantics(
      container: true,
      label: '$value $label',
      child: ExcludeSemantics(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              value,
              style: theme.textTheme.headlineSmall?.copyWith(
                fontWeight: FontWeight.w800,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
            const SizedBox(height: 1),
            Text(
              label,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }

  /// What the year tasted like: the drink logged most, the brand behind it,
  /// and the styles that kept turning up.
  Widget _drank(ThemeData theme, YearRecap recap) {
    final drink = recap.topDrink;
    final brand = recap.topBrand;
    if (drink == null && brand == null && recap.topStyles.isEmpty) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'What you drank'),
        const SizedBox(height: 10),
        if (drink != null || brand != null)
          Card(
            child: Column(
              children: [
                if (drink != null)
                  _feature(
                    theme,
                    label: 'Most check-ins',
                    name: drink.name,
                    meta: [
                      if (drink.brand.isNotEmpty && drink.brand != drink.name)
                        drink.brand,
                      sodaCount(drink.count, 'check-in'),
                    ].join(' · '),
                    icon: Icons.local_drink_rounded,
                  ),
                if (drink != null && brand != null) _rule(theme),
                if (brand != null)
                  _feature(
                    theme,
                    label: 'Top brand',
                    name: brand.name,
                    meta: sodaCount(brand.count, 'check-in'),
                    icon: Icons.storefront_rounded,
                  ),
              ],
            ),
          ),
        if (recap.topStyles.isNotEmpty) ...[
          const SizedBox(height: 14),
          Text(
            'Top styles',
            style: theme.textTheme.labelLarge?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
              fontWeight: FontWeight.w700,
            ),
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final style in recap.topStyles)
                SodaPill(
                  icon: Icons.bubble_chart_outlined,
                  label:
                      '${style.name.isEmpty ? style.slug : style.name} '
                      '· ${style.count}',
                ),
            ],
          ),
        ],
      ],
    );
  }

  Widget _feature(
    ThemeData theme, {
    required String label,
    required String name,
    required String meta,
    required IconData icon,
  }) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: Icon(
              icon,
              size: 20,
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  name.isEmpty ? 'Unknown' : name,
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                    letterSpacing: -0.2,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  meta,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _where(ThemeData theme, YearRecap recap) {
    final venue = recap.topVenue;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Where you drank'),
        const SizedBox(height: 10),
        if (venue == null)
          Text(
            'No check-ins at a venue in ${recap.year}.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          )
        else
          Card(
            child: _feature(
              theme,
              label: 'Most visits',
              name: venue.name,
              meta: sodaCount(venue.count, 'check-in'),
              icon: Icons.place_outlined,
            ),
          ),
      ],
    );
  }

  /// The rating histogram: five bands, each counted honestly, with the average
  /// stated in the stars the rest of the app uses.
  Widget _rated(ThemeData theme, YearRecap recap) {
    final highest = recap.ratingBuckets.fold<int>(
      0,
      (best, bucket) => bucket.count > best ? bucket.count : best,
    );
    final rated = recap.ratedCount;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'How you rated'),
        const SizedBox(height: 10),
        if (rated == 0)
          Text(
            'No ratings in ${recap.year}. A rating you give a soda lands in '
            'one of five quarter-star bands here.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          )
        else ...[
          Text(
            _ratedLine(rated, recap.totals.avgRating),
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 6),
          Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
              child: Column(
                children: [
                  for (
                    var index = 0;
                    index < recap.ratingBuckets.length;
                    index++
                  )
                    _RecapBar(
                      index: index,
                      label: recap.ratingBuckets[index].label,
                      meta: sodaCount(
                        recap.ratingBuckets[index].count,
                        'check-in',
                      ),
                      fraction: highest == 0
                          ? 0
                          : recap.ratingBuckets[index].count / highest,
                    ),
                ],
              ),
            ),
          ),
        ],
      ],
    );
  }

  /// The year-on-year comparison. It says which way the number moved and
  /// nothing more, because there is no second signal to interpret.
  Widget _moved(ThemeData theme, YearRecap recap) {
    if (recap.palate.isEmpty) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const SodaSectionTitle(title: 'How your ratings moved'),
          const SizedBox(height: 10),
          Text(
            'Rated check-ins in both ${recap.year} and ${recap.year - 1} are '
            'needed to compare.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      );
    }

    final rows = <(String, String)>[];
    for (final row in recap.palate) {
      rows
        ..add(('${recap.year}', _stars(row.thisYear)))
        ..add(('${recap.year - 1}', _stars(row.lastYear)));
    }
    final note = _palateNote(recap);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'How your ratings moved'),
        const SizedBox(height: 10),
        Card(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                for (final (label, value) in rows)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 4),
                    child: Row(
                      children: [
                        SizedBox(
                          width: 76,
                          child: Text(
                            label,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ),
                        Expanded(
                          child: Text(
                            value,
                            style: theme.textTheme.bodyMedium?.copyWith(
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                if (note.isNotEmpty) ...[
                  const SizedBox(height: 6),
                  Text(
                    note,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ],
    );
  }

  /// The flavour fingerprint, built from the whole history rather than just
  /// the selected year, which the API states in the same payload.
  /// The opening sentence of the fingerprint footnote. The payload says
  /// whether the axes cover the whole history or only the selected year, so the
  /// claim is data rather than hardcoded prose.
  static String _fingerprintScope(YearRecap recap) {
    final wholeHistory = recap.fingerprint?.wholeHistory ?? true;
    if (wholeHistory) {
      return 'Built from all your check-ins, not just ${recap.year}.';
    }
    return 'Built from your ${recap.year} check-ins.';
  }

  Widget _fingerprint(ThemeData theme, YearRecap recap) {
    final fingerprint = recap.fingerprint;
    final axes = fingerprint?.axes ?? const <RecapAxis>[];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Flavour fingerprint'),
        const SizedBox(height: 10),
        if (axes.isEmpty)
          Text(
            'No flavour data yet. Drinks tagged with flavours build this '
            'chart.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          )
        else ...[
          Text(
            '${_fingerprintScope(recap)} A score rises with how often you '
            'drink a flavour and how highly you rate it. Your strongest '
            'scores 100.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 6),
          Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
              child: Column(
                children: [
                  for (var index = 0; index < axes.length; index++)
                    _RecapBar(
                      index: index,
                      label: axes[index].label,
                      meta:
                          '${_whole(axes[index].score)} · '
                          '${sodaCount(axes[index].samples, 'check-in')}',
                      fraction: axes[index].score / 100,
                      flagged: !axes[index].confident,
                    ),
                ],
              ),
            ),
          ),
          if (axes.any((axis) => !axis.confident)) ...[
            const SizedBox(height: 8),
            Text(
              'A bar marked “few check-ins so far” rests on fewer than five '
              'check-ins, so it may move as you log more.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ],
      ],
    );
  }

  /// Badges awarded inside the selected year. The recap payload already
  /// filters them by award year and orders them like the badge list, so there
  /// is no second request and no client-side date parsing.
  Widget _badgesFor(ThemeData theme, YearRecap recap) {
    final earned = recap.badges;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaSectionTitle(
          title: 'Badges in ${recap.year}',
          trailing: earned.isEmpty ? null : '${earned.length}',
        ),
        const SizedBox(height: 10),
        if (earned.isEmpty)
          Text(
            'No badges in ${recap.year}.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          )
        else
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final badge in earned)
                SodaPill(
                  icon: Icons.workspace_premium_outlined,
                  label: badge.name.isEmpty ? badge.slug : badge.name,
                  background: theme.colorScheme.primary.withValues(alpha: 0.18),
                  foreground: theme.colorScheme.onSurface,
                ),
            ],
          ),
      ],
    );
  }

  Widget _emptyYear(ThemeData theme) {
    final inProgress = _year == _currentYear;
    return Padding(
      padding: const EdgeInsets.only(top: 28),
      child: SodaStatusView(
        dense: true,
        icon: Icons.local_drink_outlined,
        title: 'Nothing logged in $_year',
        message: inProgress
            ? 'The year is still running. Log a soda and this page builds '
                  'itself from your check-ins.'
            : 'No check-ins are recorded for this year. Log one and the page '
                  'fills in.',
      ),
    );
  }

  Widget _rule(ThemeData theme) {
    return Divider(
      height: 1,
      color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
    );
  }

  /// "12 check-ins, averaging 4.25." The service average is on the 0–20 scale.
  static String _ratedLine(int rated, double? avg) {
    final head = '${sodaCount(rated, 'check-in')} rated';
    if (avg == null || avg <= 0) return '$head.';
    return '$head, averaging ${_stars(avg)}.';
  }

  /// A 0–20 average on the star scale, or an honest dash when the year has no
  /// rated check-ins.
  static String _stars(double? quarters) {
    if (quarters == null || quarters <= 0) return 'No ratings';
    final stars = quarters / 4;
    if (stars == stars.roundToDouble()) return '${stars.round()}';
    final text = stars.toStringAsFixed(2);
    return text
        .replaceFirst(RegExp(r'0+$'), '')
        .replaceFirst(RegExp(r'\.$'), '');
  }

  static String _whole(double value) {
    if (value == value.roundToDouble()) return '${value.round()}';
    return value.toStringAsFixed(1);
  }

  static String _palateNote(YearRecap recap) {
    for (final row in recap.palate) {
      final thisYear = row.thisYear;
      final lastYear = row.lastYear;
      if (thisYear == null || lastYear == null) continue;
      final delta = thisYear - lastYear;
      if (delta.abs() < 0.5) return 'About the same as ${recap.year - 1}.';
      return delta > 0
          ? 'Higher than ${recap.year - 1}.'
          : 'Lower than ${recap.year - 1}.';
    }
    return '';
  }
}

/// One labelled bar. The fill grows on first paint so a chart arrives rather
/// than simply appearing; the number beside the label keeps it readable
/// without seeing the bar at all.
class _RecapBar extends StatelessWidget {
  const _RecapBar({
    required this.label,
    required this.meta,
    required this.fraction,
    this.flagged = false,
    this.index = 0,
  });

  final String label;
  final String meta;

  /// Bar length as a share of the tallest value in its chart, 0–1.
  final double fraction;
  final bool flagged;

  /// Position in its chart, used only to stagger the entrance.
  final int index;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final value = fraction.clamp(0.0, 1.0);
    return Semantics(
      container: true,
      label: '$label. $meta${flagged ? '. Few check-ins.' : '.'}',
      child: ExcludeSemantics(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 5),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.baseline,
                textBaseline: TextBaseline.alphabetic,
                children: [
                  Expanded(
                    child: Text(
                      label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodyMedium?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Flexible(
                    child: Align(
                      alignment: Alignment.centerRight,
                      child: Text(
                        meta,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 6),
              LayoutBuilder(
                builder: (context, constraints) {
                  final width = constraints.maxWidth;
                  return TweenAnimationBuilder<double>(
                    tween: Tween<double>(begin: 0, end: value),
                    duration: Duration(milliseconds: 380 + index * 70),
                    curve: Curves.easeOutCubic,
                    builder: (context, animation, _) => Container(
                      height: 8,
                      width: double.infinity,
                      alignment: Alignment.centerLeft,
                      decoration: BoxDecoration(
                        color: theme.colorScheme.onSurface.withValues(
                          alpha: 0.07,
                        ),
                        borderRadius: BorderRadius.circular(999),
                      ),
                      child: Container(
                        height: 8,
                        width: width * animation,
                        decoration: BoxDecoration(
                          color: theme.colorScheme.primary,
                          borderRadius: BorderRadius.circular(999),
                        ),
                      ),
                    ),
                  );
                },
              ),
              if (flagged) ...[
                const SizedBox(height: 3),
                Text(
                  'few check-ins so far',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
