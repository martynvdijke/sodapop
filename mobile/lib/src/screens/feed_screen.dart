import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../offline/checkin_outbox.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/soda_ui.dart';
import 'checkin_detail_screen.dart';
import 'notifications_screen.dart';
import 'user_profile_screen.dart';

class FeedScreen extends ConsumerStatefulWidget {
  const FeedScreen({super.key});

  @override
  ConsumerState<FeedScreen> createState() => _FeedScreenState();
}

class _FeedScreenState extends ConsumerState<FeedScreen> {
  List<FeedItem> _items = const [];
  bool _loading = true;
  String? _error;
  String? _notice;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load({bool quiet = false}) async {
    if (!quiet) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final sent = await CheckinOutbox().flush(ref.read(apiProvider));
      final items = await ref.read(apiProvider).feed();
      if (!mounted) return;
      setState(() {
        _items = items;
        _loading = false;
        _error = null;
        if (sent > 0) {
          _notice = '$sent offline check-in${sent == 1 ? '' : 's'} uploaded';
        }
      });
    } catch (error) {
      if (!mounted) return;
      final message = ApiClient.describe(error).message;
      if (quiet && _items.isNotEmpty) {
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

  Future<void> _openDetail(FeedItem item) async {
    await CheckinDetailScreen.open(context, item.id);
    if (!mounted) return;
    await _load(quiet: true);
  }

  void _openAuthor(FeedItem item) {
    if (item.handle.isEmpty) return;
    UserProfileScreen.open(context, item.handle);
  }

  Future<void> _toast(FeedItem item) async {
    if (item.toasted) return;
    try {
      await ref.read(apiProvider).toast(item.id);
      if (!mounted) return;
      setState(() {
        _items = _items
            .map(
              (row) => row.id == item.id
                  ? FeedItem(
                      id: row.id,
                      handle: row.handle,
                      displayName: row.displayName,
                      drinkName: row.drinkName,
                      drinkSlug: row.drinkSlug,
                      rating: row.rating,
                      note: row.note,
                      venueName: row.venueName,
                      toasts: row.toasts + 1,
                      toasted: true,
                    )
                  : row,
            )
            .toList();
      });
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Sodapop'),
        actions: [
          IconButton(
            onPressed: () => NotificationsScreen.open(context),
            tooltip: 'Notifications',
            icon: const Icon(Icons.notifications_none_rounded),
          ),
          IconButton(
            onPressed: _load,
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: RefreshIndicator(
        color: Soda.lime,
        onRefresh: _load,
        child: _body(context),
      ),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator(color: Soda.lime));
    }
    if (_error != null) {
      return ListView(
        children: [
          const SizedBox(height: 80),
          Center(child: Text(_error!, textAlign: TextAlign.center)),
          Center(
            child: TextButton(onPressed: _load, child: const Text('Try again')),
          ),
        ],
      );
    }
    if (_items.isEmpty) {
      return ListView(
        children: [
          const SizedBox(height: 100),
          const Icon(Icons.local_drink_outlined, size: 56, color: Soda.muted),
          const SizedBox(height: 12),
          Center(
            child: Text(
              'No fizz yet.\nScan a can or add a friend to fill this up.',
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
          ),
        ],
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: _items.length + (_notice == null ? 0 : 1),
      separatorBuilder: (_, _) => const SizedBox(height: 12),
      itemBuilder: (context, index) {
        if (_notice != null && index == 0) {
          return Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: Theme.of(
                context,
              ).colorScheme.primary.withValues(alpha: 0.2),
              borderRadius: BorderRadius.circular(12),
            ),
            child: Text(_notice!),
          );
        }
        final item = _items[_notice == null ? index : index - 1];
        return _FeedCard(
          item: item,
          onToast: () => _toast(item),
          onOpen: () => _openDetail(item),
          onOpenAuthor: () => _openAuthor(item),
        );
      },
    );
  }
}

class _FeedCard extends StatelessWidget {
  const _FeedCard({
    required this.item,
    required this.onToast,
    required this.onOpen,
    required this.onOpenAuthor,
  });

  final FeedItem item;
  final VoidCallback onToast;
  final VoidCallback onOpen;
  final VoidCallback onOpenAuthor;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onOpen,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: InkWell(
                      onTap: onOpenAuthor,
                      borderRadius: BorderRadius.circular(10),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 2),
                        child: Row(
                          children: [
                            SodaAvatar(
                              initial: item.displayName.isEmpty
                                  ? item.handle
                                  : item.displayName,
                              size: 36,
                            ),
                            const SizedBox(width: 10),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    item.displayName,
                                    style: theme.textTheme.titleSmall?.copyWith(
                                      fontWeight: FontWeight.w700,
                                    ),
                                  ),
                                  Text(
                                    '@${item.handle}',
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
                    ),
                  ),
                  const SizedBox(width: 8),
                  Text(
                    ratingLabel((item.rating ?? 0).round()),
                    style: theme.textTheme.titleMedium?.copyWith(
                      color: Soda.tangerine,
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              Text(
                item.drinkName,
                style: theme.textTheme.titleLarge?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              if (item.venueName.isNotEmpty)
                Text(
                  'at ${item.venueName}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              if (item.note.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(item.note, style: theme.textTheme.bodyMedium),
              ],
              const SizedBox(height: 10),
              Align(
                alignment: Alignment.centerRight,
                child: TextButton.icon(
                  onPressed: item.toasted ? null : onToast,
                  icon: Icon(
                    item.toasted
                        ? Icons.celebration
                        : Icons.celebration_outlined,
                    size: 18,
                  ),
                  label: Text(
                    '${item.toasts} toast${item.toasts == 1 ? '' : 's'}',
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
