import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'drink_detail_screen.dart';

/// Opens one drink's detail page. The app shell points this at
/// `DrinkDetailScreen.open`; tests can swap in their own opener.
typedef DrinkOpener =
    Future<void> Function(BuildContext context, String slug, {String? name});

/// Compares two to six catalog drinks in a single side-by-side table.
class CompareScreen extends ConsumerStatefulWidget {
  const CompareScreen({super.key, this.initialSlugs = const []});

  /// Drinks to start with, usually the one the viewer was already looking at.
  final List<String> initialSlugs;

  /// Optional override, used by tests. Production opens [DrinkDetailScreen].
  static DrinkOpener? drinkOpener;

  static const int maxDrinks = 6;
  static const int minDrinks = 2;

  static Future<void> open(
    BuildContext context, {
    List<String> slugs = const [],
  }) {
    return Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => CompareScreen(initialSlugs: slugs),
      ),
    );
  }

  @override
  ConsumerState<CompareScreen> createState() => _CompareScreenState();
}

class _CompareScreenState extends ConsumerState<CompareScreen> {
  final _query = TextEditingController();

  List<Drink> _selected = [];
  List<Drink> _matches = const [];
  List<Drink> _comparison = const [];

  bool _searching = false;
  bool _searched = false;
  bool _comparing = false;

  String? _searchError;
  String? _compareError;
  String? _notice;

  /// Bumped whenever the line-up changes so a slow response cannot replace a
  /// newer one.
  int _requestId = 0;

