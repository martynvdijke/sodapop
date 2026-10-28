import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/list_summary_card.dart';
import '../widgets/soda_ui.dart';
import 'list_detail_screen.dart';

/// The member's own lists, including the built-in wishlist.
class ListsScreen extends ConsumerStatefulWidget {
  const ListsScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const ListsScreen()));
  }

  @override
  ConsumerState<ListsScreen> createState() => _ListsScreenState();
}

class _ListsScreenState extends ConsumerState<ListsScreen> {
  List<ListSummary> _lists = const [];
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
      final lists = await ref.read(apiProvider).myLists();
      if (!mounted) return;
      setState(() {
        _lists = lists;
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

  void _showSnack(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _createList() async {
    final created = await showListEditorSheet(context);
    if (!mounted || created == null) return;
    _showSnack('List created.');
    await _load(showSpinner: false);
  }

  Future<void> _editList(ListSummary list) async {
    final updated = await showListEditorSheet(context, initial: list);
    if (!mounted || updated == null) return;
    _showSnack('List updated.');
    await _load(showSpinner: false);
  }

  Future<void> _deleteList(ListSummary list) async {
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
      await _load(showSpinner: false);
    } catch (error) {
      if (!mounted) return;
      _showSnack(ApiClient.describe(error).message);
    }
  }

  Future<void> _openList(ListSummary list) async {
    await ListDetailScreen.open(context, list.id, name: list.name);
    if (mounted) await _load(showSpinner: false);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Lists')),
      body: _body(),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _createList,
        backgroundColor: Theme.of(context).colorScheme.primary,
        foregroundColor: Theme.of(context).colorScheme.onPrimary,
        icon: const Icon(Icons.add_rounded),
        label: const Text('New list'),
      ),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading lists…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load your lists',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_lists.isEmpty) {
      return const SodaStatusView(
        icon: Icons.queue_music_rounded,
        title: 'No lists yet',
        message: 'Start a list for the bottles you keep coming back to.',
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: () => _load(showSpinner: false),
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 104),
        itemCount: _lists.length + 1,
        separatorBuilder: (_, _) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          if (index == 0) {
            return Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(
                'Wishlists, shopping lists, and the bottles you keep coming back to.',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            );
          }
          final list = _lists[index - 1];
          return ListSummaryCard(
            summary: list,
            onTap: () => _openList(list),
            onEdit: list.isSystem ? null : () => _editList(list),
            onDelete: list.isSystem ? null : () => _deleteList(list),
          );
        },
      ),
    );
  }
}
