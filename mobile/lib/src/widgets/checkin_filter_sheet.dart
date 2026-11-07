import 'package:flutter/material.dart';

import '../utils/url.dart';

/// One immutable set of check-in filters. The profile sends the concrete
/// `from`/`to` dates it gets back, so every page of a paginated list uses the
/// same window even when the sheet is reopened later.
class CheckinFilter {
  const CheckinFilter({
    this.range = 'all',
    this.from,
    this.to,
    this.ratingMin,
    this.ratingMax,
    this.minLikes,
    this.hasPhoto = false,
    this.visibility,
    this.sort = 'newest',
  });

  /// `all`, `7d`, `30d`, `90d`, `year` or `custom`.
  final String range;
  final String? from;
  final String? to;

  /// Quarter-star bounds 0–20. Null means the bound is open.
  final int? ratingMin;
  final int? ratingMax;
  final int? minLikes;
  final bool hasPhoto;

  /// `public`, `friends` or `private`; null means every visibility.
  final String? visibility;

  /// `newest`, `oldest`, `rating_high`, `rating_low` or `likes`.
  final String sort;

  bool get isActive =>
      range != 'all' ||
      ratingMin != null ||
      ratingMax != null ||
      minLikes != null ||
      hasPhoto ||
      visibility != null ||
      sort != 'newest';

  int get activeCount => [
    range != 'all',
    ratingMin != null || ratingMax != null,
    minLikes != null,
    hasPhoto,
    visibility != null,
    sort != 'newest',
  ].where((active) => active).length;

  String get rangeLabel {
    switch (range) {
      case '7d':
        return 'Last 7 days';
      case '30d':
        return 'Last 30 days';
      case '90d':
        return 'Last 90 days';
      case 'year':
        return 'This year';
      case 'custom':
        final start = _shortDate(from);
        final end = _shortDate(to);
        if (start != null && end != null) return '$start – $end';
        if (start != null) return 'From $start';
        if (end != null) return 'Until $end';
        return 'Custom dates';
      default:
        return 'All time';
    }
  }

  /// Short labels used by the active-filter chips under the section heading.
  List<String> get chipLabels => [
    if (range != 'all') rangeLabel,
    if (ratingMin != null || ratingMax != null) ratingChip,
    if (minLikes != null) likesChip,
    if (hasPhoto) photoChip,
    if (visibility != null) visibilityChip,
    if (sort != 'newest') sortChip,
  ];

  String get ratingChip {
    final low = ratingMin;
    final high = ratingMax;
    String star(int quarters) => quarters <= 0
        ? '0'
        : ratingLabel(quarters).replaceFirst('Not rated', '0');
    if (low != null && high != null) {
      return '★ ${star(low)}–${star(high)}';
    }
    if (low != null) return '★ ${star(low)}+';
    return '★ up to ${star(high ?? 20)}';
  }

  String get likesChip => '${minLikes ?? 0}+ toasts';

  String get photoChip => 'With photo';

  String get visibilityChip =>
      visibility == null ? '' : _visibilityLabel(visibility!);

  String get sortChip => _sortLabel(sort);

  CheckinFilter withoutRange() => CheckinFilter(
    ratingMin: ratingMin,
    ratingMax: ratingMax,
    minLikes: minLikes,
    hasPhoto: hasPhoto,
    visibility: visibility,
    sort: sort,
  );

  CheckinFilter withoutRating() => CheckinFilter(
    range: range,
    from: from,
    to: to,
    minLikes: minLikes,
    hasPhoto: hasPhoto,
    visibility: visibility,
    sort: sort,
  );

  CheckinFilter withoutMinLikes() => CheckinFilter(
    range: range,
    from: from,
    to: to,
    ratingMin: ratingMin,
    ratingMax: ratingMax,
    hasPhoto: hasPhoto,
    visibility: visibility,
    sort: sort,
  );

  CheckinFilter withoutPhoto() => CheckinFilter(
    range: range,
    from: from,
    to: to,
    ratingMin: ratingMin,
    ratingMax: ratingMax,
    minLikes: minLikes,
    visibility: visibility,
    sort: sort,
  );

  CheckinFilter withoutVisibility() => CheckinFilter(
    range: range,
    from: from,
    to: to,
    ratingMin: ratingMin,
    ratingMax: ratingMax,
    minLikes: minLikes,
    hasPhoto: hasPhoto,
    sort: sort,
  );

  CheckinFilter withoutSort() => CheckinFilter(
    range: range,
    from: from,
    to: to,
    ratingMin: ratingMin,
    ratingMax: ratingMax,
    minLikes: minLikes,
    hasPhoto: hasPhoto,
    visibility: visibility,
  );

  static String? _shortDate(String? raw) {
    final value = raw == null || raw.trim().isEmpty
        ? null
        : DateTime.tryParse(raw);
    if (value == null) return null;
    const months = [
      'Jan',
      'Feb',
      'Mar',
      'Apr',
      'May',
      'Jun',
      'Jul',
      'Aug',
      'Sep',
      'Oct',
      'Nov',
      'Dec',
    ];
    return '${value.day} ${months[value.month - 1]}';
  }

