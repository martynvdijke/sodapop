import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'drink_detail_screen.dart';
import 'user_profile_screen.dart';

/// Reads the device position. Injectable so widget tests never touch a platform
/// channel.
typedef LocationProvider = Future<Position> Function();

/// A distance bucket, not the exact distance. Nearby is about what is close,
/// never about pinpointing a pour or a person.
String nearbyDistanceLabel(int distanceM) {
  if (distanceM < 100) return 'Within 100 m';
  if (distanceM < 300) return 'Within 300 m';
  if (distanceM < 500) return 'Within 500 m';
  if (distanceM < 1000) return 'Within 1 km';
  final tenths = (distanceM / 100).ceil();
  final km = tenths / 10;
  if (km == km.roundToDouble()) return 'Within ${km.toInt()} km';
  return 'Within ${km.toStringAsFixed(1)} km';
}

/// A short, human relative time for feed rows. Falls back to a calendar date
/// for anything older than a week.
String nearbyRelativeTime(String raw, {DateTime? now}) {
  final parsed = DateTime.tryParse(raw.trim());
  if (parsed == null) return '';
  final local = (now ?? DateTime.now()).toLocal();
  final difference = local.difference(parsed.toLocal());
  if (difference.isNegative || difference.inSeconds < 60) return 'Just now';
  if (difference.inMinutes < 60) return '${difference.inMinutes}m ago';
  if (difference.inHours < 24) return '${difference.inHours}h ago';
  if (difference.inDays < 7) return '${difference.inDays}d ago';
  return sodaDate(raw);
}

class NearbyScreen extends ConsumerStatefulWidget {
  const NearbyScreen({
    super.key,
    this.locationProvider,
    this.permissionChecker,
    this.permissionRequester,
    this.serviceChecker,
    this.settingsOpener,
    this.serviceSettingsOpener,
  });

  /// Defaults to a medium-accuracy fix with a ten second limit.
  final LocationProvider? locationProvider;

  /// The remaining hooks exist so tests can cover denied and disabled services
  /// without a platform channel. Production uses [Geolocator].
  final Future<LocationPermission> Function()? permissionChecker;
  final Future<LocationPermission> Function()? permissionRequester;
  final Future<bool> Function()? serviceChecker;
  final Future<bool> Function()? settingsOpener;
  final Future<bool> Function()? serviceSettingsOpener;

  static const radiusOptions = [0.5, 1.0, 2.0, 5.0, 10.0, 25.0];

  static Future<void> open(BuildContext context) {
    return Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const NearbyScreen()));
  }

  @override
  ConsumerState<NearbyScreen> createState() => _NearbyScreenState();
}

class _NearbyScreenState extends ConsumerState<NearbyScreen> {
  /// Coordinates live in memory for this visit only, for the request at hand.
  Position? _position;
  double _radiusKm = 2;

  final List<NearbyItem> _items = [];
  final Set<String> _seen = {};
  bool _loading = false;
  bool _loadingMore = false;
  bool _locating = false;
  bool _loaded = false;
  bool _hasMore = false;
  String _nextBefore = '';
  String? _error;
  String? _locationMessage;
  bool _needsSettings = false;
  bool _needsService = false;
  int _requestId = 0;

  static Future<Position> _defaultPosition() => Geolocator.getCurrentPosition(
    locationSettings: const LocationSettings(
      accuracy: LocationAccuracy.medium,
      timeLimit: Duration(seconds: 10),
    ),
  );

