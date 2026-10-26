import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

enum _SeriesMetric { sugar, caffeine }

/// Nutrition totals for the signed-in member, scoped to a rolling window.
class InsightsScreen extends ConsumerStatefulWidget {
  const InsightsScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const InsightsScreen()));
  }

  @override
  ConsumerState<InsightsScreen> createState() => _InsightsScreenState();
}

class _InsightsScreenState extends ConsumerState<InsightsScreen> {
  int _days = 30;
  Insights? _insights;
  bool _loading = true;
  String? _error;
  _SeriesMetric _seriesMetric = _SeriesMetric.sugar;

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
      final insights = await ref.read(apiProvider).insights(days: _days);
      if (!mounted) return;
      setState(() {
        _insights = insights;
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

  void _selectDays(int days) {
    if (days == _days) return;
    setState(() => _days = days);
    _load();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Insights')),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 0),
            child: SodaSegmented<int>(
              options: const [(7, '7 days'), (30, '30 days'), (90, '90 days')],
              selected: _days,
              onChanged: _selectDays,
            ),
          ),
          Expanded(child: _content()),
        ],
      ),
    );
  }

  Widget _content() {
    if (_loading) return const SodaLoadingView(label: 'Loading insights…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load insights',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    final data = _insights!;
    if (data.pours == 0) {
      return RefreshIndicator(
        color: Soda.lime,
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: const [
            SizedBox(height: 32),
            SodaStatusView(
              dense: true,
              icon: Icons.local_drink_outlined,
              title: 'Nothing logged in this range',
              message:
                  'Totals appear once you check in a soda. Sugar, caffeine and calories come from the drink itself.',
            ),
          ],
        ),
      );
    }

    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 36),
        children: [
          _rangeLine(data),
          const SizedBox(height: 14),
          _totals(data),
          const SizedBox(height: 10),
          Text(
            'Averages ${_number(data.avgSugarPerDayG)} g sugar and '
            '${_number(data.avgCaffeinePerDayMg)} mg caffeine a day.',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          if (data.series.isNotEmpty) ...[
            const SizedBox(height: 22),
            _chart(data),
          ],
          if (data.topDrinks.isNotEmpty) ...[
            const SizedBox(height: 26),
            const SodaSectionTitle(title: 'Top drinks'),
            const SizedBox(height: 8),
            _topDrinks(data.topDrinks),
          ],
          const SizedBox(height: 26),
          _caffeineClock(data),
        ],
      ),
    );
  }

  Widget _rangeLine(Insights data) {
    final theme = Theme.of(context);
    final range = [
      if (data.rangeDays > 0) 'Last ${data.rangeDays} days',
      sodaCount(data.pours, 'pour'),
    ].join(' · ');
    return Text(
      range,
      style: theme.textTheme.bodyMedium?.copyWith(
        color: theme.colorScheme.onSurfaceVariant,
      ),
    );
  }

  Widget _totals(Insights data) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          children: [
            Row(
              children: [
                Expanded(child: _metric('Pours', '${data.pours}', '')),
                _divider(theme),
                Expanded(child: _metric('Sugar', _number(data.sugarG), 'g')),
              ],
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(
                  child: _metric('Caffeine', _number(data.caffeineMg), 'mg'),
                ),
                _divider(theme),
                Expanded(
                  child: _metric('Calories', '${data.caloriesKcal}', 'kcal'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _metric(String label, String value, String unit) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text.rich(
          TextSpan(
            children: [
              TextSpan(text: value),
              if (unit.isNotEmpty)
                TextSpan(
                  text: ' $unit',
                  style: theme.textTheme.titleSmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
            ],
          ),
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

  Widget _chart(Insights data) {
    final theme = Theme.of(context);
    final points = data.series;
    double valueOf(InsightPoint point) =>
        _seriesMetric == _SeriesMetric.sugar ? point.sugarG : point.caffeineMg;
    final maxValue = points.fold<double>(
      0,
      (value, point) => math.max(value, valueOf(point)),
    );
    final unit = _seriesMetric == _SeriesMetric.sugar ? 'g' : 'mg';
    final chartLabel = _seriesMetric == _SeriesMetric.sugar
        ? 'Daily sugar chart'
        : 'Daily caffeine chart';

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    'Daily totals',
                    style: theme.textTheme.titleSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                Text(
                  'Peak ${_number(maxValue)} $unit',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            SodaSegmented<_SeriesMetric>(
              options: const [
                (_SeriesMetric.sugar, 'Sugar'),
                (_SeriesMetric.caffeine, 'Caffeine'),
              ],
              selected: _seriesMetric,
              onChanged: (value) => setState(() => _seriesMetric = value),
            ),
            const SizedBox(height: 16),
            Semantics(
              label:
                  '$chartLabel for ${points.length} days. Peak ${_number(maxValue)} $unit.',
              child: ExcludeSemantics(
                child: SizedBox(
                  height: 126,
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.end,
                    children: [
                      for (final point in points)
                        Expanded(
                          child: Padding(
                            padding: EdgeInsets.symmetric(
                              horizontal: points.length > 40 ? 0.5 : 3,
                            ),
                            child: Align(
                              alignment: Alignment.bottomCenter,
                              child: AnimatedContainer(
                                duration: const Duration(milliseconds: 220),
                                curve: Curves.easeOutCubic,
                                width: double.infinity,
                                height: maxValue <= 0
                                    ? 3
                                    : math.max(
                                        3,
                                        (valueOf(point) / maxValue) * 120,
                                      ),
                                decoration: BoxDecoration(
                                  color: point.pours > 0
                                      ? theme.colorScheme.primary
                                      : theme.colorScheme.onSurface.withValues(
                                          alpha: 0.08,
                                        ),
                                  borderRadius: BorderRadius.circular(3),
                                ),
                              ),
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                Text(
                  sodaDate(points.first.date),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                const Spacer(),
                Text(
                  sodaDate(points.last.date),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              'Each bar is one day.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _topDrinks(List<InsightDrink> drinks) {
    final theme = Theme.of(context);
    return Card(
      child: Column(
        children: [
          for (var index = 0; index < drinks.length; index++) ...[
            if (index > 0)
              Divider(
                height: 1,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
              ),
            Padding(
              padding: const EdgeInsets.all(14),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  SizedBox(
                    width: 22,
                    child: Text(
                      '${index + 1}',
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w800,
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          drinks[index].name,
                          style: theme.textTheme.titleSmall?.copyWith(
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        if (drinks[index].brand.isNotEmpty) ...[
                          const SizedBox(height: 2),
                          Text(
                            drinks[index].brand,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ],
                        const SizedBox(height: 4),
                        Text(
                          '${sodaCount(drinks[index].pours, 'pour')} · '
                          '${_number(drinks[index].sugarG)} g sugar',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }

  Widget _caffeineClock(Insights data) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.schedule_rounded,
                  size: 20,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 8),
                Text(
                  'Caffeine clock',
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            Text(
              'When your last caffeinated pour should clear, using a five-hour half-life.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 14),
            _clockRow(
              'Bedtime',
              data.bedtime.isEmpty ? 'Not set' : sodaClock(data.bedtime),
            ),
            _clockRow(
              'Last caffeine',
              data.lastCaffeineAt.isEmpty
                  ? 'None in this range'
                  : sodaDateTime(data.lastCaffeineAt),
            ),
            _clockRow(
              'Clears by',
              data.caffeineClearBy.isEmpty
                  ? '—'
                  : sodaDateTime(data.caffeineClearBy),
            ),
            if (data.lateCaffeine) ...[
              const SizedBox(height: 12),
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(
                    Icons.info_outline_rounded,
                    size: 18,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'Your last caffeine was within eight hours of bedtime. '
                      'The clear time is an estimate, not a rule.',
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _clockRow(String label, String value) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 112,
            child: Text(
              label,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          Expanded(child: Text(value, style: theme.textTheme.bodyMedium)),
        ],
      ),
    );
  }

  static String _number(double value) {
    if (value == value.roundToDouble()) return value.toStringAsFixed(0);
    return value.toStringAsFixed(1);
  }
}
