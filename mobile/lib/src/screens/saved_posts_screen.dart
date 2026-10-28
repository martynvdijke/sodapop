import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'checkin_detail_screen.dart';

/// The check-ins the member bookmarked, newest save first.
class SavedPostsScreen extends ConsumerStatefulWidget {
  const SavedPostsScreen({super.key});

  /// Matches the API default. A full page is taken as "there may be more".
  static const pageSize = 20;

  static Future<void> open(BuildContext context) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => const SavedPostsScreen()),
    );
  }

  @override
  ConsumerState<SavedPostsScreen> createState() => _SavedPostsScreenState();
}

class _SavedPostsScreenState extends ConsumerState<SavedPostsScreen> {
  List<FeedItem> _items = const [];
  bool _loading = true;
  bool _loadingOlder = false;
  bool _hasMore = false;
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
      final items = await ref
          .read(apiProvider)
          .savedCheckins(limit: SavedPostsScreen.pageSize);
      if (!mounted) return;
      setState(() {
        _items = items;
        _hasMore = items.length >= SavedPostsScreen.pageSize;
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

  /// Appends the page before the oldest saved row on screen.
  Future<void> _loadOlder() async {
    if (_loadingOlder || _items.isEmpty) return;
    setState(() => _loadingOlder = true);
    try {
      final older = await ref
          .read(apiProvider)
          .savedCheckins(
            limit: SavedPostsScreen.pageSize,
            before: _items.last.id,
          );
      if (!mounted) return;
      final seen = _items.map((item) => item.id).toSet();
      final fresh = older.where((item) => !seen.contains(item.id)).toList();
      setState(() {
        _items = [..._items, ...fresh];
        _hasMore = older.length >= SavedPostsScreen.pageSize;
        _loadingOlder = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _loadingOlder = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _open(FeedItem item) async {
    await CheckinDetailScreen.open(context, item.id);
    if (!mounted) return;
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Saved posts')),
      body: RefreshIndicator(
        color: Soda.lime,
        onRefresh: _load,
        child: _body(),
      ),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading saved posts…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load saved posts',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_items.isEmpty) {
      return const SodaStatusView(
        icon: Icons.bookmark_border_rounded,
        title: 'No saved posts yet',
        message: 'Save a pour from your feed and it waits here for you.',
      );
    }

    final footer = _hasMore ? 1 : 0;
    return ListView.separated(
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
      itemCount: _items.length + 1 + footer,
      separatorBuilder: (_, _) => const SizedBox(height: 10),
      itemBuilder: (context, index) {
        if (index == 0) {
          return Text(
            'Check-ins you saved, newest first.',
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          );
        }
        final position = index - 1;
        if (position >= _items.length) return _olderButton();
        final item = _items[position];
        return _savedCard(item);
      },
    );
  }

  Widget _olderButton() {
    return Center(
      child: TextButton.icon(
        onPressed: _loadingOlder ? null : _loadOlder,
        icon: _loadingOlder
            ? const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: Soda.lime,
                ),
              )
            : const Icon(Icons.history_rounded, size: 18),
        label: const Text('Load older saves'),
      ),
    );
  }

  Widget _savedCard(FeedItem item) {
    final theme = Theme.of(context);
    final name = item.drinkName.isEmpty ? 'Soda' : item.drinkName;
    final quarters = (item.rating ?? 0).round();
    final rated = quarters > 0;
    final author = item.displayName.isEmpty
        ? '@${item.handle}'
        : item.displayName;
    final byline = [
      if (item.brand.isNotEmpty) item.brand,
      'by $author',
    ].join(' · ');
    final date = sodaDate(item.createdAt);
    final meta = [
      if (item.venueName.isNotEmpty) item.venueName,
      if (date.isNotEmpty) date,
    ].join(' · ');
    final metaStyle = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => _open(item),
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _thumb(item),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      byline,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: metaStyle,
                    ),
                    const SizedBox(height: 6),
                    if (rated)
                      Row(
                        children: [
                          SodaRating(quarters: quarters, size: 15),
                          const SizedBox(width: 6),
                          Text(
                            ratingLabel(quarters),
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: Soda.tangerine,
                              fontWeight: FontWeight.w800,
                            ),
                          ),
                        ],
                      )
                    else
                      Text(
                        'Not rated',
                        style: metaStyle,
                      ),
                    if (meta.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        meta,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: metaStyle,
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _thumb(FeedItem item) {
    const size = 64.0;
    final theme = Theme.of(context);
    final placeholder = Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Icon(
        Icons.local_drink_outlined,
        size: 24,
        color: theme.colorScheme.onSurfaceVariant,
      ),
    );

    final api = ref.read(apiProvider);
    if (!api.configured || item.photoId.isEmpty) return placeholder;

    return ClipRRect(
      borderRadius: BorderRadius.circular(14),
      child: Image.network(
        api.mediaUrl(item.photoId),
        width: size,
        height: size,
        fit: BoxFit.cover,
        headers: api.imageHeaders,
        semanticLabel: 'Photo of ${item.drinkName}',
        errorBuilder: (_, _, _) => placeholder,
      ),
    );
  }
}
