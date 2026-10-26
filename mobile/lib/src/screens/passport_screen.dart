import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'badges_screen.dart';

/// Lifetime stats and earned badges. The full badge list lives behind the
/// "See all badges" link and adds progress for unearned stamps.
class PassportScreen extends ConsumerStatefulWidget {
  const PassportScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const PassportScreen()));
  }

  @override
  ConsumerState<PassportScreen> createState() => _PassportScreenState();
}

class _PassportScreenState extends ConsumerState<PassportScreen> {
  Passport? _passport;
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
      final passport = await ref.read(apiProvider).passport();
      if (!mounted) return;
      setState(() {
        _passport = passport;
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
      appBar: AppBar(title: const Text('Passport')),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Opening passport…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load your passport',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    final passport = _passport!;
    final theme = Theme.of(context);
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          Text(
            'Every check-in is a stamp.',
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            'Drinks tried, toasts received, and badges earned.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 20),
          _stats(passport.stats),
          const SizedBox(height: 28),
          SodaSectionTitle(
            title: 'Earned badges',
            trailing: passport.badges.isEmpty
                ? null
                : '${passport.badges.length}',
          ),
          const SizedBox(height: 10),
          if (passport.badges.isEmpty)
            const SodaStatusView(
              dense: true,
              icon: Icons.workspace_premium_outlined,
              title: 'No badges yet',
              message: 'Check in a few sodas and stamps start showing up.',
            )
          else
            _badgeGrid(passport.badges),
          const SizedBox(height: 8),
          Center(
            child: TextButton.icon(
              onPressed: () => BadgesScreen.open(context),
              icon: const Icon(Icons.arrow_forward_rounded, size: 18),
              label: const Text('See all badges'),
            ),
          ),
        ],
      ),
    );
  }

  Widget _stats(ProfileStats stats) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(18),
        child: Column(
          children: [
            Row(
              children: [
                Expanded(child: _stat('${stats.checkins}', 'Check-ins')),
                _divider(theme),
                Expanded(child: _stat('${stats.drinks}', 'Drinks')),
              ],
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(child: _stat('${stats.toasts}', 'Toasts')),
                _divider(theme),
                Expanded(child: _stat('${stats.badges}', 'Badges')),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _stat(String value, String label) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
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
    );
  }

  Widget _divider(ThemeData theme) {
    return Container(
      width: 1,
      height: 42,
      margin: const EdgeInsets.symmetric(horizontal: 8),
      color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
    );
  }

  Widget _badgeGrid(List<ProfileBadge> badges) {
    return LayoutBuilder(
      builder: (context, constraints) {
        const gap = 10.0;
        final width = (constraints.maxWidth - gap) / 2;
        return Wrap(
          spacing: gap,
          runSpacing: gap,
          children: [
            for (final badge in badges)
              SizedBox(width: width, height: 150, child: _badgeTile(badge)),
          ],
        );
      },
    );
  }

  Widget _badgeTile(ProfileBadge badge) {
    final theme = Theme.of(context);
    return Card(
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              Icons.workspace_premium_outlined,
              size: 22,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            const SizedBox(height: 6),
            Text(
              badge.name,
              maxLines: 2,
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
                'Earned ${sodaDate(badge.awardedAt)}',
                style: theme.textTheme.labelSmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
          ],
        ),
      ),
    );
  }
}