  /// Permission is requested from the tap, never on load.
  Future<void> _requestLocation() async {
    if (_locating) return;
    setState(() {
      _locating = true;
      _locationMessage = null;
      _needsSettings = false;
      _needsService = false;
    });
    try {
      final enabled =
          await (widget.serviceChecker ??
              Geolocator.isLocationServiceEnabled)();
      if (!mounted) return;
      if (!enabled) {
        setState(() {
          _locating = false;
          _needsService = true;
          _locationMessage =
              'Location services are off. Turn them on to see nearby pours.';
        });
        return;
      }

      var permission =
          await (widget.permissionChecker ?? Geolocator.checkPermission)();
      if (permission == LocationPermission.denied) {
        permission =
            await (widget.permissionRequester ??
                Geolocator.requestPermission)();
      }
      if (!mounted) return;
      if (permission == LocationPermission.deniedForever) {
        setState(() {
          _locating = false;
          _needsSettings = true;
          _locationMessage =
              'Location is blocked for Sodapop. Allow it in settings to see nearby pours.';
        });
        return;
      }
      if (permission == LocationPermission.denied ||
          permission == LocationPermission.unableToDetermine) {
        setState(() {
          _locating = false;
          _locationMessage =
              'Location permission was declined. Nothing is shared until you allow it.';
        });
        return;
      }

      final position = await (widget.locationProvider ?? _defaultPosition)();
      if (!mounted) return;
      setState(() {
        _position = position;
        _locating = false;
        _locationMessage = null;
      });
      await _load();
    } on TimeoutException {
      if (!mounted) return;
      setState(() {
        _locating = false;
        _locationMessage = 'Finding your location took too long. Try again.';
      });
    } on LocationServiceDisabledException {
      if (!mounted) return;
      setState(() {
        _locating = false;
        _needsService = true;
        _locationMessage =
            'Location services are off. Turn them on to see nearby pours.';
      });
    } on PermissionDeniedException {
      if (!mounted) return;
      setState(() {
        _locating = false;
        _locationMessage = 'Location permission is needed to see nearby pours.';
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _locating = false;
        _locationMessage = 'Could not read your location. Try again.';
      });
    }
  }

  Future<void> _load({bool quiet = false}) async {
    final position = _position;
    if (position == null) return;
    final request = ++_requestId;
    if (!quiet) {
      setState(() {
        _loading = true;
        // A new full load supersedes any page fetch still in flight, so drop
        // its spinner rather than leaving it stuck on the fresh list.
        _loadingMore = false;
        _error = null;
      });
    } else {
      setState(() {
        _loadingMore = false;
        _error = null;
      });
    }
    // Metre-level precision is plenty for a distance bucket, and it keeps the
    // query string in step with the web client.
    final lat3 = double.parse(position.latitude.toStringAsFixed(3));
    final lng3 = double.parse(position.longitude.toStringAsFixed(3));
    try {
      final page = await ref
          .read(apiProvider)
          .nearby(lat: lat3, lng: lng3, radiusKm: _radiusKm);
      if (!mounted || request != _requestId) return;
      final fresh = <NearbyItem>[];
      final seen = <String>{};
      for (final item in page.items) {
        final id = item.checkin.id;
        if (id.isNotEmpty && !seen.add(id)) continue;
        fresh.add(item);
      }
      setState(() {
        _items
          ..clear()
          ..addAll(fresh);
        _seen
          ..clear()
          ..addAll(seen);
        _hasMore = page.hasMore && page.nextBefore.isNotEmpty;
        _nextBefore = page.nextBefore;
        _loading = false;
        _loaded = true;
        _error = null;
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      final message = ApiClient.describe(error).message;
      if (quiet && _items.isNotEmpty) {
        setState(() => _loading = false);
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(message)));
        return;
      }
      setState(() {
        _loading = false;
        _loaded = true;
        _error = message;
      });
    }
  }

