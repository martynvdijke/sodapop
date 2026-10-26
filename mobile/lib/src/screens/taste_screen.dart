import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'user_profile_screen.dart';

enum _TasteTab { twins, forYou }

/// Taste twins and personal recommendations. Both lists are personal and
/// friend-scoped; there is no global ranking here.
class TasteScreen extends ConsumerStatefulWidget {
  const TasteScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const TasteScreen()));
  }

  @override
  ConsumerState<TasteScreen> createState() => _TasteScreenState();
}

class _TasteScreenState extends ConsumerState<TasteScreen> {
  _TasteTab _tab = _TasteTab.twins;
  List<Twin> _twins = const [];
  List<Recommendation> _recommendations = const [];
  bool _loading = true;
  String? _twinsError;
  String? _recommendationsError;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _twinsError = null;
      _recommendationsError = null;
    });
    final api = ref.read(apiProvider);
    try {
      final twins = await api.tasteTwins();
      if (!mounted) return;
      setState(() => _twins = twins);
    } catch (error) {
      if (!mounted) return;
      setState(() => _twinsError = ApiClient.describe(error).message);
    }
    try {
      final recommendations = await api.tasteRecommendations();
      if (!mounted) return;
      setState(() => _recommendations = recommendations);
    } catch (error) {
      if (!mounted) return;
      setState(() => _recommendationsError = ApiClient.describe(error).message);
    }
    if (!mounted) return;
    setState(() => _loading = false);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Taste')),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 0),
            child: SodaSegmented<_TasteTab>(
              options: const [
                (_TasteTab.twins, 'Taste twins'),
                (_TasteTab.forYou, 'For you'),
              ],
              selected: _tab,
              onChanged: (value) => setState(() => _tab = value),
            ),
          ),
          Expanded(child: _body()),
        ],
      ),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Comparing palates…');
    if (_tab == _TasteTab.twins) return _twinsBody();
    return _recommendationsBody();
  }

  Widget _twinsBody() {
    if (_twinsError != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load taste twins',
        message: _twinsError,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_twins.isEmpty) {
      return const SodaStatusView(
        icon: Icons.people_outline_rounded,
        title: 'No taste twins yet',
        message:
            'Rate at least three sodas your friends have also rated, and matches show up here.',
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        itemCount: _twins.length + 1,
        separatorBuilder: (_, _) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          if (index == 0) {
            return Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(
                'Built from mutual friends and shared ratings.',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            );
          }
          return _twinCard(_twins[index - 1]);
        },
      ),
    );
  }

  Widget _twinCard(Twin twin) {
    final theme = Theme.of(context);
    final name = twin.displayName.isEmpty ? twin.handle : twin.displayName;
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: twin.handle.isEmpty
            ? null
            : () => UserProfileScreen.open(context, twin.handle),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              SodaAvatar(initial: name.isEmpty ? twin.handle : name, size: 44),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (twin.handle.isNotEmpty)
                      Text(
                        '@${twin.handle}',
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    const SizedBox(height: 4),
                    Text(
                      '${sodaCount(twin.commonDrinks, 'drink')} in common',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 10),
              SodaPill(label: '${twin.score}% match'),
            ],
          ),
        ),
      ),
    );
  }

  Widget _recommendationsBody() {
    if (_recommendationsError != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load recommendations',
        message: _recommendationsError,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_recommendations.isEmpty) {
      return const SodaStatusView(
        icon: Icons.local_drink_outlined,
        title: 'No recommendations yet',
        message: 'Add a few friends and ratings first.',
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        itemCount: _recommendations.length + 1,
        separatorBuilder: (_, _) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          if (index == 0) {
            return Padding(
              padding: const EdgeInsets.only(bottom: 2),
              child: Text(
                'Sodas your taste twins rate highly that you have not logged.',
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
            );
          }
          return _recommendationCard(_recommendations[index - 1]);
        },
      ),
    );
  }

  Widget _recommendationCard(Recommendation recommendation) {
    final theme = Theme.of(context);
    final subtitle = [
      recommendation.brand,
      recommendation.style,
    ].where((value) => value.isNotEmpty).join(' · ');
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              recommendation.name,
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            if (subtitle.isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                subtitle,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            if (recommendation.reason.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(recommendation.reason, style: theme.textTheme.bodyMedium),
            ],
          ],
        ),
      ),
    );
  }
}
