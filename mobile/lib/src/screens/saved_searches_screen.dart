import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

/// The beverage types the server understands, in the same order and wording as
/// the web form. An empty value means "any type", which is why it leads.
const savedSearchBeverageTypes = <(String, String)>[
  ('', 'Any type'),
  ('soda', 'Soda'),
  ('water', 'Water'),
  ('tea', 'Tea'),
  ('coffee', 'Coffee'),
  ('na_beer', 'NA beer'),
  ('na_wine', 'NA wine'),
  ('na_spirit', 'NA spirits'),
  ('other', 'Other'),
];

/// The per-member saved-search cap the server enforces (429/400 `limit_reached`).
const savedSearchLimit = 25;

/// Human label for a stored beverage type, falling back to the raw value.
String savedSearchBeverageLabel(String value) {
  for (final (slug, label) in savedSearchBeverageTypes) {
    if (slug == value) return label;
  }
  return value;
}

/// Readable filter pill text: "cold-brew" -> "Cold brew", matching the web's
/// humanised slug rendering.
String savedSearchFilterLabel(String slug) {
  var value = slug.replaceAll('-', ' ').replaceAll('_', ' ').trim();
  if (value.isEmpty) return value;
  value = value[0].toUpperCase() + value.substring(1);
  return value;
}

/// The readable pills for one stored search, in the same order as the form so
/// a row reads top-to-bottom the way it was filled in.
List<String> savedSearchPills(SavedSearch search) {
  final pills = <String>[];
  final query = search.query.trim();
  if (query.isNotEmpty) pills.add('“$query”');
  if (search.beverageType.trim().isNotEmpty) {
    pills.add(savedSearchBeverageLabel(search.beverageType));
  }
  for (final (prefix, slug) in [
    ('Style · ', search.styleSlug),
    ('Brand · ', search.brandSlug),
    ('Flavour · ', search.flavourSlug),
  ]) {
    final value = (slug ?? '').trim();
    if (value.isNotEmpty) pills.add('$prefix${savedSearchFilterLabel(value)}');
  }
  if (search.caffeineFree) pills.add('Caffeine-free');
  if (search.sugarFree) pills.add('Sugar-free');
  return pills;
}

/// Standing drop alerts: create, list and delete. There is no update endpoint
/// and no run action — a saved search is a subscription the server evaluates
/// when a drink enters the catalog, exactly like the web page.
class SavedSearchesScreen extends ConsumerStatefulWidget {
  const SavedSearchesScreen({
    super.key,
    this.initialQuery = '',
    this.initialBeverageType = '',
    this.initialStyleSlug = '',
    this.initialBrandSlug = '',
    this.initialFlavourSlug = '',
  });

  /// Seeds for the create form. Quick-save passes [initialQuery]; the
  /// from-drink prefill passes the catalog slugs below.
  final String initialQuery;
  final String initialBeverageType;
  final String initialStyleSlug;
  final String initialBrandSlug;
  final String initialFlavourSlug;

  static Future<void> open(
    BuildContext context, {
    String query = '',
    String beverageType = '',
    String styleSlug = '',
    String brandSlug = '',
    String flavourSlug = '',
  }) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => SavedSearchesScreen(
          initialQuery: query,
          initialBeverageType: beverageType,
          initialStyleSlug: styleSlug,
          initialBrandSlug: brandSlug,
          initialFlavourSlug: flavourSlug,
        ),
      ),
    );
  }

  @override
  ConsumerState<SavedSearchesScreen> createState() =>
      _SavedSearchesScreenState();
}

class _SavedSearchesScreenState extends ConsumerState<SavedSearchesScreen> {
  late final _name = TextEditingController();
  late final _query = TextEditingController(text: widget.initialQuery);
  late final _style = TextEditingController(text: widget.initialStyleSlug);
  late final _brand = TextEditingController(text: widget.initialBrandSlug);
  late final _flavour = TextEditingController(text: widget.initialFlavourSlug);

  late String _beverageType = _validBeverageType(widget.initialBeverageType);
  bool _caffeineFree = false;
  bool _sugarFree = false;
  bool _notifyDrops = true;

  List<SavedSearch> _searches = const [];
  bool _loading = true;
  String? _error;

  bool _creating = false;
  String? _createError;
  int? _deletingId;

