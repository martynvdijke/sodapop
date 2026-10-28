import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../offline/read_cache.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'checkin_sheet.dart';
import 'compare_screen.dart';

/// One soda's full record: facts, flavour notes and the community gallery.
class DrinkDetailScreen extends ConsumerStatefulWidget {
  const DrinkDetailScreen({
    super.key,
    required this.slug,
    this.name = '',
    this.compareOpener,
  });

  final String slug;
  final String name;

  /// Optional test override that receives the slug to preselect. Production
  /// opens [CompareScreen] with this soda already in the line-up.
  final ValueChanged<String>? compareOpener;

  static Future<void> open(
    BuildContext context,
    String slug, {
    String name = '',
  }) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => DrinkDetailScreen(slug: slug, name: name),
      ),
    );
  }

  @override
  ConsumerState<DrinkDetailScreen> createState() => _DrinkDetailScreenState();
}

class _DrinkDetailScreenState extends ConsumerState<DrinkDetailScreen> {
  Drink? _drink;
  List<DrinkImage> _images = const [];
  bool _loading = true;
  bool _notFound = false;
  bool _galleryLoading = true;
  bool _uploading = false;
  bool _moderating = false;
  bool _offline = false;
  String? _error;
  String? _galleryError;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _notFound = false;
      _error = null;
      _galleryLoading = true;
      _galleryError = null;
    });
    final api = ref.read(apiProvider);

    // Both requests start together; a gallery failure must not hide the soda.
    final drinkFuture = api.drink(widget.slug);
    final imagesFuture = api.drinkImages(widget.slug);

    Drink? drink;
    String? drinkError;
    int? status;
    try {
      drink = await drinkFuture;
    } catch (error) {
      final described = ApiClient.describe(error);
      drinkError = described.message;
      status = described.statusCode;
      if (isOfflineError(error)) {
        final cached = await ReadCache().loadDrink(widget.slug);
        if (!mounted) return;
        if (cached != null) {
          imagesFuture.ignore();
          setState(() {
            _loading = false;
            _galleryLoading = false;
            _offline = true;
            _error = null;
            _drink = cached.drink;
            _images = cached.images;
            _galleryError = null;
          });
          return;
        }
      }
    }

    var images = const <DrinkImage>[];
    String? imagesError;
    try {
      images = await imagesFuture;
    } catch (error) {
      imagesError = ApiClient.describe(error).message;
    }

    if (!mounted) return;
    setState(() {
      _loading = false;
      _galleryLoading = false;
      _offline = false;
      if (drink == null) {
        _error = drinkError;
        _notFound = status == 404;
        return;
      }
      _drink = drink;
      _images = images;
      _galleryError = imagesError;
    });
    if (drink != null && imagesError == null) {
      // Caching is best effort and must never hold up the first paint.
      unawaited(ReadCache().saveDrink(drink, images));
    }
  }

  Future<void> _loadGallery() async {
    setState(() {
      _galleryLoading = true;
      _galleryError = null;
    });
    try {
      final images = await ref.read(apiProvider).drinkImages(widget.slug);
      if (!mounted) return;
      setState(() {
        _images = images;
        _galleryLoading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _galleryLoading = false;
        _galleryError = ApiClient.describe(error).message;
      });
    }
  }

  bool _needsConnection() {
    if (!_offline) return false;
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('Needs a connection')),
    );
    return true;
  }

  Future<void> _logThisSoda(Drink drink) async {
    if (_needsConnection()) return;
    final logged = await showCheckinSheet(context, drink);
    if (!mounted || !logged) return;
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('Check-in saved.')));
  }

  Future<void> _addPhoto() async {
    if (_uploading || _needsConnection()) return;
    XFile? picked;
    try {
      picked = await ImagePicker().pickImage(
        source: ImageSource.gallery,
        maxWidth: 2000,
        imageQuality: 88,
      );
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Could not open the photo picker.')),
      );
      return;
    }
    if (picked == null || !mounted) return;

    final credit = await showDialog<String>(
      context: context,
      builder: (_) => const _AttributionDialog(),
    );
    if (credit == null || !mounted) return;

    setState(() => _uploading = true);
    try {
      await ref
          .read(apiProvider)
          .uploadDrinkImage(
            widget.slug,
            file: File(picked.path),
            attribution: credit,
          );
      if (!mounted) return;
      setState(() => _uploading = false);
      await _loadGallery();
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Photo added.')));
    } catch (error) {
      if (!mounted) return;
      setState(() => _uploading = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _makePrimary(DrinkImage image) async {
    if (_moderating || image.isPrimary || _needsConnection()) return;
    setState(() => _moderating = true);
    try {
      await ref.read(apiProvider).setDrinkImagePrimary(image.id);
      if (!mounted) return;
      setState(() => _moderating = false);
      await _loadGallery();
    } catch (error) {
      if (!mounted) return;
      setState(() => _moderating = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _removeImage(DrinkImage image) async {
    if (_moderating || _needsConnection()) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Remove this photo?'),
        content: const Text(
          'It disappears from this gallery for everyone. This cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _moderating = true);
    try {
      await ref.read(apiProvider).deleteDrinkImage(image.id);
      if (!mounted) return;
      setState(() => _moderating = false);
      await _loadGallery();
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Photo removed.')));
    } catch (error) {
      if (!mounted) return;
      setState(() => _moderating = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  void _openCompare() {
    final drink = _drink;
    final slug = drink != null && drink.slug.isNotEmpty
        ? drink.slug
        : widget.slug;
    if (slug.isEmpty) return;
    final opener = widget.compareOpener;
    if (opener != null) {
      opener(slug);
      return;
    }
    CompareScreen.open(context, slugs: [slug]);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Soda'),
        actions: [
          if (_drink != null)
            IconButton(
              key: const Key('drink-compare'),
              onPressed: _openCompare,
              tooltip: 'Compare sodas',
              icon: const Icon(Icons.compare_arrows_rounded),
            ),
          if (_moderating)
            const Padding(
              padding: EdgeInsets.all(16),
              child: SizedBox(
                width: 20,
                height: 20,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: Soda.lime,
                ),
              ),
            ),
        ],
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const SodaLoadingView(label: 'Loading soda…');
    if (_notFound) {
      return SodaStatusView(
        icon: Icons.local_drink_outlined,
        title: 'Soda unavailable',
        message: 'This soda is no longer in the catalog.',
        actionLabel: 'Back',
        onAction: () => Navigator.of(context).maybePop(),
      );
    }
    final drink = _drink;
    if (_error != null || drink == null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load this soda',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }

    final theme = Theme.of(context);
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          if (_offline) ...[
            const _SavedBanner(
              message:
                  'Showing saved soda. Pull to refresh when you are online.',
            ),
            const SizedBox(height: 12),
          ],
          _hero(drink),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: () => _logThisSoda(drink),
            icon: const Icon(Icons.add_rounded, size: 20),
            label: const Text('Log this soda'),
          ),
          if (drink.description.isNotEmpty) ...[
            const SizedBox(height: 20),
            Text(
              drink.description,
              style: theme.textTheme.bodyLarge?.copyWith(height: 1.45),
            ),
          ],
          const SizedBox(height: 26),
          _facts(drink),
          if (drink.flavours.isNotEmpty) ...[
            const SizedBox(height: 26),
            _flavours(drink),
          ],
          if (drink.aliases.isNotEmpty) ...[
            const SizedBox(height: 22),
            SodaSectionTitle(title: 'Also known as'),
            const SizedBox(height: 6),
            Text(
              drink.aliases.join(', '),
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          const SizedBox(height: 26),
          _gallerySection(),
        ],
      ),
    );
  }

  Widget _hero(Drink drink) {
    final theme = Theme.of(context);
    final meta = [
      drink.style,
      _containerLabel(drink.container),
      if (drink.volumeMl > 0) '${drink.volumeMl} ml',
    ].where((value) => value.isNotEmpty).join(' · ');
    final status = _statusLabel(drink.status);

    return Container(
      padding: const EdgeInsets.fromLTRB(20, 22, 20, 20),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(20),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            theme.colorScheme.primary.withValues(alpha: 0.32),
            theme.colorScheme.primary.withValues(alpha: 0.04),
          ],
        ),
        border: Border.all(
          color: theme.colorScheme.primary.withValues(alpha: 0.28),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (drink.brand.isNotEmpty)
            Text(
              drink.brand.toUpperCase(),
              style: theme.textTheme.labelSmall?.copyWith(
                fontWeight: FontWeight.w800,
                letterSpacing: 1.6,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          const SizedBox(height: 8),
          Text(
            drink.name,
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w800,
              letterSpacing: -0.4,
            ),
          ),
          if (meta.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              meta,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          if (status.isNotEmpty) ...[
            const SizedBox(height: 12),
            SodaPill(icon: _statusIcon(drink.status), label: status),
          ],
          const SizedBox(height: 18),
          _rating(drink),
        ],
      ),
    );
  }

  Widget _rating(Drink drink) {
    final theme = Theme.of(context);
    if (!drink.ratingShown || drink.rating == null) {
      return Row(
        children: [
          Icon(
            Icons.star_outline_rounded,
            size: 18,
            color: theme.colorScheme.onSurfaceVariant,
          ),
          const SizedBox(width: 6),
          Expanded(
            child: Text(
              'Not enough ratings yet',
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      );
    }
    final quarters = (drink.rating! * 4).round();
    return Row(
      children: [
        SodaRating(quarters: quarters, size: 22),
        const SizedBox(width: 10),
        Text(
          ratingLabel(quarters),
          style: theme.textTheme.titleMedium?.copyWith(
            color: Soda.tangerine,
            fontWeight: FontWeight.w800,
            fontFeatures: const [FontFeature.tabularFigures()],
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            sodaCount(drink.raterCount, 'rating'),
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ),
      ],
    );
  }

  Widget _facts(Drink drink) {
    final theme = Theme.of(context);
    final caffeine = drink.caffeineMgPer100ml > 0
        ? '${drink.caffeineMgPer100ml}'
        : null;
    final sugar = drink.sugarGPer100ml > 0
        ? _decimal(drink.sugarGPer100ml)
        : null;
    final calories = drink.caloriesKcalPer100ml > 0
        ? '${drink.caloriesKcalPer100ml}'
        : null;
    final missing = caffeine == null || sugar == null || calories == null;
    final details = [
      ('Sweetener', drink.sweetener),
      ('Carbonation', drink.carbonation),
      ('Acid', drink.acid),
    ].where((row) => row.$2.isNotEmpty).toList();
    final dividerColor = theme.colorScheme.onSurface.withValues(alpha: 0.1);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'Per 100 ml'),
        const SizedBox(height: 8),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    _fact('Caffeine', caffeine, 'mg'),
                    _rule(dividerColor),
                    _fact('Sugar', sugar, 'g'),
                    _rule(dividerColor),
                    _fact('Calories', calories, 'kcal'),
                  ],
                ),
                if (missing) ...[
                  const SizedBox(height: 12),
                  Text(
                    'A dash means the label did not list a value.',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
                if (details.isNotEmpty) ...[
                  const SizedBox(height: 14),
                  Divider(color: dividerColor, height: 1),
                  for (final row in details)
                    Padding(
                      padding: const EdgeInsets.only(top: 10),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(
                            child: Text(
                              row.$1,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.onSurfaceVariant,
                              ),
                            ),
                          ),
                          const SizedBox(width: 12),
                          Text(
                            row.$2,
                            style: theme.textTheme.bodyMedium?.copyWith(
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                        ],
                      ),
                    ),
                ],
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _fact(String label, String? value, String unit) {
    final theme = Theme.of(context);
    return Expanded(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text.rich(
            TextSpan(
              text: value ?? '—',
              style: theme.textTheme.titleLarge?.copyWith(
                fontWeight: FontWeight.w800,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
              children: value == null
                  ? const []
                  : [
                      TextSpan(
                        text: ' $unit',
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ],
            ),
          ),
          const SizedBox(height: 2),
          Text(
            label,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }

  Widget _rule(Color color) => Container(
    width: 1,
    height: 44,
    margin: const EdgeInsets.symmetric(horizontal: 12),
    color: color,
  );

  Widget _flavours(Drink drink) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        SodaSectionTitle(
          title: 'Flavours',
          trailing: '${drink.flavours.length}',
        ),
        const SizedBox(height: 10),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            for (final flavour in drink.flavours)
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 12,
                  vertical: 8,
                ),
                decoration: BoxDecoration(
                  color: theme.colorScheme.primary.withValues(alpha: 0.16),
                  borderRadius: BorderRadius.circular(999),
                  border: Border.all(
                    color: theme.colorScheme.primary.withValues(alpha: 0.4),
                  ),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (flavour.name.isNotEmpty)
                      Text(
                        flavour.name,
                        style: theme.textTheme.labelLarge?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    if (flavour.name.isNotEmpty && flavour.intensity > 0)
                      const SizedBox(width: 6),
                    if (flavour.intensity > 0)
                      Text(
                        '${flavour.intensity}/5',
                        style: theme.textTheme.labelSmall?.copyWith(
                          fontWeight: FontWeight.w600,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                  ],
                ),
              ),
          ],
        ),
      ],
    );
  }

  Widget _gallerySection() {
    final theme = Theme.of(context);
    final signedIn = ref.watch(authProvider).signedIn;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SodaSectionTitle(
          title: 'Photos',
          trailing: _images.isEmpty || _galleryLoading
              ? null
              : '${_images.length}',
        ),
        const SizedBox(height: 4),
        Text(
          'Photos of this soda, added by people who have logged it.',
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 12),
        if (_galleryLoading)
          const SodaLoadingView(dense: true)
        else if (_galleryError != null)
          SodaStatusView(
            dense: true,
            icon: Icons.image_not_supported_outlined,
            title: 'Photos did not load',
            message: _galleryError,
            actionLabel: 'Try again',
            onAction: _loadGallery,
          )
        else if (_images.isEmpty)
          SodaStatusView(
            dense: true,
            icon: Icons.photo_library_outlined,
            title: 'No community photos yet',
            message: signedIn
                ? 'If you have a photo of this soda, add the first one.'
                : 'Photos are added by members who are signed in.',
          )
        else
          _photoGrid(),
        if (signedIn) ...[
          const SizedBox(height: 14),
          OutlinedButton.icon(
            onPressed: _uploading ? null : _addPhoto,
            icon: _uploading
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: Soda.lime,
                    ),
                  )
                : const Icon(Icons.add_photo_alternate_outlined, size: 18),
            label: Text(_uploading ? 'Uploading…' : 'Add a photo'),
          ),
        ],
      ],
    );
  }

  Widget _photoGrid() {
    return LayoutBuilder(
      builder: (context, constraints) {
        const gap = 12.0;
        final cell = (constraints.maxWidth - gap) / 2;
        return GridView.builder(
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          padding: EdgeInsets.zero,
          gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: 2,
            mainAxisSpacing: gap,
            crossAxisSpacing: gap,
            mainAxisExtent: cell + 64,
          ),
          itemCount: _images.length,
          itemBuilder: (context, index) => _photoTile(_images[index]),
        );
      },
    );
  }

  Widget _photoTile(DrinkImage image) {
    final theme = Theme.of(context);
    final api = ref.read(apiProvider);
    final name = _drink?.name ?? widget.name;
    final attribution = image.attribution.isEmpty
        ? 'Added by a member'
        : image.attribution;
    final semanticLabel = [
      image.isPrimary ? 'Primary photo' : 'Photo',
      if (name.isNotEmpty) 'of $name',
      if (image.attribution.isNotEmpty) image.attribution,
    ].join(' ');

    return Card(
      key: ValueKey('drink-photo-${image.id}'),
      clipBehavior: Clip.antiAlias,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Expanded(
            child: Stack(
              fit: StackFit.expand,
              children: [
                if (api.configured && image.mediaId.isNotEmpty)
                  Image.network(
                    api.mediaUrl(image.mediaId),
                    headers: api.imageHeaders,
                    fit: BoxFit.cover,
                    semanticLabel: semanticLabel,
                    errorBuilder: (context, error, stackTrace) =>
                        const _PhotoPlaceholder(),
                  )
                else
                  const _PhotoPlaceholder(),
                if (image.isPrimary)
                  Positioned(
                    left: 8,
                    top: 8,
                    child: Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 4,
                      ),
                      decoration: BoxDecoration(
                        color: Soda.forest.withValues(alpha: 0.86),
                        borderRadius: BorderRadius.circular(999),
                      ),
                      child: const Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(Icons.check_rounded, size: 12, color: Soda.lime),
                          SizedBox(width: 4),
                          Text(
                            'Primary',
                            style: TextStyle(
                              color: Soda.cream,
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                if (image.canModerate)
                  Positioned(
                    right: 4,
                    top: 4,
                    child: Material(
                      color: theme.colorScheme.surface.withValues(alpha: 0.9),
                      shape: const CircleBorder(),
                      clipBehavior: Clip.antiAlias,
                      child: PopupMenuButton<String>(
                        tooltip: 'Photo options',
                        padding: EdgeInsets.zero,
                        iconSize: 20,
                        icon: const Icon(Icons.more_horiz_rounded),
                        onSelected: (value) {
                          if (value == 'primary') _makePrimary(image);
                          if (value == 'remove') _removeImage(image);
                        },
                        itemBuilder: (context) => [
                          if (!image.isPrimary)
                            const PopupMenuItem(
                              value: 'primary',
                              child: Text('Make primary'),
                            ),
                          const PopupMenuItem(
                            value: 'remove',
                            child: Text('Remove photo'),
                          ),
                        ],
                      ),
                    ),
                  ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(10, 8, 10, 9),
            child: Text(
              attribution,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Optional credit for a photo before it uploads. The controller lives here so
/// it is disposed with the dialog, after the exit animation.
class _AttributionDialog extends StatefulWidget {
  const _AttributionDialog();

  @override
  State<_AttributionDialog> createState() => _AttributionDialogState();
}

class _AttributionDialogState extends State<_AttributionDialog> {
  final _credit = TextEditingController();

  @override
  void dispose() {
    _credit.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Add a photo'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            'Credit is optional. Say who took the photo, or where it came from.',
            style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 14),
          TextField(
            controller: _credit,
            autofocus: true,
            maxLength: 200,
            textCapitalization: TextCapitalization.sentences,
            inputFormatters: [LengthLimitingTextInputFormatter(200)],
            decoration: const InputDecoration(hintText: 'Credit (optional)'),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(context, _credit.text.trim()),
          child: const Text('Upload'),
        ),
      ],
    );
  }
}

/// Banner shown when the detail page is rendering cached data.
class _SavedBanner extends StatelessWidget {
  const _SavedBanner({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer.withValues(alpha: 0.7),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        children: [
          Icon(
            Icons.cloud_off_rounded,
            size: 18,
            color: theme.colorScheme.onSecondaryContainer,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSecondaryContainer,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _PhotoPlaceholder extends StatelessWidget {
  const _PhotoPlaceholder();

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

String _decimal(double value) {
  final text = value.toStringAsFixed(1);
  return text.endsWith('.0') ? text.substring(0, text.length - 2) : text;
}

String _containerLabel(String container) {
  switch (container) {
    case 'can':
      return 'Can';
    case 'glass':
      return 'Glass bottle';
    case 'pet':
      return 'Plastic bottle';
    case 'fountain':
      return 'Fountain';
    case 'carton':
      return 'Carton';
    case 'other':
      return 'Other packaging';
    default:
      return container;
  }
}

String _statusLabel(String status) {
  switch (status) {
    case 'limited':
      return 'Limited run';
    case 'seasonal':
      return 'Seasonal';
    case 'discontinued':
      return 'Discontinued';
    default:
      return '';
  }
}

IconData _statusIcon(String status) {
  switch (status) {
    case 'limited':
      return Icons.schedule_rounded;
    case 'seasonal':
      return Icons.wb_sunny_outlined;
    case 'discontinued':
      return Icons.block_rounded;
    default:
      return Icons.info_outline_rounded;
  }
}