  @override
  void initState() {
    super.initState();
    final slugs = _cleanSlugs(widget.initialSlugs);
    if (slugs.isEmpty) return;
    _selected = [
      for (final slug in slugs)
        Drink(id: 0, slug: slug, name: _nameFromSlug(slug)),
    ];
    if (slugs.length >= CompareScreen.minDrinks) {
      _comparing = true;
      _loadInitial(slugs);
    }
  }

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _loadInitial(List<String> slugs) async {
    final request = ++_requestId;
    try {
      final rows = await ref.read(apiProvider).compare(slugs);
      if (!mounted || request != _requestId) return;
      setState(() {
        _comparing = false;
        _comparison = rows;
        _selected = _hydrate(rows);
        if (rows.isEmpty) {
          _compareError = 'That comparison came back empty. Try again.';
        }
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      setState(() {
        _comparing = false;
        _compareError = _describeCompare(error);
      });
    }
  }

  Future<void> _runCompare() async {
    if (_comparing) return;
    if (_selected.length < CompareScreen.minDrinks) {
      setState(() => _notice = 'Choose at least two drinks to compare.');
      return;
    }
    final request = ++_requestId;
    setState(() {
      _comparing = true;
      _comparison = const [];
      _compareError = null;
      _notice = null;
    });
    try {
      final rows = await ref.read(apiProvider).compare([
        for (final drink in _selected) drink.slug,
      ]);
      if (!mounted || request != _requestId) return;
      setState(() {
        _comparing = false;
        _comparison = rows;
        _selected = _hydrate(rows);
        if (rows.isEmpty) {
          _compareError = 'That comparison came back empty. Try again.';
        }
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      setState(() {
        _comparing = false;
        _compareError = _describeCompare(error);
      });
    }
  }

  Future<void> _runSearch() async {
    if (_searching) return;
    final query = _query.text.trim();
    if (query.length < 2) {
      setState(() {
        _searchError = 'Type at least two characters.';
        _matches = const [];
        _searched = false;
      });
      return;
    }
    FocusScope.of(context).unfocus();
    setState(() {
      _searching = true;
      _searched = true;
      _searchError = null;
    });
    try {
      final rows = await ref.read(apiProvider).search(query);
      if (!mounted) return;
      setState(() {
        _searching = false;
        _matches = rows;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _searching = false;
        _matches = const [];
        _searchError = ApiClient.describe(error).message;
      });
    }
  }

  void _add(Drink drink) {
    if (_selected.any((picked) => _sameDrink(picked, drink))) return;
    if (_selected.length >= CompareScreen.maxDrinks) {
      setState(
        () => _notice =
            'You can compare six drinks at a time. Remove one to add another.',
      );
      return;
    }
    setState(() {
      _selected = [..._selected, drink];
      _query.clear();
      _matches = const [];
      _searched = false;
      _searchError = null;
      _notice = null;
      _invalidateComparison();
    });
  }

  void _remove(Drink drink) {
    setState(() {
      _selected = [
        for (final picked in _selected)
          if (!_sameDrink(picked, drink)) picked,
      ];
      _notice = null;
      _searchError = null;
      _invalidateComparison();
    });
  }

  /// Selection changed, so the old table no longer describes it.
  void _invalidateComparison() {
    _requestId++;
    _comparing = false;
    _comparison = const [];
    _compareError = null;
  }

  List<Drink> _hydrate(List<Drink> rows) {
    final bySlug = <String, Drink>{};
    final byId = <int, Drink>{};
    for (final row in rows) {
      if (row.slug.isNotEmpty) bySlug[row.slug] = row;
      if (row.id != 0) byId[row.id] = row;
    }
    return [
      for (final picked in _selected)
        bySlug[picked.slug] ??
            (picked.id == 0 ? null : byId[picked.id]) ??
            picked,
    ];
  }

  Future<void> _openDrink(Drink drink) async {
    final override = CompareScreen.drinkOpener;
    if (override != null) {
      await override(context, drink.slug, name: drink.name);
      return;
    }
    await DrinkDetailScreen.open(context, drink.slug, name: drink.name);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final canCompare =
        _selected.length >= CompareScreen.minDrinks && !_comparing;
    return Scaffold(
      appBar: AppBar(title: const Text('Compare sodas')),
      body: SafeArea(
        top: false,
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(16, 4, 16, 40),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _searchCard(theme),
              const SizedBox(height: 20),
              _selectionSection(theme),
              const SizedBox(height: 16),
              SizedBox(
                height: 52,
                child: FilledButton.icon(
                  key: const Key('compare-button'),
                  onPressed: canCompare ? _runCompare : null,
                  icon: const Icon(Icons.compare_arrows_rounded, size: 20),
                  label: Text(_comparing ? 'Comparing…' : 'Compare'),
                ),
              ),
              if (_compareError != null) ...[
                const SizedBox(height: 12),
                _ErrorNote(message: _compareError!, onRetry: _runCompare),
              ],
              const SizedBox(height: 26),
              _resultsSection(theme),
            ],
          ),
        ),
      ),
    );
  }

  Widget _searchCard(ThemeData theme) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 14, 14, 8),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TextField(
              key: const Key('compare-search'),
              controller: _query,
              enabled: !_searching,
              textInputAction: TextInputAction.search,
              onSubmitted: (_) => _runSearch(),
              decoration: InputDecoration(
                hintText: 'Search by name or brand',
                suffixIcon: IconButton(
                  onPressed: _searching ? null : _runSearch,
                  tooltip: 'Search',
                  icon: const Icon(Icons.search_rounded),
                ),
              ),
            ),
            if (_searching)
              Padding(
                padding: const EdgeInsets.only(top: 2),
                child: LinearProgressIndicator(
                  minHeight: 2,
                  color: theme.colorScheme.primary,
                  backgroundColor: Colors.transparent,
                ),
              ),
            if (_searchError != null)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  _searchError!,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.error,
                  ),
                ),
              ),
            if (_matches.isNotEmpty) ...[
              const SizedBox(height: 10),
              for (var index = 0; index < _matches.length; index++) ...[
                if (index > 0)
                  Divider(
                    height: 1,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
                  ),
                _matchTile(theme, _matches[index]),
              ],
            ] else if (_searched && !_searching && _searchError == null) ...[
              const SizedBox(height: 10),
              Text(
                'No sodas matched.',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _matchTile(ThemeData theme, Drink drink) {
    final added = _selected.any((picked) => _sameDrink(picked, drink));
    final subtitle = [
      drink.brand,
      drink.style,
    ].where((value) => value.trim().isNotEmpty).join(' · ');
    return ListTile(
      key: ValueKey('compare-result-${drink.slug}'),
      contentPadding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
      title: Text(
        drink.name,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        style: const TextStyle(fontWeight: FontWeight.w700),
      ),
      subtitle: subtitle.isEmpty ? null : Text(subtitle),
      trailing: added
          ? Text(
              'Added',
              style: theme.textTheme.labelSmall?.copyWith(
                fontWeight: FontWeight.w700,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            )
          : IconButton(
              onPressed: () => _add(drink),
              tooltip: 'Add ${drink.name}',
              icon: const Icon(Icons.add_circle_outline_rounded),
            ),
      onTap: added ? null : () => _add(drink),
    );
  }

  Widget _selectionSection(ThemeData theme) {
    final count = _selected.length;
    final atLimit = count >= CompareScreen.maxDrinks;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaSectionTitle(
          title: 'Your line-up',
          trailing: '$count of ${CompareScreen.maxDrinks}',
        ),
        const SizedBox(height: 10),
        if (_selected.isEmpty)
          Text(
            'Add two to six drinks to see them side by side.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          )
        else
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final drink in _selected) _selectedChip(theme, drink),
            ],
          ),
        if (atLimit)
          Padding(
            padding: const EdgeInsets.only(top: 10),
            child: Text(
              'Six selected. Remove one to add a different drink.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        if (_notice != null)
          Container(
            margin: const EdgeInsets.only(top: 10),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            decoration: BoxDecoration(
              color: theme.colorScheme.secondary.withValues(alpha: 0.1),
              borderRadius: BorderRadius.circular(12),
            ),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  Icons.info_outline_rounded,
                  size: 18,
                  color: theme.colorScheme.secondary,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    _notice!,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurface,
                    ),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }

  Widget _selectedChip(ThemeData theme, Drink drink) {
    return InputChip(
      key: ValueKey('compare-chip-${drink.slug}'),
      label: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 210),
        child: Text(drink.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      ),
      onPressed: () => _openDrink(drink),
      onDeleted: () => _remove(drink),
      deleteButtonTooltipMessage: 'Remove ${drink.name}',
      deleteIcon: const Icon(Icons.close_rounded, size: 18),
      showCheckmark: false,
      backgroundColor: theme.colorScheme.primary.withValues(alpha: 0.18),
      side: BorderSide(
        color: theme.colorScheme.primary.withValues(alpha: 0.45),
      ),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(999)),
      labelStyle: theme.textTheme.labelLarge?.copyWith(
        fontWeight: FontWeight.w600,
        color: theme.colorScheme.onSurface,
      ),
    );
  }

  Widget _resultsSection(ThemeData theme) {
    // A failed comparison already has its own error note and retry above; an
    // empty table underneath would just repeat the lack of data.
    if (!_comparing && _comparison.isEmpty && _compareError != null) {
      return const SizedBox.shrink();
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SodaSectionTitle(
          title: 'Side by side',
          trailing: _comparison.isEmpty
              ? null
              : sodaCount(_comparison.length, 'drink'),
        ),
        const SizedBox(height: 10),
        if (_comparing)
          Card(
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: SodaLoadingView(dense: true, label: 'Comparing drinks'),
            ),
          )
        else if (_comparison.isNotEmpty)
          _CompareTable(drinks: _comparison, onOpenDrink: _openDrink)
        else
          Card(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              child: SodaStatusView(
                dense: true,
                icon: Icons.view_column_outlined,
                title: 'Nothing to compare yet',
                message: 'Pick two to six drinks above.',
              ),
            ),
          ),
      ],
    );
  }

  String _describeCompare(Object error) {
    final described = ApiClient.describe(error);
    if (described.statusCode == 400) {
      return 'Choose between two and six drinks to compare.';
    }
    return described.message;
  }
}