  static String _validBeverageType(String value) {
    for (final (slug, _) in savedSearchBeverageTypes) {
      if (slug == value) return value;
    }
    return '';
  }

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _name.dispose();
    _query.dispose();
    _style.dispose();
    _brand.dispose();
    _flavour.dispose();
    super.dispose();
  }

  void _notify(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _load({bool showSpinner = true}) async {
    if (showSpinner) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final rows = await ref.read(apiProvider).savedSearches();
      if (!mounted) return;
      setState(() {
        _searches = rows;
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

  Future<void> _create() async {
    if (_creating) return;

    final query = _query.text.trim();
    final style = _style.text.trim();
    final brand = _brand.text.trim();
    final flavour = _flavour.text.trim();
    if (!hasSavedSearchFilter(
      query: query,
      beverageType: _beverageType,
      styleSlug: style,
      brandSlug: brand,
      flavourSlug: flavour,
      caffeineFree: _caffeineFree,
      sugarFree: _sugarFree,
    )) {
      setState(() => _createError = 'Add at least one filter before saving.');
      return;
    }

    setState(() {
      _creating = true;
      _createError = null;
    });
    try {
      final created = await ref
          .read(apiProvider)
          .createSavedSearch(
            name: _name.text.trim(),
            query: query,
            beverageType: _beverageType,
            styleSlug: style,
            brandSlug: brand,
            flavourSlug: flavour,
            caffeineFree: _caffeineFree,
            sugarFree: _sugarFree,
            notifyDrops: _notifyDrops,
          );
      if (!mounted) return;
      setState(() {
        _searches = [created, ..._searches];
        _creating = false;
        _name.clear();
        _query.clear();
        _style.clear();
        _brand.clear();
        _flavour.clear();
        _beverageType = '';
        _caffeineFree = false;
        _sugarFree = false;
        _notifyDrops = true;
      });
      FocusManager.instance.primaryFocus?.unfocus();
      _notify('Saved search created.');
    } catch (error) {
      if (!mounted) return;
      final described = ApiClient.describe(error);
      setState(() {
        _creating = false;
        _createError = described.code == 'limit_reached'
            ? 'You have $savedSearchLimit saved searches. Delete one first.'
            : described.message;
      });
    }
  }

  Future<void> _delete(SavedSearch search) async {
    if (_deletingId != null) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete saved search?'),
        content: Text(
          search.name.trim().isEmpty
              ? 'You will stop getting drop alerts for this search.'
              : 'You will stop getting drop alerts for “${search.name}”.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const Key('saved-search-delete-confirm'),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _deletingId = search.id);
    try {
      await ref.read(apiProvider).deleteSavedSearch(search.id);
      if (!mounted) return;
      setState(() {
        _searches = _searches.where((row) => row.id != search.id).toList();
        _deletingId = null;
      });
      _notify('Saved search deleted.');
    } catch (error) {
      if (!mounted) return;
      setState(() => _deletingId = null);
      _notify(ApiClient.describe(error).message);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Saved searches'),
        actions: [
          IconButton(
            onPressed: _loading ? null : () => _load(showSpinner: false),
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: SafeArea(
        top: false,
        child: RefreshIndicator(
          color: Soda.lime,
          onRefresh: () => _load(showSpinner: false),
          child: ListView(
            physics: const AlwaysScrollableScrollPhysics(),
            keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
            padding: const EdgeInsets.fromLTRB(20, 12, 20, 36),
            children: [
              Text(
                'Standing catalog queries. When a drink that matches one '
                'drops into the catalog, you get a notification.',
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 20),
              _existingSection(),
              const SizedBox(height: 24),
              _createSection(),
            ],
          ),
        ),
      ),
    );
  }

  Widget _existingSection() {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaSectionTitle(
          title: 'Your searches',
          trailing: _searches.isEmpty
              ? null
              : sodaCount(_searches.length, 'search', 'searches'),
        ),
        const SizedBox(height: 4),
        Text(
          '${_searches.length} of $savedSearchLimit used. Alerts are sent at '
          'most once a day per search.',
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 10),
        if (_loading)
          const SodaLoadingView(label: 'Loading saved searches…', dense: true)
        else if (_error != null)
          SodaStatusView(
            dense: true,
            icon: Icons.cloud_off_rounded,
            title: 'Saved searches did not load',
            message: _error,
            actionLabel: 'Try again',
            onAction: _load,
          )
        else if (_searches.isEmpty)
          const Card(
            child: SodaStatusView(
              dense: true,
              icon: Icons.bookmark_border_rounded,
              title: 'No saved searches yet',
              message:
                  'A search can be as loose as a beverage type or as tight as '
                  'a brand, a style and caffeine-free together.',
            ),
          )
        else
          Card(
            child: Column(
              children: [
                for (var index = 0; index < _searches.length; index++) ...[
                  if (index > 0)
                    const Divider(height: 1, indent: 16, endIndent: 16),
                  _searchRow(theme, _searches[index]),
                ],
              ],
            ),
          ),
      ],
    );
  }

  Widget _searchRow(ThemeData theme, SavedSearch search) {
    final busy = _deletingId == search.id;
    final name = search.name.trim().isEmpty ? 'Untitled search' : search.name;
    final pills = savedSearchPills(search);
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 8, 12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Wrap(
                  spacing: 8,
                  runSpacing: 6,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    Text(
                      name,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (!search.notifyDrops)
                      const SodaPill(label: 'Alerts off'),
                  ],
                ),
                if (pills.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    children: [for (final pill in pills) SodaPill(label: pill)],
                  ),
                ],
                if (search.createdAt.isNotEmpty) ...[
                  const SizedBox(height: 8),
                  Text('Saved ${sodaDate(search.createdAt)}', style: hint),
                ],
              ],
            ),
          ),
          const SizedBox(width: 8),
          TextButton(
            key: Key('saved-search-delete-${search.id}'),
            onPressed: _deletingId == null ? () => _delete(search) : null,
            child: busy
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Soda.lime,
                    ),
                  )
                : const Text('Delete'),
          ),
        ],
      ),
    );
  }

  Widget _createSection() {
    final theme = Theme.of(context);
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'New saved search'),
        const SizedBox(height: 4),
        Text(
          'Leave a filter empty to accept any value. At least one filter is '
          'required.',
          style: hint,
        ),
        const SizedBox(height: 10),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                TextField(
                  key: const Key('saved-search-name'),
                  controller: _name,
                  maxLength: 80,
                  enabled: !_creating,
                  textCapitalization: TextCapitalization.sentences,
                  textInputAction: TextInputAction.next,
                  decoration: const InputDecoration(
                    labelText: 'Name',
                    hintText: 'Cold brew drops',
                    helperText: 'Optional. Blank is named by the server.',
                  ),
                ),
                const SizedBox(height: 8),
                TextField(
                  key: const Key('saved-search-query'),
                  controller: _query,
                  maxLength: 120,
                  enabled: !_creating,
                  textInputAction: TextInputAction.next,
                  decoration: const InputDecoration(
                    labelText: 'Words',
                    hintText: 'cherry',
                    helperText:
                        'Optional · matches the drink name, brand or '
                        'description',
                  ),
                ),
                const SizedBox(height: 8),
                DropdownButtonFormField<String>(
                  key: const Key('saved-search-beverage'),
                  initialValue: _beverageType,
                  isExpanded: true,
                  decoration: const InputDecoration(labelText: 'Beverage type'),
                  items: [
                    for (final (slug, label) in savedSearchBeverageTypes)
                      DropdownMenuItem(value: slug, child: Text(label)),
                  ],
                  onChanged: _creating
                      ? null
                      : (value) {
                          if (value != null) {
                            setState(() => _beverageType = value);
                          }
                        },
                ),
                const SizedBox(height: 16),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: TextField(
                        key: const Key('saved-search-style'),
                        controller: _style,
                        maxLength: 80,
                        enabled: !_creating,
                        autocorrect: false,
                        textInputAction: TextInputAction.next,
                        decoration: const InputDecoration(
                          labelText: 'Style slug',
                          hintText: 'cola',
                          counterText: '',
                        ),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: TextField(
                        key: const Key('saved-search-brand'),
                        controller: _brand,
                        maxLength: 80,
                        enabled: !_creating,
                        autocorrect: false,
                        textInputAction: TextInputAction.next,
                        decoration: const InputDecoration(
                          labelText: 'Brand slug',
                          hintText: 'fanta',
                          counterText: '',
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                TextField(
                  key: const Key('saved-search-flavour'),
                  controller: _flavour,
                  maxLength: 80,
                  enabled: !_creating,
                  autocorrect: false,
                  textInputAction: TextInputAction.done,
                  decoration: const InputDecoration(
                    labelText: 'Flavour slug',
                    hintText: 'cherry',
                    counterText: '',
                  ),
                ),
                const SizedBox(height: 6),
                Text(
                  'Slugs are the lowercase names used in catalog URLs, e.g. '
                  'cold-brew.',
                  style: hint,
                ),
                const SizedBox(height: 8),
                CheckboxListTile(
                  key: const Key('saved-search-caffeine-free'),
                  contentPadding: EdgeInsets.zero,
                  controlAffinity: ListTileControlAffinity.leading,
                  dense: true,
                  title: const Text('Caffeine-free only'),
                  value: _caffeineFree,
                  onChanged: _creating
                      ? null
                      : (value) =>
                            setState(() => _caffeineFree = value ?? false),
                ),
                CheckboxListTile(
                  key: const Key('saved-search-sugar-free'),
                  contentPadding: EdgeInsets.zero,
                  controlAffinity: ListTileControlAffinity.leading,
                  dense: true,
                  title: const Text('Sugar-free only'),
                  value: _sugarFree,
                  onChanged: _creating
                      ? null
                      : (value) => setState(() => _sugarFree = value ?? false),
                ),
                SwitchListTile(
                  key: const Key('saved-search-notify'),
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                  title: const Text('Notify me when a match drops'),
                  subtitle: const Text(
                    'One notification per drink, at most once a day.',
                  ),
                  value: _notifyDrops,
                  onChanged: _creating
                      ? null
                      : (value) => setState(() => _notifyDrops = value),
                ),
                if (_createError != null) ...[
                  const SizedBox(height: 8),
                  Text(
                    _createError!,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.error,
                    ),
                  ),
                ],
                const SizedBox(height: 14),
                FilledButton(
                  key: const Key('saved-search-submit'),
                  onPressed: _creating ? null : _create,
                  child: Text(_creating ? 'Saving…' : 'Save search'),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
