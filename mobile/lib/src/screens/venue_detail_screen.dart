import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

/// One venue with its recent drinks and community photos.
class VenueDetailScreen extends ConsumerStatefulWidget {
  const VenueDetailScreen({super.key, required this.venueId, this.name = ''});

  final int venueId;
  final String name;

  static Future<void> open(
    BuildContext context,
    int venueId, {
    String name = '',
  }) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => VenueDetailScreen(venueId: venueId, name: name),
      ),
    );
  }

  @override
  ConsumerState<VenueDetailScreen> createState() => _VenueDetailScreenState();
}

class _VenueDetailScreenState extends ConsumerState<VenueDetailScreen> {
  VenueDetail? _detail;
  List<VenueMedia> _media = const [];
  bool _loading = true;
  bool _unavailable = false;
  bool _saved = false;
  bool _savedLoading = true;
  bool _saving = false;
  bool _mediaLoading = true;
  String? _error;
  String? _mediaError;

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
      _mediaLoading = true;
      _mediaError = null;
      _savedLoading = true;
    });
    try {
      final detail = await ref.read(apiProvider).venue(widget.venueId);
      if (!mounted) return;
      setState(() {
        _detail = detail;
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
      return;
    }
    await Future.wait([_loadMedia(), _loadSaved()]);
  }

  Future<void> _loadMedia() async {
    setState(() {
      _mediaLoading = true;
      _mediaError = null;
    });
    try {
      final media = await ref.read(apiProvider).venueMedia(widget.venueId);
      if (!mounted) return;
      setState(() {
        _media = media.where((row) => row.mediaId.isNotEmpty).toList();
        _mediaLoading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _mediaLoading = false;
        _mediaError = ApiClient.describe(error).message;
      });
    }
  }

  Future<void> _loadSaved() async {
    try {
      final saved = await ref.read(apiProvider).savedVenues();
      if (!mounted) return;
      setState(() {
        _saved = saved.any((venue) => venue.id == widget.venueId);
        _savedLoading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _savedLoading = false);
    }
  }

  Future<void> _toggleSaved() async {
    if (_saving || _savedLoading) return;
    setState(() => _saving = true);
    final api = ref.read(apiProvider);
    try {
      if (_saved) {
        await api.unsaveVenue(widget.venueId);
      } else {
        await api.saveVenue(widget.venueId);
      }
      if (!mounted) return;
      setState(() {
        _saved = !_saved;
        _saving = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _saving = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Venue')),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading venue…');
    if (_unavailable) {
      return SodaStatusView(
        icon: Icons.place_outlined,
        title: 'Venue unavailable',
        message: 'This venue is no longer listed.',
        actionLabel: 'Back',
        onAction: () => Navigator.of(context).maybePop(),
      );
    }
    if (_error != null || _detail == null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load this venue',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    final detail = _detail!;
    final venue = detail.venue;
    final theme = Theme.of(context);
    final eyebrow = [
      sodaVenueKind(venue.kind),
      venue.city,
    ].where((value) => value.isNotEmpty).join(' · ');
    final address = [
      venue.address,
      venue.region,
      venue.country,
    ].where((value) => value.isNotEmpty).join(' · ');

    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          if (eyebrow.isNotEmpty)
            Text(
              eyebrow,
              style: theme.textTheme.labelLarge?.copyWith(
                fontWeight: FontWeight.w700,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          const SizedBox(height: 4),
          Text(
            venue.name.isEmpty ? widget.name : venue.name,
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          if (address.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(
              address,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          const SizedBox(height: 18),
          _saveButton(),
          const SizedBox(height: 26),
          SodaSectionTitle(
            title: 'Seen here',
            trailing: detail.drinks.isEmpty ? null : '${detail.drinks.length}',
          ),
          const SizedBox(height: 8),
          if (detail.drinks.isEmpty)
            const SodaStatusView(
              dense: true,
              icon: Icons.local_drink_outlined,
              title: 'No sodas logged here yet',
              message: 'Be the first to check in one at this venue.',
            )
          else
            for (final drink in detail.drinks) ...[
              _drinkCard(drink),
              const SizedBox(height: 10),
            ],
          const SizedBox(height: 20),
          SodaSectionTitle(
            title: 'Photos and menus',
            trailing: _media.isEmpty || _mediaLoading
                ? null
                : '${_media.length}',
          ),
          const SizedBox(height: 8),
          _gallery(),
        ],
      ),
    );
  }

  Widget _saveButton() {
    if (_savedLoading) {
      return const OutlinedButton(
        onPressed: null,
        child: Text('Checking saved…'),
      );
    }
    if (_saved) {
      return Tooltip(
        message: 'Remove from saved venues',
        child: FilledButton.tonalIcon(
          onPressed: _saving ? null : _toggleSaved,
          icon: const Icon(Icons.bookmark_rounded, size: 18),
          label: Text(_saving ? 'Saving…' : 'Saved'),
        ),
      );
    }
    return FilledButton.icon(
      onPressed: _saving ? null : _toggleSaved,
      icon: const Icon(Icons.bookmark_add_outlined, size: 18),
      label: Text(_saving ? 'Saving…' : 'Save venue'),
    );
  }

  Widget _drinkCard(VenueDrink drink) {
    final theme = Theme.of(context);
    final meta = [
      if (drink.brand.isNotEmpty) drink.brand,
      if (drink.lastSeenAt.isNotEmpty)
        'last seen ${sodaDate(drink.lastSeenAt)}',
    ].join(' · ');
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Text(
                    drink.name.isEmpty ? 'Soda' : drink.name,
                    style: theme.textTheme.titleSmall?.copyWith(
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                if (drink.onOffer) ...[
                  const SizedBox(width: 8),
                  const SodaPill(label: 'On offer'),
                ],
              ],
            ),
            if (meta.isNotEmpty) ...[
              const SizedBox(height: 4),
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
    );
  }

  Widget _gallery() {
    if (_mediaLoading) return const SodaLoadingView(dense: true);
    if (_mediaError != null) {
      return SodaStatusView(
        dense: true,
        icon: Icons.image_not_supported_outlined,
        title: 'Photos did not load',
        message: _mediaError,
        actionLabel: 'Try again',
        onAction: _loadMedia,
      );
    }
    if (_media.isEmpty) {
      return const SodaStatusView(
        dense: true,
        icon: Icons.photo_library_outlined,
        title: 'No photos or menus yet',
        message: 'People who visit can add photos and menu shots.',
      );
    }
    final api = ref.read(apiProvider);
    final configured = api.configured;
    return SizedBox(
      height: 138,
      child: ListView.separated(
        scrollDirection: Axis.horizontal,
        itemCount: _media.length,
        separatorBuilder: (_, _) => const SizedBox(width: 10),
        itemBuilder: (context, index) {
          final media = _media[index];
          final caption = [
            media.kind == 'menu' ? 'Menu' : 'Photo',
            if (media.label.isNotEmpty) media.label,
          ].join(' · ');
          return SizedBox(
            width: 118,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(12),
                    child: SizedBox(
                      width: double.infinity,
                      child: !configured
                          ? const _MediaPlaceholder()
                          : Image.network(
                              '${api.mediaUrl(media.mediaId)}/thumb',
                              headers: api.imageHeaders,
                              fit: BoxFit.cover,
                              semanticLabel:
                                  '$caption at ${_detail?.venue.name ?? 'this venue'}',
                              errorBuilder: (context, error, stackTrace) =>
                                  const _MediaPlaceholder(),
                            ),
                    ),
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  caption,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          );
        },
      ),
    );
  }
}

class _MediaPlaceholder extends StatelessWidget {
  const _MediaPlaceholder();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
      alignment: Alignment.center,
      child: Icon(
        Icons.image_not_supported_outlined,
        color: theme.colorScheme.onSurfaceVariant,
      ),
    );
  }
}