/// One attribute row shared by every drink column.
class _Attribute {
  const _Attribute(
    this.label,
    this.read, {
    this.height = 52,
    this.maxLines = 2,
  });

  final String label;
  final String Function(Drink drink) read;
  final double height;
  final int maxLines;
}

final List<_Attribute> _attributes = [
  _Attribute('Brand', (drink) => _orDash(drink.brand)),
  _Attribute('Style', (drink) => _orDash(drink.style)),
  _Attribute('Container', (drink) => _human(drink.container)),
  _Attribute(
    'Volume',
    (drink) => drink.volumeMl > 0 ? '${drink.volumeMl} ml' : '—',
  ),
  _Attribute(
    'Caffeine / 100 ml',
    (drink) =>
        drink.caffeineMgPer100ml > 0 ? '${drink.caffeineMgPer100ml} mg' : '—',
    height: 56,
  ),
  _Attribute(
    'Sugar / 100 ml',
    (drink) =>
        drink.sugarGPer100ml > 0 ? '${_number(drink.sugarGPer100ml)} g' : '—',
    height: 56,
  ),
  _Attribute(
    'Calories / 100 ml',
    (drink) => drink.caloriesKcalPer100ml > 0
        ? '${drink.caloriesKcalPer100ml} kcal'
        : '—',
    height: 56,
  ),
  _Attribute('Sweetener', (drink) => _human(drink.sweetener)),
  _Attribute('Carbonation', (drink) => _human(drink.carbonation)),
  _Attribute('Acid', (drink) => _human(drink.acid)),
  _Attribute('Status', (drink) => _human(drink.status)),
  _Attribute('Average rating', _rating, height: 64, maxLines: 3),
  _Attribute('Flavours', _flavours, height: 64, maxLines: 3),
];

