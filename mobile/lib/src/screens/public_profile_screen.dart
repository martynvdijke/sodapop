import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/public_preview.dart';
import 'public_checkin_screen.dart';
import 'public_shell.dart';

/// A read-only public profile and its public pours.
class PublicProfileScreen extends ConsumerStatefulWidget {
  const PublicProfileScreen({super.key, required this.handle});

  final String handle;

  static Future<void> open(BuildContext context, String handle) =>
      Navigator.of(context).push(
        MaterialPageRoute(builder: (_) => PublicProfileScreen(handle: handle)),
      );

  @override
  ConsumerState<PublicProfileScreen> createState() =>
      _PublicProfileScreenState();
}

class _PublicProfileScreenState extends ConsumerState<PublicProfileScreen> {
  PublicProfile? _profile;
  List<FeedItem> _items = const [];
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
      final api = ref.read(apiProvider);
      final results = await Future.wait([
        api.publicUser(widget.handle),
        api.publicUserCheckins(widget.handle, limit: 30),
      ]);
      if (!mounted) return;
      setState(() {
        _profile = results[0] as PublicProfile;
        _items = results[1] as List<FeedItem>;
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
      appBar: AppBar(
        title: Text('@${widget.handle}'),
        actions: const [PreviewSignInButton()],
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
    final profile = _profile;
    if (_error != null || profile == null) {
      return ListView(
        children: [
          const SizedBox(height: 80),
          Center(
            child: Text(
              _error ?? 'Profile not found',
              textAlign: TextAlign.center,
            ),
          ),
          Center(
            child: TextButton(onPressed: _load, child: const Text('Try again')),
          ),
        ],
      );
    }
    final theme = Theme.of(context);
    final api = ref.read(apiProvider);
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Row(
          children: [
            CircleAvatar(
              radius: 30,
              backgroundColor: theme.colorScheme.primary.withValues(alpha: 0.3),
              child: Text(
                profile.displayName.isEmpty
                    ? profile.handle.characters.first.toUpperCase()
                    : profile.displayName.characters.first.toUpperCase(),
                style: theme.textTheme.headlineSmall?.copyWith(
                  fontWeight: FontWeight.w800,
                ),
              ),
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    profile.displayName.isEmpty
                        ? '@${profile.handle}'
                        : profile.displayName,
                    style: theme.textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                  Text(
                    '@${profile.handle}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
        if (profile.bio.isNotEmpty) ...[
          const SizedBox(height: 12),
          Text(profile.bio, style: theme.textTheme.bodyMedium),
        ],
        const SizedBox(height: 14),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            _StatChip(label: 'pours', value: profile.pours),
            _StatChip(label: 'unique', value: profile.uniqueDrinks),
            _StatChip(label: 'styles', value: profile.styles),
            _StatChip(label: 'toasts', value: profile.toasts),
          ],
        ),
        const SizedBox(height: 20),
        Text(
          'Public pours',
          style: theme.textTheme.titleMedium?.copyWith(
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(height: 10),
        if (_items.isEmpty)
          const Padding(
            padding: EdgeInsets.symmetric(vertical: 24),
            child: Center(child: Text('No public pours yet.')),
          )
        else
          ..._items.map(
            (item) => Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: PublicCheckinCard(
                item: item,
                api: api,
                onTap: () => PublicCheckinScreen.open(context, item.id),
              ),
            ),
          ),
      ],
    );
  }
}

class _StatChip extends StatelessWidget {
  const _StatChip({required this.label, required this.value});

  final String label;
  final int value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        '$value $label',
        style: theme.textTheme.bodySmall?.copyWith(fontWeight: FontWeight.w600),
      ),
    );
  }
}
