import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'list_detail_screen.dart';

/// A profile as seen by the signed-in member. Private, missing and blocked
/// accounts all show the same neutral "Profile unavailable" state.
class UserProfileScreen extends ConsumerStatefulWidget {
  const UserProfileScreen({super.key, required this.handle});

  final String handle;

  static Future<void> open(BuildContext context, String handle) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => UserProfileScreen(handle: handle),
      ),
    );
  }

  @override
  ConsumerState<UserProfileScreen> createState() => _UserProfileScreenState();
}

class _UserProfileScreenState extends ConsumerState<UserProfileScreen> {
  UserProfile? _profile;
  bool _loading = true;
  bool _unavailable = false;
  bool _blocked = false;
  bool _followBusy = false;
  bool _blockBusy = false;
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
      final profile = await ref.read(apiProvider).userProfile(widget.handle);
      if (!mounted) return;
      setState(() {
        _profile = profile;
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

  Future<void> _toggleFollow() async {
    final profile = _profile;
    if (profile == null || _followBusy) return;
    setState(() => _followBusy = true);
    final api = ref.read(apiProvider);
    try {
      if (profile.isFollowing) {
        await api.unfollow(profile.handle);
      } else {
        await api.follow(profile.handle);
      }
      if (!mounted) return;
      setState(() {
        _followBusy = false;
        _profile = UserProfile(
          id: profile.id,
          handle: profile.handle,
          displayName: profile.displayName,
          bio: profile.bio,
          createdAt: profile.createdAt,
          stats: profile.stats,
          followerCount: profile.isFollowing
              ? (profile.followerCount > 0 ? profile.followerCount - 1 : 0)
              : profile.followerCount + 1,
          followingCount: profile.followingCount,
          isFollowing: !profile.isFollowing,
          isFriend: profile.isFriend,
          canBlock: profile.canBlock,
          badges: profile.badges,
          topStyles: profile.topStyles,
          lists: profile.lists,
        );
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _followBusy = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _confirmBlock() async {
    final profile = _profile;
    if (profile == null || _blockBusy) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Block @${profile.handle}?'),
        content: Text(
          'Their check-ins stay out of your feed, and they cannot follow you or comment on your check-ins. You can undo this here.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: Text('Block @${profile.handle}'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _blockBusy = true);
    try {
      await ref.read(apiProvider).blockUser(profile.handle);
      if (!mounted) return;
      setState(() {
        _blockBusy = false;
        _blocked = true;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _blockBusy = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _unblock() async {
    final profile = _profile;
    if (profile == null || _blockBusy) return;
    setState(() => _blockBusy = true);
    try {
      await ref.read(apiProvider).unblockUser(profile.handle);
      if (!mounted) return;
      setState(() {
        _blockBusy = false;
        _blocked = false;
      });
      await _load();
    } catch (error) {
      if (!mounted) return;
      setState(() => _blockBusy = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('@${_profile?.handle ?? widget.handle}'),
        actions: [
          if (_profile != null &&
              _profile!.canBlock &&
              !_blocked &&
              !_unavailable)
            PopupMenuButton<String>(
              tooltip: 'More options',
              onSelected: (value) {
                if (value == 'block') _confirmBlock();
              },
              itemBuilder: (context) => [
                PopupMenuItem(
                  value: 'block',
                  child: Text('Block @${_profile!.handle}'),
                ),
              ],
            ),
        ],
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading profile…');
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
        title: 'Could not load this profile',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    final profile = _profile!;
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          _header(profile),
          if (_blocked) ...[
            const SizedBox(height: 20),
            _blockedPanel(profile),
          ] else ...[
            const SizedBox(height: 18),
            _followButton(profile),
            const SizedBox(height: 24),
            _stats(profile),
            if (profile.badges.isNotEmpty) ...[
              const SizedBox(height: 28),
              const SodaSectionTitle(title: 'Badges'),
              const SizedBox(height: 10),
              _badges(profile.badges),
            ],
            if (profile.topStyles.isNotEmpty) ...[
              const SizedBox(height: 26),
              const SodaSectionTitle(title: 'Top styles'),
              const SizedBox(height: 10),
              _topStyles(profile.topStyles),
            ],
            if (profile.lists.isNotEmpty) ...[
              const SizedBox(height: 26),
              const SodaSectionTitle(title: 'Lists'),
              const SizedBox(height: 10),
              for (final list in profile.lists) ...[
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: _listRow(list),
                ),
              ],
            ],
          ],
        ],
      ),
    );
  }

  Widget _header(UserProfile profile) {
    final theme = Theme.of(context);
    final joined = sodaJoined(profile.createdAt);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaAvatar(
          initial: profile.displayName.isEmpty
              ? profile.handle
              : profile.displayName,
          size: 64,
        ),
        const SizedBox(width: 16),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                profile.displayName.isEmpty
                    ? '@${profile.handle}'
                    : profile.displayName,
                style: theme.textTheme.headlineSmall?.copyWith(
                  fontWeight: FontWeight.w800,
                ),
              ),
              const SizedBox(height: 2),
              Wrap(
                spacing: 8,
                runSpacing: 4,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  Text(
                    '@${profile.handle}',
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  if (profile.isFriend) const SodaPill(label: 'Friends'),
                ],
              ),
              if (joined.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(
                  joined,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              const SizedBox(height: 6),
              Text(
                '${sodaCount(profile.followerCount, 'follower')} · ${profile.followingCount} following',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              if (profile.bio.isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(profile.bio, style: theme.textTheme.bodyMedium),
              ],
            ],
          ),
        ),
      ],
    );
  }

  Widget _followButton(UserProfile profile) {
    if (profile.isFollowing) {
      return Align(
        alignment: Alignment.centerLeft,
        child: FilledButton.tonalIcon(
          onPressed: _followBusy ? null : _toggleFollow,
          icon: const Icon(Icons.check_rounded, size: 18),
          label: const Text('Following'),
        ),
      );
    }
    return Align(
      alignment: Alignment.centerLeft,
      child: FilledButton.icon(
        onPressed: _followBusy ? null : _toggleFollow,
        icon: const Icon(Icons.person_add_alt_1_rounded, size: 18),
        label: const Text('Follow'),
      ),
    );
  }

  Widget _stats(UserProfile profile) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Row(
          children: [
            _stat('${profile.stats.checkins}', 'Check-ins'),
            _divider(theme),
            _stat('${profile.stats.drinks}', 'Drinks'),
            _divider(theme),
            _stat('${profile.stats.toasts}', 'Toasts'),
          ],
        ),
      ),
    );
  }

  Widget _stat(String value, String label) {
    final theme = Theme.of(context);
    return Expanded(
      child: Column(
        children: [
          Text(
            value,
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 2),
          Text(
            label,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _divider(ThemeData theme) {
    return Container(
      width: 1,
      height: 34,
      color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
    );
  }

  Widget _badges(List<ProfileBadge> badges) {
    final theme = Theme.of(context);
    return SizedBox(
      height: 132,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        itemCount: badges.length,
        separatorBuilder: (_, _) => const SizedBox(width: 10),
        itemBuilder: (context, index) {
          final badge = badges[index];
          return SizedBox(
            width: 178,
            child: Card(
              margin: EdgeInsets.zero,
              child: Padding(
                padding: const EdgeInsets.all(14),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(
                      Icons.workspace_premium_outlined,
                      color: theme.colorScheme.onSurfaceVariant,
                      size: 20,
                    ),
                    const SizedBox(height: 6),
                    Text(
                      badge.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (badge.description.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Expanded(
                        child: Text(
                          badge.description,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                    ] else
                      const Spacer(),
                    if (badge.awardedAt.isNotEmpty)
                      Text(
                        sodaDate(badge.awardedAt),
                        style: theme.textTheme.labelSmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                  ],
                ),
              ),
            ),
          );
        },
      ),
    );
  }

  Widget _topStyles(List<ProfileTopStyle> styles) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final style in styles)
          SodaPill(label: '${style.name} · ${sodaCount(style.count, 'pour')}'),
      ],
    );
  }

  Widget _listRow(ListSummary list) {
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => ListDetailScreen.open(context, list.id, name: list.name),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      list.name,
                      style: Theme.of(context).textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      sodaCount(list.itemCount, 'soda'),
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: Theme.of(context).colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              Icon(
                Icons.chevron_right_rounded,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _blockedPanel(UserProfile profile) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'Blocked',
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w800,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              'You blocked @${profile.handle}. Their check-ins stay out of your feed, and they cannot follow you or comment on your check-ins.',
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 14),
            OutlinedButton(
              onPressed: _blockBusy ? null : _unblock,
              child: Text('Unblock @${profile.handle}'),
            ),
          ],
        ),
      ),
    );
  }
}
