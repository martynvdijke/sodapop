import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'checkin_detail_screen.dart';
import 'user_profile_screen.dart';

/// Activity for the signed-in member: toasts, comments, badges and friend
/// requests, newest first.
class NotificationsScreen extends ConsumerStatefulWidget {
  const NotificationsScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => const NotificationsScreen()),
    );
  }

  @override
  ConsumerState<NotificationsScreen> createState() =>
      _NotificationsScreenState();
}

class _NotificationsScreenState extends ConsumerState<NotificationsScreen> {
  List<NotificationItem> _items = const [];
  final Set<String> _readOverrides = {};
  final Map<String, String> _resolved = {};
  bool _loading = true;
  bool _markingAll = false;
  String? _busyId;
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
      final items = await ref.read(apiProvider).notifications();
      if (!mounted) return;
      setState(() {
        _items = items;
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

  bool _isRead(NotificationItem item) =>
      item.read || _readOverrides.contains(item.id);

  bool get _hasUnread => _items.any((item) => !_isRead(item));

  Future<void> _markAllRead() async {
    if (_markingAll || !_hasUnread) return;
    setState(() => _markingAll = true);
    try {
      await ref.read(apiProvider).markNotificationsRead();
      if (!mounted) return;
      setState(() {
        _readOverrides.addAll(_items.map((item) => item.id));
        _markingAll = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _markingAll = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _accept(NotificationItem item) async {
    final requestId = item.friendRequestId;
    if (requestId == null || _busyId != null) return;
    setState(() => _busyId = item.id);
    try {
      await ref.read(apiProvider).acceptFriendRequest(requestId);
      if (!mounted) return;
      setState(() {
        _resolved[item.id] = 'Accepted';
        _readOverrides.add(item.id);
        _busyId = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _busyId = null);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _decline(NotificationItem item) async {
    final requestId = item.friendRequestId;
    if (requestId == null || _busyId != null) return;
    setState(() => _busyId = item.id);
    try {
      await ref.read(apiProvider).removeFriend(requestId);
      if (!mounted) return;
      setState(() {
        _resolved[item.id] = 'Declined';
        _readOverrides.add(item.id);
        _busyId = null;
      });
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Friend request declined.')));
    } catch (error) {
      if (!mounted) return;
      setState(() => _busyId = null);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  /// Maps a server-supplied relative href to an in-app screen. Hrefs are
  /// limited to check-ins and profiles; everything else stays put.
  VoidCallback? _target(NotificationItem item) {
    final parts = Uri.tryParse(item.href)?.pathSegments ?? const <String>[];
    if (parts.length == 2 && parts.first == 'checkins') {
      final id = parts[1];
      return () => CheckinDetailScreen.open(context, id);
    }
    if (parts.length == 2 && (parts.first == 'u' || parts.first == 'users')) {
      final handle = parts[1];
      return () => UserProfileScreen.open(context, handle);
    }
    return null;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Notifications'),
        actions: [
          IconButton(
            onPressed: _hasUnread && !_markingAll ? _markAllRead : null,
            tooltip: 'Mark all as read',
            icon: _markingAll
                ? const SizedBox(
                    width: 20,
                    height: 20,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Soda.lime,
                    ),
                  )
                : const Icon(Icons.done_all_rounded),
          ),
        ],
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading notifications…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load notifications',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_items.isEmpty) {
      return const SodaStatusView(
        icon: Icons.notifications_none_rounded,
        title: 'No notifications yet',
        message: 'Toasts, comments and friend requests show up here.',
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        itemCount: _items.length + 1,
        separatorBuilder: (_, _) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          if (index == 0) {
            return Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(
                _hasUnread
                    ? '${_items.where((item) => !_isRead(item)).length} unread'
                    : 'All caught up',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            );
          }
          return _tile(_items[index - 1]);
        },
      ),
    );
  }

  Widget _tile(NotificationItem item) {
    final theme = Theme.of(context);
    final unread = !_isRead(item);
    final target = _target(item);
    final resolved = _resolved[item.id];
    final actorName = item.actorDisplayName.isEmpty
        ? (item.actorHandle.isEmpty ? '' : '@${item.actorHandle}')
        : item.actorDisplayName;
    final date = sodaDate(item.createdAt);

    return Card(
      clipBehavior: Clip.antiAlias,
      color: unread ? theme.colorScheme.primary.withValues(alpha: 0.1) : null,
      child: InkWell(
        onTap: target,
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SodaAvatar(
                initial: item.actorInitial.isEmpty ? 'S' : item.actorInitial,
                size: 36,
                semanticLabel:
                    '${actorName.isEmpty ? 'Sodapop' : actorName} avatar',
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Text(
                          _kindLabel(item.kind),
                          style: theme.textTheme.labelSmall?.copyWith(
                            fontWeight: FontWeight.w800,
                            color: theme.colorScheme.onSurfaceVariant,
                            letterSpacing: 0.3,
                          ),
                        ),
                        if (unread) ...[
                          const SizedBox(width: 8),
                          Container(
                            width: 7,
                            height: 7,
                            decoration: const BoxDecoration(
                              color: Soda.lime,
                              shape: BoxShape.circle,
                            ),
                          ),
                          const SizedBox(width: 4),
                          Text(
                            'Unread',
                            style: theme.textTheme.labelSmall?.copyWith(
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ],
                      ],
                    ),
                    const SizedBox(height: 4),
                    Text.rich(
                      TextSpan(
                        children: [
                          if (actorName.isNotEmpty)
                            TextSpan(
                              text: '$actorName ',
                              style: const TextStyle(
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                          TextSpan(text: item.body),
                        ],
                      ),
                      style: theme.textTheme.bodyMedium,
                    ),
                    if (date.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        [
                          if (item.actorHandle.isNotEmpty)
                            '@${item.actorHandle}',
                          date,
                        ].join(' · '),
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                    if (resolved != null) ...[
                      const SizedBox(height: 10),
                      SodaPill(label: resolved),
                    ] else if (item.kind == 'friend_request') ...[
                      if (item.friendRequestId != null) ...[
                        const SizedBox(height: 10),
                        Row(
                          children: [
                            FilledButton(
                              style: FilledButton.styleFrom(
                                minimumSize: const Size(44, 44),
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 18,
                                ),
                              ),
                              onPressed: _busyId == null
                                  ? () => _accept(item)
                                  : null,
                              child: const Text('Accept'),
                            ),
                            const SizedBox(width: 6),
                            TextButton(
                              style: TextButton.styleFrom(
                                minimumSize: const Size(44, 44),
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 14,
                                ),
                              ),
                              onPressed: _busyId == null
                                  ? () => _decline(item)
                                  : null,
                              child: const Text('Decline'),
                            ),
                          ],
                        ),
                      ],
                    ] else if (target != null) ...[
                      const SizedBox(height: 6),
                      Align(
                        alignment: Alignment.centerRight,
                        child: Icon(
                          Icons.chevron_right_rounded,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
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

  static String _kindLabel(String kind) {
    switch (kind) {
      case 'friend_request':
        return 'FRIEND REQUEST';
      case 'friend_accept':
        return 'NEW FRIEND';
      case 'toast':
        return 'TOAST';
      case 'badge':
        return 'BADGE';
      case 'comment':
        return 'COMMENT';
      default:
        return 'NOTIFICATION';
    }
  }
}
