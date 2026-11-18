import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import 'add_drink_screen.dart';
import 'ai_draft_screen.dart';
import 'checkin_sheet.dart';
import 'compare_screen.dart';
import 'drink_detail_screen.dart';
import 'nearby_screen.dart';
import 'venues_screen.dart';

class DiscoverScreen extends ConsumerStatefulWidget {
  const DiscoverScreen({
    super.key,
    this.compareOpener,
    this.draftOpener,
    this.addOpener,
  });

  /// Optional test overrides for the drafting entry points. Production opens
  /// [CompareScreen], [AiDraftScreen] and [AddDrinkScreen].
  final VoidCallback? compareOpener;
  final VoidCallback? draftOpener;
  final ValueChanged<String>? addOpener;

  @override
  ConsumerState<DiscoverScreen> createState() => _DiscoverScreenState();
}

class _DiscoverScreenState extends ConsumerState<DiscoverScreen> {
  final _query = TextEditingController();
  List<Drink> _results = const [];
  bool _searching = false;
  bool _searched = false;
  bool _savingAlert = false;
  bool _queryActive = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _query.addListener(_onQueryChanged);
  }

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  /// The quick-save entry appears only while the search box has text, so a
  /// whole rebuild is needed just on the empty/non-empty transition.
  void _onQueryChanged() {
    final active = _query.text.trim().isNotEmpty;
    if (active != _queryActive && mounted) {
      setState(() => _queryActive = active);
    }
  }

  void _snack(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  /// Captures the typed words as a standing drop alert: the query alone
  /// satisfies the server's at-least-one-filter rule, so no form is needed
  /// where the search already happened.
  Future<void> _saveDropAlert() async {
    final value = _query.text.trim();
    if (value.isEmpty || _savingAlert) return;
    setState(() => _savingAlert = true);
    try {
      await ref.read(apiProvider).createSavedSearch(query: value);
      if (!mounted) return;
      setState(() => _savingAlert = false);
      _snack('Drop alert saved for “$value”.');
    } catch (error) {
      if (!mounted) return;
      setState(() => _savingAlert = false);
      final described = ApiClient.describe(error);
      _snack(
        described.code == 'limit_reached'
            ? 'You have reached 25 saved searches. Delete one in settings first.'
            : described.message,
      );
    }
  }

  Future<void> _search() async {
    final value = _query.text.trim();
    if (value.length < 2) {
      setState(() => _error = 'Type at least two characters.');
      return;
    }
    setState(() {
      _searching = true;
      _error = null;
      _searched = true;
    });
    try {
      final results = await ref.read(apiProvider).search(value);
      if (!mounted) return;
      setState(() {
        _results = results;
        _searching = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _searching = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  /// Shown only when the search box carries words. Tapping it turns those
  /// words into a saved search, because a drop alert is where searching
  /// already happens.
  Widget _saveSearchEntry(ThemeData theme) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: Align(
        alignment: Alignment.centerLeft,
        child: TextButton.icon(
          key: const Key('discover-save-search'),
          onPressed: _savingAlert ? null : _saveDropAlert,
          icon: _savingAlert
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: Soda.lime,
                  ),
                )
              : const Icon(Icons.notifications_active_outlined, size: 18),
          label: const Text('Save as drop alert'),
        ),
      ),
    );
  }

  /// A quiet shortcut under the search field: drafting starts from notes, so
  /// it never competes with the search action for attention.
  Widget _draftEntry(ThemeData theme) {
    final accent = theme.colorScheme.primary;
    final radius = BorderRadius.circular(14);
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: MergeSemantics(
        child: Semantics(
          button: true,
          child: Material(
            color: accent.withValues(alpha: 0.1),
            borderRadius: radius,
            child: InkWell(
              key: const Key('discover-draft'),
              borderRadius: radius,
              onTap: _openDraft,
              child: Container(
                padding: const EdgeInsets.fromLTRB(12, 9, 10, 9),
                decoration: BoxDecoration(
                  borderRadius: radius,
                  border: Border.all(color: accent.withValues(alpha: 0.32)),
                ),
                child: Row(
                  children: [
                    Container(
                      width: 32,
                      height: 32,
                      alignment: Alignment.center,
                      decoration: BoxDecoration(
                        color: accent.withValues(alpha: 0.5),
                        shape: BoxShape.circle,
                      ),
                      child: Icon(
                        Icons.auto_awesome_rounded,
                        size: 16,
                        color: theme.colorScheme.onPrimary,
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            'Draft a soda with AI',
                            style: theme.textTheme.labelLarge?.copyWith(
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          Text(
                            'Turn a few notes into a provisional soda.',
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ],
                      ),
                    ),
                    Icon(
                      Icons.chevron_right_rounded,
                      size: 20,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  void _openCompare() {
    final opener = widget.compareOpener;
    if (opener != null) {
      opener();
      return;
    }
    CompareScreen.open(context);
  }

  void _openDraft() {
    final opener = widget.draftOpener;
    if (opener != null) {
      opener();
      return;
    }
    AiDraftScreen.open(context);
  }

  /// The manual form is the fallback when search finds nothing or the server
  /// has no AI. The current query becomes the proposed name.
  void _openAdd() {
    final opener = widget.addOpener;
    if (opener != null) {
      opener(_query.text.trim());
      return;
    }
    AddDrinkScreen.open(context, name: _query.text.trim());
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Discover'),
        actions: [
          IconButton(
            key: const Key('discover-nearby'),
            onPressed: () => NearbyScreen.open(context),
            tooltip: 'Nearby',
            icon: const Icon(Icons.push_pin_outlined),
          ),
          IconButton(
            key: const Key('discover-compare'),
            onPressed: _openCompare,
            tooltip: 'Compare sodas',
            icon: const Icon(Icons.compare_arrows_rounded),
          ),
          IconButton(
            onPressed: () => VenuesScreen.open(context),
            tooltip: 'Venues',
            icon: const Icon(Icons.place_outlined),
          ),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _query,
                    textInputAction: TextInputAction.search,
                    decoration: const InputDecoration(
                      hintText: 'Cola, ginger beer, cherry…',
                    ),
                    onSubmitted: (_) => _search(),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton.filled(
                  onPressed: _search,
                  icon: const Icon(Icons.search_rounded),
                ),
              ],
            ),
          ),
          if (_queryActive) _saveSearchEntry(theme),
          _draftEntry(theme),
          if (_error != null)
            Padding(padding: const EdgeInsets.all(16), child: Text(_error!)),
          if (_searching) const LinearProgressIndicator(color: Soda.lime),
          Expanded(
            child: !_searched
                ? const Center(
                    child: Text(
                      'Search the shared catalog.\nResults are capped, because Sodapop is not a database to scrape.',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Soda.muted),
                    ),
                  )
                : _results.isEmpty
                ? Center(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const Text('No sodas matched.'),
                        const SizedBox(height: 8),
                        TextButton.icon(
                          key: const Key('discover-add-empty'),
                          onPressed: _openAdd,
                          icon: const Icon(Icons.add_rounded, size: 18),
                          label: const Text('Add a soda'),
                        ),
                      ],
                    ),
                  )
                : ListView.separated(
                    padding: const EdgeInsets.all(16),
                    itemCount: _results.length,
                    separatorBuilder: (_, _) => const SizedBox(height: 10),
                    itemBuilder: (context, index) {
                      final drink = _results[index];
                      return Card(
                        child: ListTile(
                          contentPadding: const EdgeInsets.symmetric(
                            horizontal: 16,
                            vertical: 8,
                          ),
                          onTap: drink.slug.isEmpty
                              ? null
                              : () => DrinkDetailScreen.open(
                                  context,
                                  drink.slug,
                                  name: drink.name,
                                ),
                          title: Text(
                            drink.name,
                            style: const TextStyle(fontWeight: FontWeight.w700),
                          ),
                          subtitle: Text(drink.subtitle),
                          trailing: FilledButton(
                            onPressed: () => showCheckinSheet(context, drink),
                            child: const Text('Log'),
                          ),
                        ),
                      );
                    },
                  ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 10, 10, 10),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    'Tip: scanning a barcode is faster than typing.',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
                TextButton.icon(
                  key: const Key('discover-add'),
                  onPressed: _openAdd,
                  icon: const Icon(Icons.add_rounded, size: 18),
                  label: const Text('Add a soda'),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
