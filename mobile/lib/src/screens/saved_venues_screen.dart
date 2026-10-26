import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'venue_detail_screen.dart';

/// Venues the member bookmarked. Tapping one opens the venue, where it can be
/// unsaved.
class SavedVenuesScreen extends ConsumerStatefulWidget {
  const SavedVenuesScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const SavedVenuesScreen()));
  }

  @override
  ConsumerState<SavedVenuesScreen> createState() => _SavedVenuesScreenState();
}

class _SavedVenuesScreenState extends ConsumerState<SavedVenuesScreen> {
  List<SavedVenue> _venues = const [];
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
      final venues = await ref.read(apiProvider).savedVenues();
      if (!mounted) return;
      setState(() {
        _venues = venues;
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
      appBar: AppBar(title: const Text('Saved venues')),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading venues…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load saved venues',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    if (_venues.isEmpty) {
      return const SodaStatusView(
        icon: Icons.bookmark_border_rounded,
        title: 'No saved venues yet',
        message: 'Open a venue and tap Save venue, and it shows up here.',
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView.separated(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        itemCount: _venues.length + 1,
        separatorBuilder: (_, _) => const SizedBox(height: 10),
        itemBuilder: (context, index) {
          if (index == 0) {
            return Text(
              'Places you bookmarked. Newest first.',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            );
          }
          return _venueCard(_venues[index - 1]);
        },
      ),
    );
  }

  Widget _venueCard(SavedVenue venue) {
    final theme = Theme.of(context);
    final kind = sodaVenueKind(venue.kind);
    final meta = [
      kind,
      venue.city,
    ].where((value) => value.isNotEmpty).join(' · ');
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () async {
          await VenueDetailScreen.open(context, venue.id, name: venue.name);
          if (mounted) await _load();
        },
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: theme.colorScheme.primary.withValues(alpha: 0.25),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(
                  _kindIcon(venue.kind),
                  color: theme.colorScheme.onSurface,
                  size: 22,
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      venue.name,
                      style: theme.textTheme.titleMedium?.copyWith(
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
              const SizedBox(width: 6),
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

  static IconData _kindIcon(String kind) {
    switch (kind) {
      case 'bar':
        return Icons.sports_bar_outlined;
      case 'restaurant':
        return Icons.restaurant_outlined;
      case 'cafe':
        return Icons.local_cafe_outlined;
      case 'shop':
        return Icons.storefront_outlined;
      case 'hotel':
        return Icons.hotel_outlined;
      case 'festival':
        return Icons.festival_outlined;
      case 'vending':
        return Icons.point_of_sale_outlined;
      default:
        return Icons.place_outlined;
    }
  }
}