String _orDash(String value) {
  final text = value.trim();
  return text.isEmpty ? '—' : text;
}

String _human(String value) {
  final text = value.replaceAll('_', ' ').trim();
  if (text.isEmpty) return '—';
  return text[0].toUpperCase() + text.substring(1);
}

String _number(double value) {
  if (value == value.roundToDouble()) return '${value.toInt()}';
  return value.toStringAsFixed(1);
}

String _rating(Drink drink) {
  if (!drink.ratingShown) return 'Not enough ratings';
  final value = drink.rating;
  if (value == null) return '—';
  return '${value.toStringAsFixed(1)} · ${sodaCount(drink.raterCount, 'rating')}';
}

String _flavours(Drink drink) {
  final names = [
    for (final flavour in drink.flavours)
      if (flavour.name.trim().isNotEmpty) flavour.name.trim(),
  ];
  return names.isEmpty ? '—' : names.join(', ');
}

List<String> _cleanSlugs(List<String> raw) {
  final seen = <String>{};
  final slugs = <String>[];
  for (final value in raw) {
    final slug = value.trim();
    if (slug.isEmpty || !seen.add(slug)) continue;
    slugs.add(slug);
    if (slugs.length == CompareScreen.maxDrinks) break;
  }
  return slugs;
}

String _nameFromSlug(String slug) {
  final words = slug
      .replaceAll(RegExp(r'[_\-]+'), ' ')
      .trim()
      .split(RegExp(r'\s+'));
  return [
    for (final word in words)
      if (word.isNotEmpty) word[0].toUpperCase() + word.substring(1),
  ].join(' ');
}

bool _sameDrink(Drink a, Drink b) {
  if (a.slug.isNotEmpty && b.slug.isNotEmpty) return a.slug == b.slug;
  return a.id != 0 && a.id == b.id;
}

class _CompareTable extends StatelessWidget {
  const _CompareTable({required this.drinks, required this.onOpenDrink});

  final List<Drink> drinks;
  final ValueChanged<Drink> onOpenDrink;

  static const double _headerHeight = 76;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scale = MediaQuery.textScalerOf(context).scale(1);
    final line = theme.colorScheme.onSurface.withValues(alpha: 0.1);
    final headerTint = theme.colorScheme.primary.withValues(alpha: 0.12);
    final labelFill = theme.colorScheme.onSurface.withValues(alpha: 0.035);

