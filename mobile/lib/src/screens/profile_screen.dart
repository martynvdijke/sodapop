import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import 'insights_screen.dart';
import 'lists_screen.dart';
import 'mix_screen.dart';
import 'notifications_screen.dart';
import 'passport_screen.dart';
import 'saved_venues_screen.dart';
import 'taste_screen.dart';
import 'venues_screen.dart';

class ProfileScreen extends ConsumerWidget {
  const ProfileScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authProvider);
    final me = auth.me;
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('You')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          Row(
            children: [
              CircleAvatar(
                radius: 34,
                backgroundColor: theme.colorScheme.primary,
                child: Text(
                  (me?.displayName ?? '?').characters.first.toUpperCase(),
                  style: const TextStyle(
                    color: Soda.forest,
                    fontSize: 28,
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
              const SizedBox(width: 16),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      me?.displayName ?? '',
                      style: theme.textTheme.headlineSmall?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                    Text(
                      '@${me?.handle ?? ''}',
                      style: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 24),
          Text(
            'Activity',
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 10),
          Card(
            child: Padding(
              padding: const EdgeInsets.all(8),
              child: LayoutBuilder(
                builder: (context, constraints) {
                  const gap = 8.0;
                  final width = (constraints.maxWidth - gap) / 2;
                  final items = [
                    (
                      Icons.notifications_none_rounded,
                      'Notifications',
                      () => NotificationsScreen.open(context),
                    ),
                    (
                      Icons.insights_outlined,
                      'Insights',
                      () => InsightsScreen.open(context),
                    ),
                    (
                      Icons.people_outline_rounded,
                      'Taste',
                      () => TasteScreen.open(context),
                    ),
                    (
                      Icons.explore_outlined,
                      'Passport',
                      () => PassportScreen.open(context),
                    ),
                    (
                      Icons.queue_music_rounded,
                      'Lists',
                      () => ListsScreen.open(context),
                    ),
                    (
                      Icons.science_outlined,
                      'Mix',
                      () => MixScreen.open(context),
                    ),
                    (
                      Icons.place_outlined,
                      'Venues',
                      () => VenuesScreen.open(context),
                    ),
                    (
                      Icons.bookmark_border_rounded,
                      'Saved venues',
                      () => SavedVenuesScreen.open(context),
                    ),
                  ];
                  return Wrap(
                    spacing: gap,
                    runSpacing: gap,
                    children: [
                      for (final item in items)
                        SizedBox(
                          width: width,
                          child: _ActivityTile(
                            icon: item.$1,
                            label: item.$2,
                            onTap: item.$3,
                          ),
                        ),
                    ],
                  );
                },
              ),
            ),
          ),
          const SizedBox(height: 28),
          Text(
            'Account',
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 10),
          Card(
            child: Column(
              children: [
                ListTile(
                  leading: const Icon(Icons.dns_outlined),
                  title: const Text('Server'),
                  subtitle: Text(auth.serverUrl),
                ),
                const Divider(height: 1),
                ListTile(
                  leading: const Icon(Icons.key_outlined),
                  title: const Text('Personal API token'),
                  subtitle: const Text('Stored in Android encrypted storage'),
                  trailing: const Icon(Icons.check_circle, color: Soda.lime),
                ),
                const Divider(height: 1),
                ListTile(
                  leading: const Icon(Icons.mail_outline_rounded),
                  title: const Text('Email'),
                  subtitle: Text(me?.email ?? ''),
                ),
              ],
            ),
          ),
          const SizedBox(height: 24),
          FilledButton.tonal(
            onPressed: () => showDialog<void>(
              context: context,
              builder: (context) => AlertDialog(
                title: const Text('Sign out?'),
                content: const Text(
                  'The token is deleted from this device. Your check-ins stay on the server.',
                ),
                actions: [
                  TextButton(
                    onPressed: () => Navigator.pop(context),
                    child: const Text('Cancel'),
                  ),
                  FilledButton(
                    onPressed: () {
                      Navigator.pop(context);
                      ref.read(authProvider.notifier).signOut();
                    },
                    child: const Text('Sign out'),
                  ),
                ],
              ),
            ),
            child: const Text('Sign out'),
          ),
          const SizedBox(height: 28),
          Text(
            'Sodapop is a quiet, barcode-first soda log. There is no global feed and no public catalog API.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

class _ActivityTile extends StatelessWidget {
  const _ActivityTile({
    required this.icon,
    required this.label,
    required this.onTap,
  });

  final IconData icon;
  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Material(
      color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: SizedBox(
          height: 56,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10),
            child: Row(
              children: [
                Icon(icon, size: 20),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    label,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodySmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