  Future<void> _loadMore() async {
    final position = _position;
    if (position == null ||
        _loading ||
        _loadingMore ||
        !_hasMore ||
        _nextBefore.isEmpty) {
      return;
    }
    final request = _requestId;
    setState(() => _loadingMore = true);
    // Metre-level precision is plenty for a distance bucket, and it keeps the
    // query string in step with the web client.
    final lat3 = double.parse(position.latitude.toStringAsFixed(3));
    final lng3 = double.parse(position.longitude.toStringAsFixed(3));
    try {
      final page = await ref
          .read(apiProvider)
          .nearby(
            lat: lat3,
            lng: lng3,
            radiusKm: _radiusKm,
            before: _nextBefore,
          );
      if (!mounted || request != _requestId) return;
      final added = <NearbyItem>[];
      final seen = <String>{..._seen};
      for (final item in page.items) {
        final id = item.checkin.id;
        if (id.isNotEmpty && !seen.add(id)) continue;
        added.add(item);
      }
      setState(() {
        _items.addAll(added);
        _seen
          ..clear()
          ..addAll(seen);
        _hasMore = page.hasMore && page.nextBefore.isNotEmpty;
        _nextBefore = page.nextBefore;
        _loadingMore = false;
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      setState(() => _loadingMore = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  /// Pull to refresh only makes a request when a location is already set.
  Future<void> _refresh() async {
    if (_position == null) return;
    await _load(quiet: true);
  }

  Future<void> _setRadius(double radiusKm) async {
    if (radiusKm == _radiusKm) return;
    setState(() => _radiusKm = radiusKm);
    if (_position != null) await _load();
  }

  Future<void> _openSettings() async {
    await (widget.settingsOpener ?? Geolocator.openAppSettings)();
  }

  Future<void> _openLocationSettings() async {
    await (widget.serviceSettingsOpener ?? Geolocator.openLocationSettings)();
  }

  void _openDrink(NearbyItem item) {
    final slug = item.checkin.drinkSlug;
    if (slug.isEmpty) return;
    DrinkDetailScreen.open(context, slug, name: item.checkin.drinkName);
  }

  void _openAuthor(String handle) {
    if (handle.isEmpty) return;
    UserProfileScreen.open(context, handle);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Nearby')),
      body: RefreshIndicator(
        color: Soda.lime,
        onRefresh: _refresh,
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
          children: [
            _header(theme),
            const SizedBox(height: 16),
            _radiusPicker(theme),
            if (_locationMessage != null) ...[
              const SizedBox(height: 12),
              _messageCard(theme),
            ],
            const SizedBox(height: 20),
            _results(theme),
          ],
        ),
      ),
    );
  }

  Widget _header(ThemeData theme) {
    final located = _position != null;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.primary.withValues(alpha: 0.14),
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.primary.withValues(alpha: 0.42),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 38,
                height: 38,
                decoration: BoxDecoration(
                  color: theme.colorScheme.primary,
                  shape: BoxShape.circle,
                ),
                child: Icon(
                  Icons.near_me_rounded,
                  size: 20,
                  color: theme.colorScheme.onPrimary,
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  'Pours around you',
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(
            'Your coordinates are sent to your server for this request only and are never stored. Results are limited to people you can already see.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
              height: 1.35,
            ),
          ),
          const SizedBox(height: 14),
          FilledButton.icon(
            key: const Key('nearby-use-location'),
            onPressed: _locating ? null : _requestLocation,
            icon: _locating
                ? SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: theme.colorScheme.onPrimary,
                    ),
                  )
                : const Icon(Icons.my_location_rounded, size: 18),
            label: Text(located ? 'Update location' : 'Use my location'),
          ),
        ],
      ),
    );
  }

  Widget _radiusPicker(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'Search radius',
          style: theme.textTheme.titleSmall?.copyWith(
            fontWeight: FontWeight.w700,
          ),
        ),
        const SizedBox(height: 8),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final radius in NearbyScreen.radiusOptions)
              _RadiusChip(
                radiusKm: radius,
                selected: radius == _radiusKm,
                onTap: () => _setRadius(radius),
              ),
          ],
        ),
      ],
    );
  }

  Widget _messageCard(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer.withValues(alpha: 0.55),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                Icons.info_outline_rounded,
                size: 18,
                color: theme.colorScheme.onSecondaryContainer,
              ),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  _locationMessage ?? '',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSecondaryContainer,
                  ),
                ),
              ),
            ],
          ),
          if (_needsSettings)
            Align(
              alignment: Alignment.centerRight,
              child: TextButton(
                key: const Key('nearby-open-settings'),
                onPressed: _openSettings,
                child: const Text('Open settings'),
              ),
            ),
          if (_needsService)
            Align(
              alignment: Alignment.centerRight,
              child: TextButton(
                key: const Key('nearby-enable-services'),
                onPressed: _openLocationSettings,
                child: const Text('Enable location services'),
              ),
            ),
        ],
      ),
    );
  }

  Widget _results(ThemeData theme) {
    final located = _position != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.baseline,
          textBaseline: TextBaseline.alphabetic,
          children: [
            Expanded(
              child: Text(
                'Nearby pours',
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: FontWeight.w800,
                ),
              ),
            ),
            if (located && _items.isNotEmpty)
              Text(
                '${_items.length} shown',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
          ],
        ),
        const SizedBox(height: 10),
        if (!located)
          _status(
            theme,
            icon: Icons.location_searching_rounded,
            title: 'No location yet',
            message:
                'Nothing loads until you tap Use my location. The feed stays quiet otherwise.',
          )
        else if (_loading && _items.isEmpty)
          const SodaLoadingView(dense: true, label: 'Finding nearby pours')
        else if (_error != null && _items.isEmpty)
          _status(
            theme,
            icon: Icons.cloud_off_rounded,
            title: 'Could not load nearby pours',
            message: _error!,
            actionLabel: 'Try again',
            actionKey: const Key('nearby-retry'),
            onAction: _load,
          )
        else if (_loaded && _items.isEmpty)
          _emptyState(theme)
        else ...[
          for (final item in _items)
            Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: _NearbyCard(
                item: item,
                api: ref.read(apiProvider),
                onOpenDrink: () => _openDrink(item),
                onOpenAuthor: () => _openAuthor(item.checkin.handle),
              ),
            ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: Text(
                _error!,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.error,
                ),
              ),
            ),
          if (_hasMore)
            Center(
              child: Padding(
                padding: const EdgeInsets.only(top: 2),
                child: _loadingMore
                    ? const SodaLoadingView(dense: true)
                    : OutlinedButton.icon(
                        key: const Key('nearby-load-more'),
                        onPressed: _loadMore,
                        icon: const Icon(Icons.expand_more_rounded, size: 18),
                        label: const Text('More nearby pours'),
                      ),
              ),
            ),
        ],
      ],
    );
  }

  Widget _emptyState(ThemeData theme) {
    final wider = NearbyScreen.radiusOptions
        .where((radius) => radius > _radiusKm)
        .firstOrNull;
    return _status(
      theme,
      icon: Icons.location_off_outlined,
      title: 'No nearby pours',
      message:
          'Nothing within ${_radiusLabel(_radiusKm)}. Widen the radius to look further.',
      actionLabel: wider == null ? null : 'Widen to ${_radiusLabel(wider)}',
      actionKey: const Key('nearby-widen'),
      onAction: wider == null ? null : () => _setRadius(wider),
    );
  }

  Widget _status(
    ThemeData theme, {
    required IconData icon,
    required String title,
    required String message,
    String? actionLabel,
    Key? actionKey,
    VoidCallback? onAction,
  }) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(20, 26, 20, 20),
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.12),
        ),
      ),
      child: Column(
        children: [
          Icon(icon, size: 38, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(height: 12),
          Text(
            title,
            textAlign: TextAlign.center,
            style: theme.textTheme.titleSmall?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            message,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
              height: 1.35,
            ),
          ),
          if (actionLabel != null) ...[
            const SizedBox(height: 10),
            TextButton(
              key: actionKey,
              onPressed: onAction,
              child: Text(actionLabel),
            ),
          ],
        ],
      ),
    );
  }
}