    return LayoutBuilder(
      builder: (context, constraints) {
        final width = constraints.maxWidth;
        final wide = width >= 700;
        final labelWidth = wide ? 150.0 : 112.0;
        final available = math.max(0.0, width - labelWidth);
        final minColumn = (wide ? 96.0 : 138.0) * math.min(scale, 1.25);
        final columnWidth = wide
            ? math.max(minColumn, available / drinks.length)
            : minColumn;
        final totalWidth = columnWidth * drinks.length;
        final scrolls = totalWidth > available + 0.5;
        final headerHeight = _headerHeight * scale;
        final tableHeight =
            headerHeight +
            _attributes.fold<double>(0, (sum, row) => sum + row.height * scale);

        final columns = Row(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (var index = 0; index < drinks.length; index++)
              _drinkColumn(
                context,
                theme,
                drinks[index],
                columnWidth,
                index == drinks.length - 1,
                headerHeight,
                headerTint,
                line,
                scale,
              ),
          ],
        );

        final table = SizedBox(
          height: tableHeight,
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              SizedBox(
                width: labelWidth,
                child: ColoredBox(
                  color: labelFill,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      _labelHeader(theme, headerHeight, headerTint),
                      for (final attribute in _attributes)
                        _labelCell(
                          theme,
                          attribute,
                          attribute.height * scale,
                          line,
                        ),
                    ],
                  ),
                ),
              ),
              Expanded(
                child: scrolls
                    ? SingleChildScrollView(
                        scrollDirection: Axis.horizontal,
                        child: SizedBox(width: totalWidth, child: columns),
                      )
                    : columns,
              ),
            ],
          ),
        );

        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (scrolls)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(
                  children: [
                    Icon(
                      Icons.swipe_left_rounded,
                      size: 15,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        'Swipe the table to see every drink.',
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            Card(clipBehavior: Clip.antiAlias, child: table),
          ],
        );
      },
    );
  }

  Widget _labelHeader(ThemeData theme, double height, Color tint) {
    return Container(
      height: height,
      alignment: Alignment.bottomLeft,
      padding: const EdgeInsets.fromLTRB(14, 8, 10, 12),
      decoration: BoxDecoration(
        color: tint,
        border: Border(
          bottom: BorderSide(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
          ),
          right: BorderSide(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.14),
          ),
        ),
      ),
      child: Text(
        'Drink',
        style: theme.textTheme.labelSmall?.copyWith(
          fontWeight: FontWeight.w800,
          letterSpacing: 0.6,
          color: theme.colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }

  Widget _labelCell(
    ThemeData theme,
    _Attribute attribute,
    double height,
    Color line,
  ) {
    return Container(
      height: height,
      alignment: Alignment.centerLeft,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
      decoration: BoxDecoration(
        border: Border(
          bottom: BorderSide(color: line),
          right: BorderSide(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.14),
          ),
        ),
      ),
      child: Text(
        attribute.label,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        style: theme.textTheme.labelSmall?.copyWith(
          fontWeight: FontWeight.w700,
          letterSpacing: 0.4,
          height: 1.2,
          color: theme.colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }

  Widget _drinkColumn(
    BuildContext context,
    ThemeData theme,
    Drink drink,
    double width,
    bool last,
    double headerHeight,
    Color headerTint,
    Color line,
    double scale,
  ) {
    final border = Border(
      right: last ? BorderSide.none : BorderSide(color: line),
    );
    return SizedBox(
      width: width,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _headerCell(
            context,
            theme,
            drink,
            width,
            headerHeight,
            headerTint,
            line,
            border,
          ),
          for (final attribute in _attributes)
            _valueCell(
              theme,
              drink,
              attribute,
              attribute.height * scale,
              line,
              border,
            ),
        ],
      ),
    );
  }

  Widget _headerCell(
    BuildContext context,
    ThemeData theme,
    Drink drink,
    double width,
    double height,
    Color tint,
    Color line,
    Border border,
  ) {
    final content = Padding(
      padding: const EdgeInsets.fromLTRB(12, 8, 10, 8),
      child: Align(
        alignment: Alignment.bottomLeft,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              drink.name,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodyMedium?.copyWith(
                fontWeight: FontWeight.w800,
                height: 1.15,
              ),
            ),
            if (drink.brand.trim().isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                drink.brand,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      ),
    );

    final cell = Container(
      height: height,
      decoration: BoxDecoration(
        color: tint,
        border: Border(
          bottom: BorderSide(color: line),
          right: border.right,
        ),
      ),
      child: content,
    );

    return Semantics(
      button: true,
      label: 'Open ${drink.name}',
      excludeSemantics: true,
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          key: ValueKey('compare-header-${drink.slug}'),
          onTap: () => onOpenDrink(drink),
          child: cell,
        ),
      ),
    );
  }

  Widget _valueCell(
    ThemeData theme,
    Drink drink,
    _Attribute attribute,
    double height,
    Color line,
    Border border,
  ) {
    final value = attribute.read(drink);
    return Semantics(
      container: true,
      label: '${drink.name}, ${attribute.label}: $value',
      excludeSemantics: true,
      child: Container(
        height: height,
        alignment: Alignment.centerLeft,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
        decoration: BoxDecoration(
          border: Border(
            bottom: BorderSide(color: line),
            right: border.right,
          ),
        ),
        child: Text(
          value,
          maxLines: attribute.maxLines,
          overflow: TextOverflow.ellipsis,
          style: theme.textTheme.bodySmall?.copyWith(
            height: 1.3,
            fontWeight: FontWeight.w600,
            color: theme.colorScheme.onSurface,
          ),
        ),
      ),
    );
  }
}

class _ErrorNote extends StatelessWidget {
  const _ErrorNote({required this.message, this.onRetry});

  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.error.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          Icon(
            Icons.error_outline_rounded,
            size: 18,
            color: theme.colorScheme.error,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.error,
              ),
            ),
          ),
          if (onRetry != null)
            TextButton(onPressed: onRetry, child: const Text('Retry')),
        ],
      ),
    );
  }
}
