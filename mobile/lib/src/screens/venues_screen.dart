import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'saved_venues_screen.dart';
import 'venue_detail_screen.dart';

/// Venue search. Results are capped by the server and never listed in bulk.
class VenuesScreen extends ConsumerStatefulWidget {
  const VenuesScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const VenuesScreen()));
  }

  @override
  ConsumerState<VenuesScreen> createState() => _VenuesScreenState();
}

class _VenuesScreenState extends ConsumerState<VenuesScreen> {
  final _query = TextEditingController();
  List<Venue> _results = const [];
  bool _searching = false;
  bool _searched = false;
  String? _error;

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _search() async {
    final query = _query.text.trim();
    if (query.length < 2) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Type at least two characters.')),
      );
      return;
    }
    setState(() {
      _searching = true;
      _searched = true;
      _error = null;
    });
    FocusScope.of(context).unfocus();
    try {
      final results = await ref.read(apiProvider).venues(query);
      if (!mounted) return;
      setState(() {
        _results = results;
        _searching = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _searching = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Venues'),
        actions: [
          IconButton(
            onPressed: () => SavedVenuesScreen.open(context),
            tooltip: 'Saved venues',
            icon: const Icon(Icons.bookmark_border_rounded),
          ),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _query,
                    textInputAction: TextInputAction.search,
                    decoration: const InputDecoration(
                      hintText: 'Hotel, shop, café, city…',
                    ),
                    onSubmitted: (_) => _search(),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton.filled(
                  onPressed: _search,
                  tooltip: 'Search venues',
                  icon: const Icon(Icons.search_rounded),
                ),
              ],
            ),
          ),
          Expanded(child: _body()),
        ],
      ),
    );
  }

  Widget _body() {
    if (_searching) return const SodaLoadingView(label: 'Searching venues…');
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not search venues',
        message: _error,
        actionLabel: 'Try again',
        onAction: _search,
      );
    }
    if (!_searched) {
      return const SodaStatusView(
        icon: Icons.place_outlined,
        title: 'Find a venue',
        message: 'Search by name or city. Results are capped at ten.',
      );
    }
    if (_results.isEmpty) {
      return const SodaStatusView(
        icon: Icons.search_off_rounded,
        title: 'No venues matched',
        message: 'Try a shorter name or a nearby city.',
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      itemCount: _results.length,
      separatorBuilder: (_, _) => const SizedBox(height: 10),
      itemBuilder: (context, index) => _venueCard(_results[index]),
    );
  }

  Widget _venueCard(Venue venue) {
    final theme = Theme.of(context);
    final kind = sodaVenueKind(venue.kind);
    final meta = [
      kind,
      venue.city,
      venue.address,
    ].where((value) => value.isNotEmpty).join(' · ');
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () =>
            VenueDetailScreen.open(context, venue.id, name: venue.name),
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
                  Icons.place_outlined,
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
                      venue.name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (meta.isNotEmpty) ...[
                      const SizedBox(height: 3),
                      Text(
                        meta,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
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
}
