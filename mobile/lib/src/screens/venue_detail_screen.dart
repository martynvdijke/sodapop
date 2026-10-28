import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

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
  bool _uploading = false;
  String _uploadKind = 'photo';
  String? _removingId;
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
          if (_canContribute) ...[_mediaActions(), const SizedBox(height: 14)],
          _gallery(),
        ],
      ),
    );
  }

  /// Signed-in members hold a bearer token; the token also decorates image
  /// requests, so this is the same signal the gallery uses.
  bool get _canContribute {
    final api = ref.read(apiProvider);
    return api.configured && api.imageHeaders.isNotEmpty;
  }

  Widget _mediaActions() {
    final theme = Theme.of(context);
    final photoBusy = _uploading && _uploadKind == 'photo';
    final menuBusy = _uploading && _uploadKind == 'menu';
    final style = OutlinedButton.styleFrom(
      minimumSize: const Size(44, 48),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
      side: BorderSide(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.2),
      ),
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(
              child: OutlinedButton.icon(
                onPressed: _uploading ? null : () => _addMedia('photo'),
                style: style,
                icon: const Icon(Icons.add_a_photo_outlined, size: 18),
                label: Text(photoBusy ? 'Adding…' : 'Add photo'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: OutlinedButton.icon(
                onPressed: _uploading ? null : () => _addMedia('menu'),
                style: style,
                icon: const Icon(Icons.menu_book_outlined, size: 18),
                label: Text(menuBusy ? 'Adding…' : 'Add menu'),
              ),
            ),
          ],
        ),
        if (_uploading) ...[
          const SizedBox(height: 10),
          ClipRRect(
            borderRadius: BorderRadius.circular(999),
            child: LinearProgressIndicator(
              minHeight: 4,
              color: Soda.lime,
              backgroundColor: theme.colorScheme.onSurface.withValues(
                alpha: 0.08,
              ),
            ),
          ),
        ],
      ],
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
      return SodaStatusView(
        dense: true,
        icon: Icons.photo_library_outlined,
        title: 'No photos or menus yet',
        message: _canContribute
            ? 'Add a photo of the shelf or the menu so the next visitor knows what to look for.'
            : 'Photos and menus added by visitors show up here.',
      );
    }
    return LayoutBuilder(
      builder: (context, constraints) {
        const gap = 12.0;
        final columns = constraints.maxWidth >= 560 ? 3 : 2;
        final width = (constraints.maxWidth - gap * (columns - 1)) / columns;
        return Wrap(
          spacing: gap,
          runSpacing: gap,
          children: [
            for (final media in _media)
              SizedBox(width: width, child: _mediaCard(media)),
          ],
        );
      },
    );
  }

  static String _kindLabel(String kind) => kind == 'menu' ? 'Menu' : 'Photo';

  Widget _mediaCard(VenueMedia media) {
    final theme = Theme.of(context);
    final api = ref.read(apiProvider);
    final venueName = _detail?.venue.name ?? 'this venue';
    final kindLabel = _kindLabel(media.kind);
    final label = media.label.trim();
    final meta = [
      if (media.uploaderHandle.isNotEmpty) '@${media.uploaderHandle}',
      if (media.createdAt.isNotEmpty) sodaDate(media.createdAt),
    ].join(' · ');
    final description =
        '$kindLabel${label.isEmpty ? '' : ': $label'} at $venueName';

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: api.configured ? () => _openMedia(media) : null,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            AspectRatio(
              aspectRatio: 1,
              child: Stack(
                fit: StackFit.expand,
                children: [
                  if (api.configured)
                    Image.network(
                      '${api.mediaUrl(media.mediaId)}/thumb',
                      headers: api.imageHeaders,
                      fit: BoxFit.cover,
                      semanticLabel: description,
                      errorBuilder: (context, error, stackTrace) =>
                          const _MediaPlaceholder(),
                    )
                  else
                    const _MediaPlaceholder(),
                  Positioned(
                    top: 8,
                    left: 8,
                    child: Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 3,
                      ),
                      decoration: BoxDecoration(
                        color: theme.colorScheme.surface.withValues(
                          alpha: 0.92,
                        ),
                        borderRadius: BorderRadius.circular(999),
                      ),
                      child: Text(
                        kindLabel,
                        style: theme.textTheme.labelSmall?.copyWith(
                          fontWeight: FontWeight.w700,
                          color: theme.colorScheme.onSurface,
                        ),
                      ),
                    ),
                  ),
                  if (media.canRemove)
                    Positioned(
                      top: 4,
                      right: 4,
                      child: _removeButton(media, kindLabel),
                    ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(10, 8, 10, 10),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (label.isNotEmpty)
                    Text(
                      label,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  if (meta.isNotEmpty) ...[
                    if (label.isNotEmpty) const SizedBox(height: 2),
                    Text(
                      meta,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
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

  Widget _removeButton(VenueMedia media, String kindLabel) {
    final removing = _removingId == media.id;
    return IconButton(
      onPressed: removing ? null : () => _removeMedia(media),
      tooltip: 'Remove ${kindLabel.toLowerCase()}',
      iconSize: 20,
      padding: EdgeInsets.zero,
      constraints: const BoxConstraints.tightFor(width: 44, height: 44),
      style: IconButton.styleFrom(
        backgroundColor: Colors.black.withValues(alpha: 0.5),
        foregroundColor: Colors.white,
        disabledBackgroundColor: Colors.black.withValues(alpha: 0.5),
        disabledForegroundColor: Colors.white,
      ),
      icon: removing
          ? const SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: Colors.white,
              ),
            )
          : const Icon(Icons.delete_outline_rounded),
    );
  }

  void _openMedia(VenueMedia media) {
    final api = ref.read(apiProvider);
    final venueName = _detail?.venue.name ?? 'this venue';
    final kindLabel = _kindLabel(media.kind);
    final label = media.label.trim();
    showDialog<void>(
      context: context,
      barrierColor: Colors.black.withValues(alpha: 0.94),
      builder: (context) => Dialog.fullscreen(
        backgroundColor: Colors.transparent,
        child: Stack(
          children: [
            Positioned.fill(
              child: InteractiveViewer(
                maxScale: 4,
                child: Center(
                  child: Image.network(
                    api.mediaUrl(media.mediaId),
                    headers: api.imageHeaders,
                    fit: BoxFit.contain,
                    semanticLabel:
                        '$kindLabel${label.isEmpty ? '' : ': $label'} at $venueName',
                    errorBuilder: (context, error, stackTrace) =>
                        const _MediaPlaceholder(),
                  ),
                ),
              ),
            ),
            SafeArea(
              child: Align(
                alignment: Alignment.topRight,
                child: Padding(
                  padding: const EdgeInsets.all(8),
                  child: IconButton.filledTonal(
                    onPressed: () => Navigator.of(context).pop(),
                    tooltip: 'Close',
                    icon: const Icon(Icons.close_rounded),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _addMedia(String kind) async {
    if (_uploading) return;
    var source = ImageSource.gallery;
    if (kind == 'photo') {
      final chosen = await _pickSource();
      if (chosen == null || !mounted) return;
      source = chosen;
    }

    XFile? picked;
    try {
      picked = await ImagePicker().pickImage(
        source: source,
        maxWidth: 2400,
        imageQuality: 88,
      );
    } catch (_) {
      if (!mounted) return;
      _showSnack(
        source == ImageSource.camera
            ? 'Could not open the camera.'
            : 'Could not open the photo picker.',
      );
      return;
    }
    if (picked == null || !mounted) return;

    final file = File(picked.path);
    final problem = _fileProblem(file);
    if (problem != null) {
      _showSnack(problem);
      return;
    }

    final label = await _askLabel(kind);
    if (!mounted || label == null) return;

    setState(() {
      _uploading = true;
      _uploadKind = kind;
    });
    try {
      await ref
          .read(apiProvider)
          .uploadVenueMedia(
            widget.venueId,
            file: file,
            kind: kind,
            label: label,
          );
      await _loadMedia();
      if (!mounted) return;
      _showSnack(kind == 'menu' ? 'Menu added.' : 'Photo added.');
    } catch (error) {
      if (!mounted) return;
      _showSnack(_uploadProblem(error));
    } finally {
      if (mounted) setState(() => _uploading = false);
    }
  }

  Future<void> _removeMedia(VenueMedia media) async {
    if (_removingId != null) return;
    final kindLabel = _kindLabel(media.kind);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Remove this ${kindLabel.toLowerCase()}?'),
        content: const Text('It will no longer appear at this venue.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _removingId = media.id);
    try {
      await ref.read(apiProvider).deleteVenueMedia(media.id);
      await _loadMedia();
      if (!mounted) return;
      _showSnack('$kindLabel removed.');
    } catch (error) {
      if (!mounted) return;
      _showSnack(ApiClient.describe(error).message);
    } finally {
      if (mounted) setState(() => _removingId = null);
    }
  }

  Future<ImageSource?> _pickSource() {
    return showModalBottomSheet<ImageSource>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.photo_camera_outlined),
              title: const Text('Take a photo'),
              onTap: () => Navigator.of(context).pop(ImageSource.camera),
            ),
            ListTile(
              leading: const Icon(Icons.photo_library_outlined),
              title: const Text('Choose from gallery'),
              onTap: () => Navigator.of(context).pop(ImageSource.gallery),
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }

  Future<String?> _askLabel(String kind) {
    return showDialog<String>(
      context: context,
      builder: (_) => _LabelDialog(kind: kind),
    );
  }

  /// Cheap local checks before spending a slow upload on a file the server
  /// would reject anyway. The server re-validates everything.
  static String? _fileProblem(File file) {
    const allowed = {'jpg', 'jpeg', 'png', 'gif', 'webp'};
    try {
      if (!file.existsSync()) {
        return 'That file could not be read. Choose another image.';
      }
      final size = file.lengthSync();
      if (size == 0) {
        return 'That file is empty. Choose another image.';
      }
      if (size > 25 * 1024 * 1024) {
        return 'That image is over 25 MB. Choose a smaller file.';
      }
    } on FileSystemException {
      return 'That file could not be read. Choose another image.';
    }
    final name = file.path.toLowerCase();
    final dot = name.lastIndexOf('.');
    final extension = dot == -1 ? '' : name.substring(dot + 1);
    if (!allowed.contains(extension)) {
      return 'Use a JPG, PNG, GIF or WebP image.';
    }
    return null;
  }

  static String _uploadProblem(Object error) {
    final described = ApiClient.describe(error);
    switch (described.statusCode) {
      case 413:
        return 'That image is too large. Choose one under 25 MB.';
      case 429:
        return 'Too many uploads for now. Try again in a little while.';
      case 409:
        return 'This venue is at its photo and menu limit.';
    }
    return described.message;
  }

  void _showSnack(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }
}

class _LabelDialog extends StatefulWidget {
  const _LabelDialog({required this.kind});

  final String kind;

  @override
  State<_LabelDialog> createState() => _LabelDialogState();
}

class _LabelDialogState extends State<_LabelDialog> {
  final _label = TextEditingController();

  @override
  void dispose() {
    _label.dispose();
    super.dispose();
  }

  void _submit() => Navigator.of(context).pop(_label.text.trim());

  @override
  Widget build(BuildContext context) {
    final menu = widget.kind == 'menu';
    return AlertDialog(
      title: Text(menu ? 'Add a menu' : 'Add a photo'),
      content: TextField(
        controller: _label,
        autofocus: true,
        maxLength: 80,
        textCapitalization: TextCapitalization.sentences,
        textInputAction: TextInputAction.done,
        decoration: InputDecoration(
          labelText: 'Label (optional)',
          hintText: menu ? 'Sunday drinks menu' : 'Fridge shelf',
        ),
        onSubmitted: (_) => _submit(),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(onPressed: _submit, child: const Text('Upload')),
      ],
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
