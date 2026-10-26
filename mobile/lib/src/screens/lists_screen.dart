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

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final lists = await ref.read(apiProvider).myLists();
      if (!mounted) return;
      setState(() {
        _lists = lists;
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
      appBar: AppBar(title: const Text('Lists')),
      body: _body(),
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
        message: 'Create a list on the web app and it shows up here.',
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
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
            onTap: () =>
                ListDetailScreen.open(context, list.id, name: list.name),
          );
        },
      ),
    );
  }
}