class _RadiusChip extends StatelessWidget {
  const _RadiusChip({
    required this.radiusKm,
    required this.selected,
    required this.onTap,
  });

  final double radiusKm;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final label = _radiusLabel(radiusKm);
    return Semantics(
      button: true,
      selected: selected,
      label: '$label radius',
      child: Material(
        color: selected ? theme.colorScheme.primary : theme.colorScheme.surface,
        shape: StadiumBorder(
          side: BorderSide(
            color: selected
                ? Colors.transparent
                : theme.colorScheme.onSurface.withValues(alpha: 0.18),
          ),
        ),
        child: InkWell(
          key: Key('nearby-radius-${_radiusKey(radiusKm)}'),
          customBorder: const StadiumBorder(),
          onTap: onTap,
          child: Container(
            constraints: const BoxConstraints(minHeight: 44, minWidth: 52),
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
            alignment: Alignment.center,
            child: Text(
              label,
              style: theme.textTheme.labelLarge?.copyWith(
                fontWeight: FontWeight.w700,
                color: selected
                    ? theme.colorScheme.onPrimary
                    : theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _NearbyCard extends StatelessWidget {
  const _NearbyCard({
    required this.item,
    required this.api,
    required this.onOpenDrink,
    required this.onOpenAuthor,
  });

  final NearbyItem item;
  final ApiClient api;
  final VoidCallback onOpenDrink;
  final VoidCallback onOpenAuthor;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final checkin = item.checkin;
    final quarters = (checkin.rating ?? 0).round();
    final photoId = checkin.photoId;
    final showPhoto = photoId.isNotEmpty && api.configured;
    final time = nearbyRelativeTime(checkin.createdAt);

    return Card(
      clipBehavior: Clip.antiAlias,
      child: Padding(
        padding: const EdgeInsets.all(14),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (showPhoto) ...[
              ClipRRect(
                borderRadius: BorderRadius.circular(12),
                child: Image.network(
                  api.mediaUrl(photoId),
                  headers: api.imageHeaders,
                  width: 64,
                  height: 64,
                  fit: BoxFit.cover,
                  semanticLabel: 'Photo of ${checkin.drinkName}',
                  errorBuilder: (context, error, stackTrace) => Container(
                    width: 64,
                    height: 64,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
                    alignment: Alignment.center,
                    child: Icon(
                      Icons.image_not_supported_outlined,
                      size: 20,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
              ),
              const SizedBox(width: 12),
            ],
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Flexible(
                        child: _DistancePill(
                          label: nearbyDistanceLabel(item.distanceM),
                        ),
                      ),
                      const SizedBox(width: 8),
                      if (time.isNotEmpty)
                        Text(
                          time,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  InkWell(
                    onTap: checkin.drinkSlug.isEmpty ? null : onOpenDrink,
                    borderRadius: BorderRadius.circular(8),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(vertical: 1),
                      child: Text(
                        checkin.drinkName,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleMedium?.copyWith(
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 2),
                  InkWell(
                    onTap: checkin.handle.isEmpty ? null : onOpenAuthor,
                    borderRadius: BorderRadius.circular(8),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(vertical: 2),
                      child: Text(
                        '@${checkin.handle}',
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                          fontWeight: FontWeight.w700,
                          decoration: TextDecoration.underline,
                        ),
                      ),
                    ),
                  ),
                  if (checkin.venueName.isNotEmpty) ...[
                    const SizedBox(height: 4),
                    Row(
                      children: [
                        Icon(
                          Icons.place_outlined,
                          size: 14,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                        const SizedBox(width: 4),
                        Expanded(
                          child: Text(
                            checkin.venueName,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ],
                  const SizedBox(height: 8),
                  if (quarters > 0)
                    Row(
                      children: [
                        SodaRating(quarters: quarters, size: 16),
                        const SizedBox(width: 6),
                        Text(
                          ratingLabel(quarters),
                          style: theme.textTheme.labelLarge?.copyWith(
                            color: Soda.tangerine,
                            fontWeight: FontWeight.w800,
                          ),
                        ),
                      ],
                    )
                  else
                    Text(
                      'Not rated',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  if (checkin.note.isNotEmpty) ...[
                    const SizedBox(height: 8),
                    Text(
                      checkin.note,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodyMedium,
                    ),
                  ],
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 14,
                    runSpacing: 4,
                    children: [
                      _Count(
                        icon: Icons.celebration_outlined,
                        label: sodaCount(checkin.toasts, 'toast'),
                      ),
                      _Count(
                        icon: Icons.chat_bubble_outline_rounded,
                        label: sodaCount(checkin.comments, 'comment'),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DistancePill extends StatelessWidget {
  const _DistancePill({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
      decoration: BoxDecoration(
        color: theme.colorScheme.primary.withValues(alpha: 0.22),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: theme.textTheme.labelSmall?.copyWith(
          fontWeight: FontWeight.w700,
          color: theme.colorScheme.onSurface,
        ),
      ),
    );
  }
}

class _Count extends StatelessWidget {
  const _Count({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Icon(icon, size: 14, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 4),
        Text(
          label,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }
}

String _radiusLabel(double radiusKm) {
  final text = radiusKm == radiusKm.roundToDouble()
      ? '${radiusKm.toInt()}'
      : radiusKm.toStringAsFixed(1);
  return '$text km';
}

String _radiusKey(double radiusKm) => radiusKm == radiusKm.roundToDouble()
    ? '${radiusKm.toInt()}'
    : radiusKm.toStringAsFixed(1).replaceAll('.', '_');
