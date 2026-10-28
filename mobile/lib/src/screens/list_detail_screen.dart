import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/list_summary_card.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'drink_detail_screen.dart';

/// One list with its saved sodas. Visibility is enforced by the server; a
/// private or missing list shows the same neutral state.
class ListDetailScreen extends ConsumerStatefulWidget {
  const ListDetailScreen({super.key, required this.listId, this.name = ''});

  final String listId;
  final String name;

  static Future<void> open(
    BuildContext context,
    String listId, {
    String name = '',
  }) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => ListDetailScreen(listId: listId, name: name),
      ),
    );
  }

  @override
  ConsumerState<ListDetailScreen> createState() => _ListDetailScreenState();
}

class _ListDetailScreenState extends ConsumerState<ListDetailScreen> {
  ListDetail? _list;
  bool _loading = true;
  bool _unavailable = false;
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
        _unavailable = false;
      });
    }
    try {
      final list = await ref.read(apiProvider).getList(widget.listId);
      if (!mounted) return;
      setState(() {
        _list = list;
        _loading = false;
        _error = null;
        _unavailable = false;
      });
    } catch (error) {
      if (!mounted) return;
      final described = ApiClient.describe(error);
      setState(() {
        _loading = false;
        _error = described.message;
        _unavailable = described.statusCode == 404;
      });
    }
  }

  void _showSnack(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _editList() async {
    final list = _list;
    if (list == null) return;
    final updated = await showListEditorSheet(context, initial: list);
    if (!mounted || updated == null) return;
    _showSnack('List updated.');
    await _load(showSpinner: false);
  }

  Future<void> _deleteList() async {
    final list = _list;
    if (list == null) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete list?'),
        content: Text(
          'Delete "${list.name}"? The sodas stay in the catalog, but the list and its notes are gone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    try {
      await ref.read(apiProvider).deleteList(list.id);
      if (!mounted) return;
      _showSnack('List deleted.');
      Navigator.of(context).pop();
    } catch (error) {
      if (!mounted) return;
      _showSnack(ApiClient.describe(error).message);
    }
  }

  Future<void> _openAddSoda() async {
    final list = _list;
    if (list == null) return;
    final added = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (_) => _AddSodaSheet(
        listId: list.id,
        existing: {for (final item in list.items) item.drinkId},
      ),
    );
    if (!mounted) return;
    await _load(showSpinner: false);
    if (added == true && mounted) _showSnack('Added to the list.');
  }

  Future<void> _removeItem(ListItem item) async {
    final list = _list;
    if (list == null) return;
    final name = item.name.isEmpty ? 'this soda' : '"${item.name}"';
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Remove soda?'),
        content: Text('Remove $name from "${list.name}"?'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    try {
      await ref.read(apiProvider).removeListItem(list.id, item.drinkId);
      if (!mounted) return;
      _showSnack('Removed from the list.');
      await _load(showSpinner: false);
    } catch (error) {
      if (!mounted) return;
      _showSnack(ApiClient.describe(error).message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final list = _list;
    return Scaffold(
      appBar: AppBar(
        title: Text(
          list?.name.isNotEmpty == true
              ? list!.name
              : (widget.name.isEmpty ? 'List' : widget.name),
        ),
        actions: [
          if (list != null && !list.isSystem)
            PopupMenuButton<String>(
              tooltip: 'List options',
              onSelected: (value) {
                if (value == 'edit') _editList();
                if (value == 'delete') _deleteList();
              },
              itemBuilder: (context) => const [
                PopupMenuItem(value: 'edit', child: Text('Edit list')),
                PopupMenuItem(value: 'delete', child: Text('Delete list')),
              ],
            ),
        ],
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading list…');
    if (_unavailable) {
      return SodaStatusView(
        icon: Icons.lock_outline_rounded,
        title: 'List unavailable',
        message: 'This list is private or no longer exists.',
        actionLabel: 'Back',
        onAction: () => Navigator.of(context).maybePop(),
      );
    }
    if (_error != null || _list == null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load this list',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }

    final list = _list!;
    final visibility = sodaListVisibility(list.visibility);
    final theme = Theme.of(context);
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: () => _load(showSpinner: false),
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          if (list.description.isNotEmpty) ...[
            Text(list.description, style: theme.textTheme.bodyLarge),
            const SizedBox(height: 12),
          ],
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              if (visibility.isNotEmpty) SodaPill(label: visibility),
              SodaPill(label: sodaCount(list.itemCount, 'soda')),
              if (list.isSystem) const SodaPill(label: 'Built in'),
            ],
          ),
          const SizedBox(height: 18),
          FilledButton.icon(
            key: const Key('add-soda-button'),
            onPressed: _openAddSoda,
            icon: const Icon(Icons.add_rounded),
            label: const Text('Add soda'),
          ),
          const SizedBox(height: 22),
          if (list.items.isEmpty)
            const SodaStatusView(
              dense: true,
              icon: Icons.local_drink_outlined,
              title: 'Nothing here yet',
              message:
                  'Search the catalog and add the sodas you have your eye on.',
            )
          else ...[
            if (list.items.length < list.itemCount) ...[
              Padding(
                padding: const EdgeInsets.only(bottom: 10),
                child: Text(
                  'Showing the newest ${list.items.length} of ${list.itemCount}.',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ),
            ],
            for (final item in list.items) ...[
              _itemCard(item),
              const SizedBox(height: 10),
            ],
          ],
        ],
      ),
    );
  }

  Widget _itemCard(ListItem item) {
    final theme = Theme.of(context);
    final name = item.name.isEmpty ? 'Soda' : item.name;
    final openable = item.slug.isNotEmpty;
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: openable
            ? () => DrinkDetailScreen.open(context, item.slug, name: name)
            : null,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 4, 12),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (item.brand.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        item.brand,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                    if (item.note.isNotEmpty) ...[
                      const SizedBox(height: 10),
                      Container(
                        width: double.infinity,
                        padding: const EdgeInsets.fromLTRB(10, 8, 10, 8),
                        decoration: BoxDecoration(
                          color: theme.colorScheme.onSurface.withValues(
                            alpha: 0.04,
                          ),
                          border: Border(
                            left: BorderSide(
                              color: theme.colorScheme.primary,
                              width: 3,
                            ),
                          ),
                          borderRadius: const BorderRadius.horizontal(
                            right: Radius.circular(8),
                          ),
                        ),
                        child: Text(
                          item.note,
                          style: theme.textTheme.bodyMedium,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              PopupMenuButton<String>(
                tooltip: 'Options for $name',
                icon: const Icon(Icons.more_horiz_rounded),
                onSelected: (value) {
                  if (value == 'remove') _removeItem(item);
                },
                itemBuilder: (context) => const [
                  PopupMenuItem(value: 'remove', child: Text('Remove')),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Searches the catalog and adds one soda at a time, staying open so several
/// bottles can go on the list in one visit.
class _AddSodaSheet extends ConsumerStatefulWidget {
  const _AddSodaSheet({required this.listId, required this.existing});

  final String listId;
  final Set<int> existing;

  @override
  ConsumerState<_AddSodaSheet> createState() => _AddSodaSheetState();
}

class _AddSodaSheetState extends ConsumerState<_AddSodaSheet> {
  final _search = TextEditingController();
  final Set<int> _added = {};
  List<Drink> _results = const [];
  bool _searching = false;
  bool _searched = false;
  bool _changed = false;
  int? _addingId;
  String? _error;

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  bool _isOnList(Drink drink) =>
      widget.existing.contains(drink.id) || _added.contains(drink.id);

  Future<void> _runSearch() async {
    if (_searching) return;
    final query = _search.text.trim();
    if (query.length < 2) {
      setState(() {
        _error = 'Type at least two characters.';
        _searched = false;
        _results = const [];
      });
      return;
    }
    setState(() {
      _searching = true;
      _searched = true;
      _error = null;
    });
    FocusScope.of(context).unfocus();
    try {
      final results = await ref.read(apiProvider).search(query);
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

  Future<void> _add(Drink drink) async {
    if (_addingId != null || _isOnList(drink)) return;
    setState(() {
      _addingId = drink.id;
      _error = null;
    });
    try {
      await ref.read(apiProvider).addListItem(widget.listId, drinkId: drink.id);
      if (!mounted) return;
      setState(() {
        _added.add(drink.id);
        _addingId = null;
        _changed = true;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _addingId = null;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final query = _search.text.trim();
    final insets = MediaQuery.viewInsetsOf(context).bottom;
    return Padding(
      padding: EdgeInsets.only(bottom: insets),
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: (MediaQuery.sizeOf(context).height - insets) * 0.8,
        ),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 10, 20, 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Center(
                child: Container(
                  width: 40,
                  height: 4,
                  decoration: BoxDecoration(
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.16),
                    borderRadius: BorderRadius.circular(999),
                  ),
                ),
              ),
              const SizedBox(height: 14),
              Row(
                children: [
                  Expanded(
                    child: Text(
                      'Add soda',
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                        letterSpacing: -0.3,
                      ),
                    ),
                  ),
                  IconButton(
                    onPressed: () => Navigator.of(context).pop(_changed),
                    tooltip: 'Close',
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              TextField(
                key: const Key('list-add-search'),
                controller: _search,
                autofocus: true,
                textInputAction: TextInputAction.search,
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => _runSearch(),
                decoration: InputDecoration(
                  hintText: 'Name or brand',
                  suffixIcon: IconButton(
                    onPressed: _runSearch,
                    tooltip: 'Search',
                    icon: const Icon(Icons.search_rounded),
                  ),
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 8),
                Text(
                  _error!,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.error,
                  ),
                ),
              ],
              const SizedBox(height: 10),
              Flexible(child: _resultsBody(query)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _resultsBody(String query) {
    final theme = Theme.of(context);
    if (_searching) return const SodaLoadingView(dense: true);
    if (query.isEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 24),
        child: Text(
          'Search the shared catalog by name or brand.',
          textAlign: TextAlign.center,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      );
    }
    if (_searched && _results.isEmpty) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 24),
        child: Text(
          'No sodas found',
          textAlign: TextAlign.center,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      );
    }
    if (_results.isEmpty) {
      return const SizedBox(height: 8);
    }
    return ListView.separated(
      shrinkWrap: true,
      padding: const EdgeInsets.only(bottom: 4),
      itemCount: _results.length,
      separatorBuilder: (_, _) => const SizedBox(height: 8),
      itemBuilder: (context, index) {
        final drink = _results[index];
        final onList = _isOnList(drink);
        final adding = _addingId == drink.id;
        return Card(
          child: ListTile(
            onTap: onList || adding ? null : () => _add(drink),
            contentPadding: const EdgeInsets.fromLTRB(16, 4, 8, 4),
            title: Text(
              drink.name,
              style: const TextStyle(fontWeight: FontWeight.w700),
            ),
            subtitle: drink.brand.isEmpty ? null : Text(drink.brand),
            trailing: adding
                ? const SizedBox(
                    width: 20,
                    height: 20,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Soda.lime,
                    ),
                  )
                : onList
                ? const SodaPill(label: 'Added')
                : IconButton(
                    onPressed: () => _add(drink),
                    tooltip: 'Add ${drink.name}',
                    icon: const Icon(Icons.add_circle_outline_rounded),
                  ),
          ),
        );
      },
    );
  }
}
