import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'user_profile_screen.dart';

/// The signed-in member's friend directory: accepted friends, pending requests
/// in both directions, and a handle-first way to add someone.
class FriendsScreen extends ConsumerStatefulWidget {
  const FriendsScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => const FriendsScreen()),
    );
  }

  @override
  ConsumerState<FriendsScreen> createState() => _FriendsScreenState();
}

enum _FriendsTab { friends, requests, add }

class _FriendsScreenState extends ConsumerState<FriendsScreen> {
  final _handle = TextEditingController();
  FriendsDirectory? _directory;
  _FriendsTab _tab = _FriendsTab.friends;
  bool _loading = true;
  bool _sending = false;
  String? _error;
  String? _handleError;
  String? _busyId;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _handle.dispose();
    super.dispose();
  }

  Future<void> _load({bool quiet = false}) async {
    if (!quiet) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final directory = await ref.read(apiProvider).friends();
      if (!mounted) return;
      setState(() {
        _directory = directory;
        _loading = false;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      final message = ApiClient.describe(error).message;
      if (quiet && _directory != null) {
        setState(() => _loading = false);
        _notify(message);
        return;
      }
      setState(() {
        _loading = false;
        _error = message;
      });
    }
  }

  void _notify(String message) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(message)),
    );
  }

  Future<void> _accept(PendingFriend request) async {
    if (_busyId != null) return;
    setState(() => _busyId = request.id);
    try {
      await ref.read(apiProvider).acceptFriendRequest(request.id);
      if (!mounted) return;
      setState(() => _busyId = null);
      _notify('Friend added.');
      await _load(quiet: true);
    } catch (error) {
      if (!mounted) return;
      setState(() => _busyId = null);
      _notify(ApiClient.describe(error).message);
    }
  }

  Future<void> _decline(PendingFriend request) async {
    if (_busyId != null) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Decline request from @${request.handle}?'),
        content: const Text(
          'This removes the request. You can send one back later.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Decline'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _busyId = request.id);
    try {
      await ref.read(apiProvider).removeFriend(request.id);
      if (!mounted) return;
      setState(() => _busyId = null);
      _notify('Request declined.');
      await _load(quiet: true);
    } catch (error) {
      if (!mounted) return;
      setState(() => _busyId = null);
      _notify(ApiClient.describe(error).message);
    }
  }

  Future<void> _cancel(PendingFriend request) async {
    if (_busyId != null) return;
    setState(() => _busyId = request.id);
    try {
      await ref.read(apiProvider).removeFriend(request.id);
      if (!mounted) return;
      setState(() => _busyId = null);
      _notify('Request cancelled.');
      await _load(quiet: true);
    } catch (error) {
      if (!mounted) return;
      setState(() => _busyId = null);
      _notify(ApiClient.describe(error).message);
    }
  }

  Future<void> _send() async {
    if (_sending) return;
    final handle = _handle.text
        .trim()
        .toLowerCase()
        .replaceFirst(RegExp(r'^@+'), '');
    if (!RegExp(r'^[a-z0-9_]{3,24}$').hasMatch(handle)) {
      setState(() {
        _handleError = 'Use 3–24 lowercase letters, numbers or underscores.';
      });
      return;
    }

    setState(() {
      _sending = true;
      _handleError = null;
    });
    try {
      await ref.read(apiProvider).sendFriendRequest(handle);
      if (!mounted) return;
      setState(() => _sending = false);
      _handle.clear();
      FocusManager.instance.primaryFocus?.unfocus();
      _notify('If that account exists, a request has been sent.');
      await _load(quiet: true);
    } catch (error) {
      if (!mounted) return;
      setState(() => _sending = false);
      _notify(ApiClient.describe(error).message);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Friends'),
        actions: [
          IconButton(
            onPressed: _loading ? null : _load,
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading friends…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load friends',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }

    final directory = _directory ?? const FriendsDirectory();
    final pending = directory.incoming.length + directory.outgoing.length;
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 8, 20, 4),
          child: SodaSegmented<_FriendsTab>(
            options: [
              (_FriendsTab.friends, 'Friends'),
              (
                _FriendsTab.requests,
                pending == 0 ? 'Requests' : 'Requests ($pending)',
              ),
              (_FriendsTab.add, 'Add'),
            ],
            selected: _tab,
            onChanged: (tab) => setState(() => _tab = tab),
          ),
        ),
        Expanded(child: _tabBody(directory)),
      ],
    );
  }

  Widget _tabBody(FriendsDirectory directory) {
    switch (_tab) {
      case _FriendsTab.friends:
        return _friendsTab(directory);
      case _FriendsTab.requests:
        return _requestsTab(directory);
      case _FriendsTab.add:
        return _addTab();
    }
  }

  Widget _friendsTab(FriendsDirectory directory) {
    final friends = directory.friends;
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: friends.isEmpty
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(20, 28, 20, 32),
              children: [
                SodaStatusView(
                  dense: true,
                  icon: Icons.people_outline_rounded,
                  title: 'No friends yet',
                  message:
                      'Add someone by their handle and their pours will show up in your feed.',
                  actionLabel: 'Add a friend',
                  onAction: () => setState(() => _tab = _FriendsTab.add),
                ),
              ],
            )
          : ListView.separated(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
              itemCount: friends.length + 1,
              separatorBuilder: (_, _) => const SizedBox(height: 10),
              itemBuilder: (context, index) {
                if (index == 0) {
                  return Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 4),
                    child: Text(
                      sodaCount(friends.length, 'friend'),
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: Theme.of(
                          context,
                        ).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  );
                }
                return _friendCard(friends[index - 1]);
              },
            ),
    );
  }

  Widget _friendCard(FriendUser friend) {
    final theme = Theme.of(context);
    final since = sodaDate(friend.since);
    final details = [
      '@${friend.handle}',
      if (since.isNotEmpty) 'Friends since $since',
    ].join(' · ');
    final name = friend.displayName.isEmpty
        ? '@${friend.handle}'
        : friend.displayName;

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: friend.handle.isEmpty
            ? null
            : () => UserProfileScreen.open(context, friend.handle),
        child: Padding(
          padding: const EdgeInsets.fromLTRB(14, 12, 10, 12),
          child: Row(
            children: [
              SodaAvatar(
                initial: friend.displayName.isEmpty
                    ? friend.handle
                    : friend.displayName,
                size: 40,
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
                    const SizedBox(height: 2),
                    Text(
                      details,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
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

  Widget _requestsTab(FriendsDirectory directory) {
    final incoming = directory.incoming;
    final outgoing = directory.outgoing;
    if (incoming.isEmpty && outgoing.isEmpty) {
      return RefreshIndicator(
        color: Soda.lime,
        onRefresh: _load,
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          padding: const EdgeInsets.fromLTRB(20, 28, 20, 32),
          children: const [
            SodaStatusView(
              dense: true,
              icon: Icons.mark_email_unread_outlined,
              title: 'No requests',
              message: 'Requests you send and receive show up here.',
            ),
          ],
        ),
      );
    }

    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        children: [
          if (incoming.isNotEmpty) ...[
            SodaSectionTitle(
              title: 'Incoming',
              trailing: sodaCount(incoming.length, 'request'),
            ),
            const SizedBox(height: 10),
            for (final request in incoming) ...[
              _incomingCard(request),
              const SizedBox(height: 10),
            ],
          ],
          if (outgoing.isNotEmpty) ...[
            if (incoming.isNotEmpty) const SizedBox(height: 8),
            SodaSectionTitle(
              title: 'Sent',
              trailing: sodaCount(outgoing.length, 'request'),
            ),
            const SizedBox(height: 10),
            for (final request in outgoing) ...[
              _outgoingCard(request),
              const SizedBox(height: 10),
            ],
          ],
        ],
      ),
    );
  }

  Widget _incomingCard(PendingFriend request) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 12, 14, 10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _requestIdentity(request),
            const SizedBox(height: 10),
            Row(
              children: [
                FilledButton(
                  style: FilledButton.styleFrom(
                    minimumSize: const Size(44, 44),
                    padding: const EdgeInsets.symmetric(horizontal: 18),
                  ),
                  onPressed: _busyId == null ? () => _accept(request) : null,
                  child: Text(
                    _busyId == request.id ? 'Accepting…' : 'Accept',
                  ),
                ),
                const SizedBox(width: 6),
                TextButton(
                  style: TextButton.styleFrom(
                    minimumSize: const Size(44, 44),
                    padding: const EdgeInsets.symmetric(horizontal: 14),
                  ),
                  onPressed: _busyId == null ? () => _decline(request) : null,
                  child: const Text('Decline'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _outgoingCard(PendingFriend request) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 12, 10, 8),
        child: Row(
          children: [
            Expanded(child: _requestIdentity(request)),
            const SizedBox(width: 6),
            TextButton(
              style: TextButton.styleFrom(
                minimumSize: const Size(44, 44),
                padding: const EdgeInsets.symmetric(horizontal: 12),
              ),
              onPressed: _busyId == null ? () => _cancel(request) : null,
              child: Text(_busyId == request.id ? 'Cancelling…' : 'Cancel'),
            ),
          ],
        ),
      ),
    );
  }

  Widget _requestIdentity(PendingFriend request) {
    final theme = Theme.of(context);
    final date = sodaDate(request.createdAt);
    final name = request.displayName.isEmpty
        ? '@${request.handle}'
        : request.displayName;
    final details = [
      '@${request.handle}',
      if (date.isNotEmpty) date,
    ].join(' · ');

    return Row(
      children: [
        SodaAvatar(
          initial: request.displayName.isEmpty
              ? request.handle
              : request.displayName,
          size: 36,
        ),
        const SizedBox(width: 10),
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
                details,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _addTab() {
    final theme = Theme.of(context);
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.fromLTRB(20, 16, 20, 36),
      children: [
        Card(
          child: Padding(
            padding: const EdgeInsets.all(18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Add a friend',
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                  ),
                ),
                const SizedBox(height: 6),
                Text(
                  'Enter a handle to send a request. Handles are 3–24 lowercase letters, numbers or underscores.',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: _handle,
                  enabled: !_sending,
                  autocorrect: false,
                  enableSuggestions: false,
                  textCapitalization: TextCapitalization.none,
                  textInputAction: TextInputAction.done,
                  onSubmitted: (_) => _send(),
                  inputFormatters: [
                    LengthLimitingTextInputFormatter(25),
                    _LowercaseHandleFormatter(),
                  ],
                  decoration: InputDecoration(
                    prefixText: '@',
                    hintText: 'handle',
                    errorText: _handleError,
                    errorMaxLines: 2,
                  ),
                ),
                const SizedBox(height: 14),
                Align(
                  alignment: Alignment.centerRight,
                  child: FilledButton.icon(
                    onPressed: _sending ? null : _send,
                    icon: const Icon(Icons.person_add_alt_1_rounded, size: 18),
                    label: Text(_sending ? 'Sending…' : 'Send request'),
                  ),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 16),
        Text(
          'Requests stay private. The other person sees your handle and can accept or decline.',
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }
}

/// Keeps handle input lowercase as it is typed, so the field always reads like
/// a handle rather than a display name.
class _LowercaseHandleFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(
    TextEditingValue oldValue,
    TextEditingValue newValue,
  ) {
    final text = newValue.text.toLowerCase();
    if (text == newValue.text) return newValue;
    return newValue.copyWith(text: text);
  }
}
