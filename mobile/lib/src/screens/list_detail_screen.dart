import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

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

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
      _unavailable = false;
    });
    try {
      final list = await ref.read(apiProvider).getList(widget.listId);
      if (!mounted) return;
      setState(() {
        _list = list;
        _loading = false;
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

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(
          _list?.name.isNotEmpty == true
              ? _list!.name
              : (widget.name.isEmpty ? 'List' : widget.name),
        ),
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
      onRefresh: _load,
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
          const SizedBox(height: 20),
          if (list.items.isEmpty)
            const SodaStatusView(
              dense: true,
              icon: Icons.local_drink_outlined,
              title: 'Nothing here yet',
              message: 'Add sodas to this list on the web app.',
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
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
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
              const SizedBox(height: 8),
              Text(item.note, style: theme.textTheme.bodyMedium),
            ],
          ],
        ),
      ),
    );
  }
}
