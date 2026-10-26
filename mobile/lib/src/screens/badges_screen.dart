import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

/// Every badge definition with the viewer's earned state and rule progress.
class BadgesScreen extends ConsumerStatefulWidget {
  const BadgesScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const BadgesScreen()));
  }

  @override
  ConsumerState<BadgesScreen> createState() => _BadgesScreenState();
}

class _BadgesScreenState extends ConsumerState<BadgesScreen> {
  List<BadgeStatus> _badges = const [];
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
      final badges = await ref.read(apiProvider).badges();
      if (!mounted) return;
      setState(() {
        _badges = badges;
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
      appBar: AppBar(title: const Text('Badges')),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading badges…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load badges',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    final earned = _badges.where((badge) => badge.earned).toList();
    final inProgress = _badges.where((badge) => !badge.earned).toList();
    if (_badges.isEmpty) {
      return const SodaStatusView(
        icon: Icons.workspace_premium_outlined,
        title: 'No badges yet',
        message: 'Badge definitions show up here once your server seeds them.',
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 36),
        children: [
          if (earned.isNotEmpty) ...[
            SodaSectionTitle(title: 'Earned', trailing: '${earned.length}'),
            const SizedBox(height: 8),
            for (final badge in earned) ...[
              _badgeCard(badge),
              const SizedBox(height: 10),
            ],
          ],
          if (inProgress.isNotEmpty) ...[
            if (earned.isNotEmpty) const SizedBox(height: 16),
            const SodaSectionTitle(title: 'In progress'),
            const SizedBox(height: 8),
            for (final badge in inProgress) ...[
              _badgeCard(badge),
              const SizedBox(height: 10),
            ],
          ],
        ],
      ),
    );
  }

  Widget _badgeCard(BadgeStatus badge) {
    final theme = Theme.of(context);
    final hasProgress = badge.target > 0;
    final percent = badge.percent.clamp(0, 100);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: badge.earned
                    ? theme.colorScheme.primary.withValues(alpha: 0.25)
                    : theme.colorScheme.onSurface.withValues(alpha: 0.05),
                borderRadius: BorderRadius.circular(12),
              ),
              child: Icon(
                Icons.workspace_premium_outlined,
                size: 22,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: Text(
                          badge.name,
                          style: theme.textTheme.titleSmall?.copyWith(
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                      ),
                      if (badge.level > 1) ...[
                        const SizedBox(width: 8),
                        SodaPill(label: 'Level ${badge.level}'),
                      ],
                    ],
                  ),
                  if (badge.description.isNotEmpty) ...[
                    const SizedBox(height: 3),
                    Text(
                      badge.description,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                  const SizedBox(height: 10),
                  if (badge.earned)
                    Row(
                      children: [
                        Icon(
                          Icons.check_circle_rounded,
                          size: 16,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                        const SizedBox(width: 6),
                        Text(
                          badge.awardedAt.isEmpty
                              ? 'Earned'
                              : 'Earned ${sodaDate(badge.awardedAt)}',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    )
                  else if (hasProgress) ...[
                    Row(
                      children: [
                        Text(
                          '${badge.progress} / ${badge.target}',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                        const Spacer(),
                        Text(
                          '$percent%',
                          style: theme.textTheme.labelSmall?.copyWith(
                            fontWeight: FontWeight.w700,
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 6),
                    ClipRRect(
                      borderRadius: BorderRadius.circular(4),
                      child: LinearProgressIndicator(
                        value: percent / 100,
                        minHeight: 6,
                        color: theme.colorScheme.primary,
                        backgroundColor: theme.colorScheme.onSurface.withValues(
                          alpha: 0.08,
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
