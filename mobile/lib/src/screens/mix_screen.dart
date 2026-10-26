import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'mix_create_screen.dart';
import 'mix_detail_screen.dart';

/// The signed-in member's own mix recipes.
class MixScreen extends ConsumerStatefulWidget {
  const MixScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const MixScreen()));
  }

  @override
  ConsumerState<MixScreen> createState() => _MixScreenState();
}

class _MixScreenState extends ConsumerState<MixScreen> {
  List<MixRecipe> _mixes = const [];
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
      final mixes = await ref.read(apiProvider).myMixes();
      if (!mounted) return;
      setState(() {
        _mixes = mixes;
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

  Future<void> _create() async {
    final created = await MixCreateScreen.open(context);
    if (!mounted || created == null) return;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('Mix created.')));
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Mix lab'),
        actions: [
          IconButton(
            onPressed: _create,
            tooltip: 'New mix',
            icon: const Icon(Icons.add_rounded),
          ),
        ],
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading mixes…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load your mixes',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_mixes.isEmpty) {
      return SodaStatusView(
        icon: Icons.science_outlined,
        title: 'No mixes yet',
        message: 'Build one from two to six sodas and give it a name.',
        actionLabel: 'Create a mix',
        onAction: _create,
      );
    }
    final theme = Theme.of(context);
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        itemCount: _mixes.length + 1,
        separatorBuilder: (_, _) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          if (index == 0) {
            return Text(
              'Two to six sodas, poured in parts.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            );
          }
          return _mixCard(_mixes[index - 1]);
        },
      ),
    );
  }

  Widget _mixCard(MixRecipe mix) {
    final theme = Theme.of(context);
    final meta = [
      sodaCount(mix.components.length, 'soda'),
      if (mix.visibility.isNotEmpty) sodaListVisibility(mix.visibility),
      if (mix.createdAt.isNotEmpty) sodaDate(mix.createdAt),
    ].where((value) => value.isNotEmpty).join(' · ');
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => MixDetailScreen.open(context, mix.id),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      mix.name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (mix.notes.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        mix.notes,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                    const SizedBox(height: 6),
                    Text(
                      meta,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 8),
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