  static String _visibilityLabel(String value) {
    switch (value) {
      case 'public':
        return 'Anyone';
      case 'friends':
        return 'Friends';
      case 'private':
        return 'Only you';
      default:
        return value;
    }
  }

  static String _sortLabel(String value) {
    switch (value) {
      case 'oldest':
        return 'Oldest first';
      case 'rating_high':
        return 'Highest rated';
      case 'rating_low':
        return 'Lowest rated';
      case 'likes':
        return 'Most toasted';
      default:
        return 'Newest first';
    }
  }
}

Future<CheckinFilter?> showCheckinFilterSheet(
  BuildContext context, {
  required CheckinFilter initial,
}) {
  return showModalBottomSheet<CheckinFilter>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    builder: (_) => _CheckinFilterSheet(initial: initial),
  );
}

class _CheckinFilterSheet extends StatefulWidget {
  const _CheckinFilterSheet({required this.initial});

  final CheckinFilter initial;

  @override
  State<_CheckinFilterSheet> createState() => _CheckinFilterSheetState();
}

class _CheckinFilterSheetState extends State<_CheckinFilterSheet> {
  late String _range;
  DateTime? _from;
  DateTime? _to;
  int? _ratingMin;
  int? _ratingMax;
  int? _minLikes;
  late bool _hasPhoto;
  String? _visibility;
  late String _sort;

  @override
  void initState() {
    super.initState();
    final initial = widget.initial;
    _range = initial.range;
    _from = _parseDate(initial.from);
    _to = _parseDate(initial.to);
    _ratingMin = initial.ratingMin;
    _ratingMax = initial.ratingMax;
    _minLikes = initial.minLikes;
    _hasPhoto = initial.hasPhoto;
    _visibility = initial.visibility;
    _sort = initial.sort;
  }

  static DateTime? _parseDate(String? raw) {
    if (raw == null || raw.trim().isEmpty) return null;
    return DateTime.tryParse(raw);
  }

  void _reset() {
    setState(() {
      _range = 'all';
      _from = null;
      _to = null;
      _ratingMin = null;
      _ratingMax = null;
      _minLikes = null;
      _hasPhoto = false;
      _visibility = null;
      _sort = 'newest';
    });
  }

  void _apply() {
    final now = DateTime.now();
    final today = DateTime(now.year, now.month, now.day);
    DateTime? from;
    DateTime? to;
    switch (_range) {
      case '7d':
        from = today.subtract(const Duration(days: 6));
        break;
      case '30d':
        from = today.subtract(const Duration(days: 29));
        break;
      case '90d':
        from = today.subtract(const Duration(days: 89));
        break;
      case 'year':
        from = DateTime(now.year);
        break;
      case 'custom':
        from = _from;
        to = _to;
        break;
    }
    final min = _ratingMin == null || _ratingMin! <= 0 ? null : _ratingMin;
    final max = _ratingMax == null || _ratingMax! >= 20 ? null : _ratingMax;
    Navigator.pop(
      context,
      CheckinFilter(
        range: _range,
        from: _isoDate(from),
        to: _isoDate(to),
        ratingMin: min,
        ratingMax: max,
        minLikes: _minLikes,
        hasPhoto: _hasPhoto,
        visibility: _visibility,
        sort: _sort,
      ),
    );
  }

  static String? _isoDate(DateTime? value) {
    if (value == null) return null;
    final month = value.month.toString().padLeft(2, '0');
    final day = value.day.toString().padLeft(2, '0');
    return '${value.year}-$month-$day';
  }

