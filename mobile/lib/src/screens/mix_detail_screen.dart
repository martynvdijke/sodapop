import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

/// One mix recipe with its component parts and volumes.
class MixDetailScreen extends ConsumerStatefulWidget {
  const MixDetailScreen({super.key, required this.mixId});

  final String mixId;

  static Future<void> open(BuildContext context, String mixId) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => MixDetailScreen(mixId: mixId)),
    );
  }

  @override
  ConsumerState<MixDetailScreen> createState() => _MixDetailScreenState();
}

class _MixDetailScreenState extends ConsumerState<MixDetailScreen> {
  MixRecipe? _mix;
  bool _loading = true;
  bool _unavailable = false;
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
      final mix = await ref.read(apiProvider).getMix(widget.mixId);
      if (!mounted) return;
      setState(() {
        _mix = mix;
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

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Mix')),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading mix…');
    if (_unavailable) {
      return SodaStatusView(
        icon: Icons.lock_outline_rounded,
        title: 'Mix unavailable',
        message: 'This mix is private or no longer exists.',
        actionLabel: 'Back',
        onAction: () => Navigator.of(context).maybePop(),
      );
    }
    if (_error != null || _mix == null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load this mix',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    final mix = _mix!;
    final theme = Theme.of(context);
    final byline = [
      if (mix.createdBy.isNotEmpty) '@${mix.createdBy}',
      if (mix.createdAt.isNotEmpty) sodaDate(mix.createdAt),
    ].join(' · ');
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          Text(
            mix.name,
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          if (byline.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              byline,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              if (mix.visibility.isNotEmpty)
                SodaPill(label: sodaListVisibility(mix.visibility)),
              SodaPill(label: sodaCount(mix.components.length, 'component')),
            ],
          ),
          if (mix.notes.isNotEmpty) ...[
            const SizedBox(height: 16),
            Text(mix.notes, style: theme.textTheme.bodyLarge),
          ],
          const SizedBox(height: 26),
          const SodaSectionTitle(title: 'Recipe'),
          const SizedBox(height: 10),
          if (mix.components.isEmpty)
            const SodaStatusView(
              dense: true,
              icon: Icons.local_drink_outlined,
              title: 'No components listed',
              message: 'This recipe is empty.',
            )
          else
            for (final component in mix.components) ...[
              _componentCard(component),
              const SizedBox(height: 10),
            ],
        ],
      ),
    );
  }

  Widget _componentCard(MixComponent component) {
    final theme = Theme.of(context);
    final meta = [
      if (component.parts > 0) sodaCount(component.parts, 'part'),
      if (component.volumeMl > 0) '${component.volumeMl} ml',
    ].join(' · ');
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(
                color: theme.colorScheme.primary.withValues(alpha: 0.22),
                borderRadius: BorderRadius.circular(12),
              ),
              child: Icon(
                Icons.local_drink_outlined,
                size: 22,
                color: theme.colorScheme.onSurface,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    component.name.isEmpty ? 'Soda' : component.name,
                    style: theme.textTheme.titleSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  if (meta.isNotEmpty) ...[
                    const SizedBox(height: 3),
                    Text(
                      meta,
                      style: theme.textTheme.bodySmall?.copyWith(
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
    );
  }
}
