import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_ui.dart';
import 'user_profile_screen.dart';

/// Which side of a member's follow graph a [FollowListScreen] shows.
enum FollowListMode {
  followers,
  following;

  String get label =>
      this == FollowListMode.followers ? 'Followers' : 'Following';
}

/// The people who follow [handle], or the people [handle] follows.
///
/// Keyset paging: the endpoint hands back a `next_cursor`, passed back as
/// `before` for the next page, and the list asks for it when the last row is
/// built. A private, blocked or deleted profile answers `404`, which reads the
/// same as a missing one.
class FollowListScreen extends ConsumerStatefulWidget {
  const FollowListScreen({super.key, required this.handle, required this.mode});

  final String handle;
  final FollowListMode mode;

  /// Matches the endpoint default; the server clamps at 50.
  static const pageSize = 20;

  static Future<void> open(
    BuildContext context, {
    required String handle,
    required FollowListMode mode,
  }) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => FollowListScreen(handle: handle, mode: mode),
      ),
    );
  }

  @override
  ConsumerState<FollowListScreen> createState() => _FollowListScreenState();
}

class _FollowListScreenState extends ConsumerState<FollowListScreen> {
  List<FollowUser> _items = const [];
  String? _nextCursor;
  bool _loading = true;
  bool _loadingMore = false;
  bool _unavailable = false;
  String? _error;
  String? _moreError;

  /// Guards against a page landing after a refresh replaced the list.
  int _requestId = 0;

  bool get _isFollowers => widget.mode == FollowListMode.followers;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<FollowPage> _page({String? before}) {
    final api = ref.read(apiProvider);
    return _isFollowers
        ? api.listFollowers(
            widget.handle,
            limit: FollowListScreen.pageSize,
            before: before,
          )
        : api.listFollowing(
            widget.handle,
            limit: FollowListScreen.pageSize,
            before: before,
          );
  }

  Future<void> _load() async {
    final request = ++_requestId;
    setState(() {
      _loading = true;
      _unavailable = false;
      _error = null;
      _moreError = null;
    });
    try {
      final page = await _page();
      if (!mounted || request != _requestId) return;
      setState(() {
        _items = page.items;
        _nextCursor = page.nextCursor;
        _loading = false;
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      final described = ApiClient.describe(error);
      setState(() {
        _loading = false;
        _unavailable = described.statusCode == 404;
        _error = _unavailable ? null : described.message;
      });
    }
  }

  Future<void> _loadMore() async {
    final cursor = _nextCursor;
    if (_loading || _loadingMore || cursor == null) return;
    final request = _requestId;
    setState(() {
      _loadingMore = true;
      _moreError = null;
    });
    try {
      final page = await _page(before: cursor);
      if (!mounted || request != _requestId) return;
      final seen = _items.map((item) => item.handle).toSet();
      final fresh = page.items.where((item) => !seen.contains(item.handle));
      setState(() {
        _items = [..._items, ...fresh];
        _nextCursor = page.nextCursor;
        _loadingMore = false;
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      setState(() {
        _loadingMore = false;
        _moreError = ApiClient.describe(error).message;
      });
    }
  }

  /// Called after the last row is laid out, which is the moment the member has
  /// scrolled to the end of what is loaded.
  void _queueLoadMore() {
    if (!_loadingMore && _nextCursor != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _loadMore();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.mode.label),
        actions: [
          IconButton(
            onPressed: _loading ? null : _load,
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: RefreshIndicator(
        color: Soda.lime,
        onRefresh: _load,
        child: _body(),
      ),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading people…');
    if (_unavailable) {
      return SodaStatusView(
        icon: Icons.person_off_outlined,
        title: 'Profile unavailable',
        message: 'This profile is private or no longer exists.',
        actionLabel: 'Back',
        onAction: () => Navigator.of(context).maybePop(),
      );
    }
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: _isFollowers
            ? 'Could not load followers'
            : 'Could not load following',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_items.isEmpty) {
      return ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(20, 28, 20, 32),
        children: [
          SodaStatusView(
            dense: true,
            icon: _isFollowers
                ? Icons.people_outline_rounded
                : Icons.person_search_outlined,
            title: _isFollowers
                ? 'No followers yet'
                : 'Not following anyone yet',
            message: _isFollowers
                ? 'Nobody follows @${widget.handle} yet.'
                : '@${widget.handle} does not follow anyone yet.',
          ),
        ],
      );
    }

    final footer = _nextCursor != null ? 1 : 0;
    return ListView.separated(
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
      itemCount: _items.length + footer,
      separatorBuilder: (_, _) => const SizedBox(height: 10),
      itemBuilder: (context, index) {
        if (index >= _items.length) return _footer();
        final person = _items[index];
        if (index == _items.length - 1) _queueLoadMore();
        return _personCard(person);
      },
    );
  }

  Widget _footer() {
    final theme = Theme.of(context);
    if (_moreError != null) {
      return Column(
        children: [
          Text(
            _moreError!,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.error,
            ),
          ),
          TextButton.icon(
            onPressed: _loadMore,
            icon: const Icon(Icons.refresh_rounded, size: 18),
            label: const Text('Try again'),
          ),
        ],
      );
    }
    return const Padding(
      padding: EdgeInsets.symmetric(vertical: 12),
      child: Center(child: SodaLoadingView(dense: true)),
    );
  }

  Widget _personCard(FollowUser person) {
    final theme = Theme.of(context);
    final api = ref.read(apiProvider);
    final name = person.displayName.isEmpty
        ? '@${person.handle}'
        : person.displayName;

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: person.handle.isEmpty
            ? null
            : () => UserProfileScreen.open(context, person.handle),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(14, 12, 10, 12),
          child: Row(
            children: [
              SodaAvatar(
                initial: person.displayName.isEmpty
                    ? person.handle
                    : person.displayName,
                size: 40,
                imageUrl: person.avatarUrl,
                headers: api.imageHeaders,
                baseUrl: api.server,
              ),
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
                    if (person.displayName.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        '@${person.handle}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                    if (person.isFriend || person.followedByViewer) ...[
                      const SizedBox(height: 6),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        children: [
                          if (person.isFriend) const SodaPill(label: 'Friend'),
                          if (person.followedByViewer)
                            const SodaPill(label: 'Following'),
                        ],
                      ),
                    ],
                  ],
                ),
              ),
              Icon(
                Icons.chevron_right_rounded,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ],
          ),
        ),
      ),
    );
  }
}