  Future<void> _pickDate({required bool start}) async {
    final now = DateTime.now();
    final initial = start ? (_from ?? now) : (_to ?? _from ?? now);
    final picked = await showDatePicker(
      context: context,
      initialDate: initial,
      firstDate: DateTime(2000),
      lastDate: now,
    );
    if (picked == null || !mounted) return;
    setState(() {
      _range = 'custom';
      if (start) {
        _from = picked;
        if (_to != null && picked.isAfter(_to!)) _to = picked;
      } else {
        _to = picked;
        if (_from != null && picked.isBefore(_from!)) _from = picked;
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final media = MediaQuery.of(context);
    return Padding(
      padding: EdgeInsets.only(bottom: media.viewInsets.bottom),
      child: SafeArea(
        top: false,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: media.size.height * 0.86),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 12, 8),
                child: Row(
                  children: [
                    Expanded(
                      child: Text(
                        'Filter check-ins',
                        style: theme.textTheme.titleLarge?.copyWith(
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                    ),
                    IconButton(
                      tooltip: 'Close',
                      onPressed: () => Navigator.pop(context),
                      icon: const Icon(Icons.close_rounded),
                    ),
                  ],
                ),
              ),
              Flexible(
                child: SingleChildScrollView(
                  padding: const EdgeInsets.fromLTRB(20, 4, 20, 16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      _section(
                        theme,
                        'When',
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            for (final option in const [
                              ('all', 'All time'),
                              ('7d', '7 days'),
                              ('30d', '30 days'),
                              ('90d', '90 days'),
                              ('year', 'This year'),
                              ('custom', 'Custom'),
                            ])
                              ChoiceChip(
                                key: Key('filter-range-${option.$1}'),
                                label: Text(option.$2),
                                selected: _range == option.$1,
                                onSelected: (_) =>
                                    setState(() => _range = option.$1),
                              ),
                          ],
                        ),
                      ),
                      if (_range == 'custom') ...[
                        const SizedBox(height: 10),
                        Row(
                          children: [
                            Expanded(
                              child: OutlinedButton.icon(
                                key: const Key('filter-from'),
                                onPressed: () => _pickDate(start: true),
                                icon: const Icon(
                                  Icons.calendar_today_outlined,
                                  size: 16,
                                ),
                                label: Text(
                                  _from == null
                                      ? 'From'
                                      : _isoDate(_from) ?? 'From',
                                ),
                              ),
                            ),
                            const SizedBox(width: 10),
                            Expanded(
                              child: OutlinedButton.icon(
                                key: const Key('filter-to'),
                                onPressed: () => _pickDate(start: false),
                                icon: const Icon(
                                  Icons.calendar_today_outlined,
                                  size: 16,
                                ),
                                label: Text(
                                  _to == null ? 'To' : _isoDate(_to) ?? 'To',
                                ),
                              ),
                            ),
                          ],
                        ),
                      ],
                      const SizedBox(height: 22),
                      _section(
                        theme,
                        'Rating',
                        Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              _ratingText,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.onSurfaceVariant,
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                            RangeSlider(
                              key: const Key('filter-rating'),
                              min: 0,
                              max: 20,
                              divisions: 10,
                              values: RangeValues(
                                (_ratingMin ?? 0).toDouble(),
                                (_ratingMax ?? 20).toDouble(),
                              ),
                              labels: RangeLabels(
                                _star(_ratingMin ?? 0),
                                _star(_ratingMax ?? 20),
                              ),
                              onChanged: (values) => setState(() {
                                _ratingMin = values.start.round();
                                _ratingMax = values.end.round();
                              }),
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 18),
                      _section(
                        theme,
                        'Minimum toasts',
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            for (final option in const [
                              (null, 'Any'),
                              (1, '1+'),
                              (5, '5+'),
                              (10, '10+'),
                              (25, '25+'),
                            ])
                              ChoiceChip(
                                key: Key('filter-likes-${option.$1 ?? 'any'}'),
                                label: Text(option.$2),
                                selected: _minLikes == option.$1,
                                onSelected: (_) =>
                                    setState(() => _minLikes = option.$1),
                              ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 18),
                      SwitchListTile(
                        key: const Key('filter-photo'),
                        contentPadding: EdgeInsets.zero,
                        title: const Text('Only check-ins with a photo'),
                        value: _hasPhoto,
                        onChanged: (value) => setState(() => _hasPhoto = value),
                      ),
                      const SizedBox(height: 10),
                      _section(
                        theme,
                        'Visibility',
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            for (final option in const [
                              (null, 'Any'),
                              ('public', 'Anyone'),
                              ('friends', 'Friends'),
                              ('private', 'Only you'),
                            ])
                              ChoiceChip(
                                key: Key(
                                  'filter-visibility-${option.$1 ?? 'any'}',
                                ),
                                label: Text(option.$2),
                                selected: _visibility == option.$1,
                                onSelected: (_) =>
                                    setState(() => _visibility = option.$1),
                              ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 18),
                      _section(
                        theme,
                        'Sort',
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            for (final option in const [
                              ('newest', 'Newest'),
                              ('oldest', 'Oldest'),
                              ('rating_high', 'Highest rated'),
                              ('rating_low', 'Lowest rated'),
                              ('likes', 'Most toasted'),
                            ])
                              ChoiceChip(
                                key: Key('filter-sort-${option.$1}'),
                                label: Text(option.$2),
                                selected: _sort == option.$1,
                                onSelected: (_) =>
                                    setState(() => _sort = option.$1),
                              ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              const Divider(height: 1),
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 12, 20, 12),
                child: Row(
                  children: [
                    TextButton(
                      key: const Key('filter-reset'),
                      onPressed: _reset,
                      child: const Text('Reset'),
                    ),
                    const Spacer(),
                    FilledButton(
                      key: const Key('filter-apply'),
                      onPressed: _apply,
                      child: const Text('Apply filters'),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  String get _ratingText {
    if (_ratingMin == null && _ratingMax == null) return 'Any rating';
    if (_ratingMin != null && _ratingMax != null) {
      return '${_star(_ratingMin!)} to ${_star(_ratingMax!)} stars';
    }
    if (_ratingMin != null) return '${_star(_ratingMin!)} stars and up';
    return 'Up to ${_star(_ratingMax!)} stars';
  }

  static String _star(int quarters) {
    if (quarters <= 0) return '0';
    return ratingLabel(quarters);
  }

  Widget _section(ThemeData theme, String title, Widget child) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          title,
          style: theme.textTheme.labelLarge?.copyWith(
            fontWeight: FontWeight.w800,
            color: theme.colorScheme.onSurface,
          ),
        ),
        const SizedBox(height: 8),
        child,
      ],
    );
  }
}
