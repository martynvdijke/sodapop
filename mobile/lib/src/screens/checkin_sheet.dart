import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../offline/checkin_outbox.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

/// Largest video this sheet will upload. The server accepts more, but reading
/// a huge clip into memory just to lose it on a flaky connection is wasteful.
const maxVideoAttachmentBytes = 20 * 1024 * 1024;

/// Test seam: the sheet never talks to the camera directly when a picker is
/// supplied. Production code leaves these null and uses [ImagePicker].
typedef CheckinImagePicker = Future<XFile?> Function(ImageSource source);
typedef CheckinVideoPicker = Future<XFile?> Function();

Future<bool> showCheckinSheet(BuildContext context, Drink drink) async {
  final result = await showModalBottomSheet<bool>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => CheckinSheet(drink: drink),
  );
  return result ?? false;
}

class CheckinSheet extends ConsumerStatefulWidget {
  const CheckinSheet({
    super.key,
    required this.drink,
    this.pickImage,
    this.pickVideo,
  });

  final Drink drink;

  /// Optional overrides so widget tests can attach media without a camera.
  final CheckinImagePicker? pickImage;
  final CheckinVideoPicker? pickVideo;

  @override
  ConsumerState<CheckinSheet> createState() => _CheckinSheetState();
}

class _CheckinSheetState extends ConsumerState<CheckinSheet> {
  static const _servings = <(String, String)>[
    ('can', 'Can'),
    ('bottle', 'Bottle'),
    ('glass', 'Glass'),
    ('fountain', 'Fountain'),
  ];

  static const _visibilities = <(String, String)>[
    ('private', 'Only you'),
    ('friends', 'Friends'),
    ('public', 'Anyone'),
  ];

  final _note = TextEditingController();
  int _quarters = 0;
  bool _notRated = true;
  bool _saving = false;
  bool _picking = false;
  String _serving = 'can';
  String _visibility = 'private';
  Venue? _venue;
  String? _error;

  Uint8List? _photoBytes;
  String? _photoName;
  String? _photoMime;

  Uint8List? _videoBytes;
  String? _videoName;
  String? _videoMime;

  /// One key per sheet, reused if a direct write fails and the row is queued.
  String? _key;

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  String get _ratingText => _notRated ? 'Not rated' : ratingLabel(_quarters);

  String get _visibilityHint {
    switch (_visibility) {
      case 'friends':
        return 'Your friends see this in their feed.';
      case 'public':
        return 'Anyone can see this on the drink page.';
      default:
        return 'Only you can see this check-in.';
    }
  }

  bool get _hasVideo => _videoBytes != null;

  void _removePhoto() {
    setState(() {
      _photoBytes = null;
      _photoName = null;
      _photoMime = null;
    });
  }

  void _removeVideo() {
    setState(() {
      _videoBytes = null;
      _videoName = null;
      _videoMime = null;
    });
  }

  Future<void> _pickPhoto() async {
    if (_picking || _saving) return;
    final source = await showModalBottomSheet<ImageSource>(
      context: context,
      useSafeArea: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: _topRounded,
      builder: (_) => const _PhotoSourceSheet(),
    );
    if (source == null || !mounted) return;

    setState(() {
      _picking = true;
      _error = null;
    });
    try {
      final XFile? picked;
      final picker = widget.pickImage;
      if (picker != null) {
        picked = await picker(source);
      } else {
        picked = await ImagePicker().pickImage(
          source: source,
          maxWidth: 1600,
          imageQuality: 85,
        );
      }
      if (picked == null) return;
      final file = picked;
      final bytes = await file.readAsBytes();
      if (!mounted) return;
      if (bytes.isEmpty) {
        setState(() => _error = 'That photo came back empty. Try another one.');
        return;
      }
      setState(() {
        _videoBytes = null;
        _videoName = null;
        _videoMime = null;
        _photoBytes = bytes;
        _photoName = _nameOr(file.name, 'photo.jpg');
        _photoMime = _mimeFor(file.mimeType, _photoName!, image: true);
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _error = 'Could not open the photo picker.');
    } finally {
      if (mounted) setState(() => _picking = false);
    }
  }

  Future<void> _pickVideo() async {
    if (_picking || _saving) return;
    setState(() {
      _picking = true;
      _error = null;
    });
    try {
      final XFile? picked;
      final picker = widget.pickVideo;
      if (picker != null) {
        picked = await picker();
      } else {
        picked = await ImagePicker().pickVideo(
          source: ImageSource.camera,
          maxDuration: const Duration(seconds: 30),
        );
      }
      if (picked == null) return;
      final file = picked;
      final bytes = await file.readAsBytes();
      if (!mounted) return;
      if (bytes.length > maxVideoAttachmentBytes) {
        setState(
          () => _error = 'That video is over 20 MB. Try a shorter clip.',
        );
        return;
      }
      setState(() {
        _photoBytes = null;
        _photoName = null;
        _photoMime = null;
        _videoBytes = bytes;
        _videoName = _nameOr(file.name, 'video.mp4');
        _videoMime = _mimeFor(file.mimeType, _videoName!, image: false);
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _error = 'Could not open the video camera.');
    } finally {
      if (mounted) setState(() => _picking = false);
    }
  }

  Future<void> _openVenuePicker() async {
    if (_saving) return;
    final choice = await showModalBottomSheet<_VenueChoice>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: _topRounded,
      builder: (_) => const _VenuePickerSheet(),
    );
    if (choice == null || !mounted) return;
    setState(() => _venue = choice.venue);
  }

  Future<void> _save() async {
    if (_saving) return;
    FocusScope.of(context).unfocus();
    setState(() {
      _saving = true;
      _error = null;
    });

    final api = ref.read(apiProvider);
    final rating = _notRated ? null : _quarters;
    final key = _key ??= newIdempotencyKey();
    final video = _videoBytes;
    try {
      await api.createCheckin(
        drinkId: widget.drink.id,
        rating: rating,
        note: _note.text,
        venueId: _venue?.id,
        serving: _serving,
        visibility: _visibility,
        idempotencyKey: key,
        photo: video ?? _photoBytes,
        photoFilename: video != null ? _videoName : _photoName,
        photoMime: video != null ? _videoMime : _photoMime,
      );
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      final status = _statusOf(error);
      if (status != null && status >= 400 && status < 500) {
        setState(() {
          _saving = false;
          _error = ApiClient.describe(error).message;
        });
        return;
      }
      if (video != null) {
        setState(() {
          _saving = false;
          _error = 'Video check-ins need a connection. Try again when online.';
        });
        return;
      }
      await _queue(rating, key);
    }
  }

  Future<void> _queue(int? rating, String key) async {
    final bool photoKept;
    try {
      photoKept = await CheckinOutbox().enqueue(
        {
          'drink_id': widget.drink.id,
          'rating': rating,
          'note': _note.text,
          'venue_id': _venue?.id,
          'serving': _serving,
          'visibility': _visibility,
          'idempotency_key': key,
        },
        photo: _photoBytes,
        photoFilename: _photoName,
        photoMime: _photoMime,
      );
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = 'Could not save offline. Try again.';
      });
      return;
    }

    if (!mounted) return;
    final droppedPhoto = _photoBytes != null && !photoKept;
    final messenger = ScaffoldMessenger.of(context);
    Navigator.of(context).pop(true);
    messenger.showSnackBar(
      SnackBar(
        content: Text(
          droppedPhoto
              ? 'Saved offline without the photo. It was too large to keep.'
              : 'Saved offline',
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      top: false,
      child: Padding(
        padding: EdgeInsets.only(
          bottom: MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: SingleChildScrollView(
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 24),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 520),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Center(
                    child: Container(
                      width: 42,
                      height: 4,
                      decoration: BoxDecoration(
                        color: theme.dividerColor,
                        borderRadius: BorderRadius.circular(2),
                      ),
                    ),
                  ),
                  const SizedBox(height: 18),
                  Text(
                    'Log a soda',
                    style: theme.textTheme.labelSmall?.copyWith(
                      letterSpacing: 1.4,
                      fontWeight: FontWeight.w700,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    widget.drink.name,
                    style: theme.textTheme.headlineSmall?.copyWith(
                      fontWeight: FontWeight.w800,
                      letterSpacing: -0.4,
                    ),
                  ),
                  if (widget.drink.subtitle.isNotEmpty)
                    Text(
                      widget.drink.subtitle,
                      style: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  const SizedBox(height: 24),
                  _ratingSection(theme),
                  const SizedBox(height: 22),
                  _servingSection(theme),
                  const SizedBox(height: 22),
                  _venueSection(theme),
                  const SizedBox(height: 22),
                  _visibilitySection(theme),
                  const SizedBox(height: 22),
                  _noteSection(theme),
                  const SizedBox(height: 22),
                  _attachmentSection(theme),
                  if (_error != null) ...[
                    const SizedBox(height: 16),
                    _errorBanner(theme),
                  ],
                  const SizedBox(height: 20),
                  FilledButton(
                    onPressed: _saving || _picking ? null : _save,
                    child: Text(_saving ? 'Saving…' : 'Save check-in'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _ratingSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Text(
              'Your rating',
              style: theme.textTheme.titleMedium?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            const Spacer(),
            Text(
              _ratingText,
              style: theme.textTheme.titleLarge?.copyWith(
                color: Soda.tangerine,
                fontWeight: FontWeight.w800,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
          ],
        ),
        Semantics(
          label: 'Rating',
          child: Slider(
            value: _quarters.toDouble(),
            min: 0,
            max: 20,
            divisions: 20,
            activeColor: Soda.tangerine,
            label: _ratingText,
            onChanged: (value) {
              final quarters = value.round();
              setState(() {
                _quarters = quarters;
                _notRated = quarters == 0;
              });
            },
          ),
        ),
        Align(
          alignment: Alignment.centerLeft,
          child: ChoiceChip(
            label: const Text('Not rated'),
            selected: _notRated,
            showCheckmark: false,
            selectedColor: theme.colorScheme.primary,
            side: BorderSide(
              color: theme.colorScheme.onSurface.withValues(alpha: 0.16),
            ),
            labelStyle: theme.textTheme.labelLarge?.copyWith(
              fontWeight: FontWeight.w600,
              color: _notRated
                  ? theme.colorScheme.onPrimary
                  : theme.colorScheme.onSurfaceVariant,
            ),
            onSelected: (selected) {
              if (!selected) return;
              setState(() {
                _notRated = true;
                _quarters = 0;
              });
            },
          ),
        ),
      ],
    );
  }

  Widget _servingSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _sectionTitle(theme, 'Serving'),
        const SizedBox(height: 8),
        SodaSegmented<String>(
          options: _servings,
          selected: _serving,
          onChanged: (value) {
            if (_saving) return;
            setState(() => _serving = value);
          },
        ),
      ],
    );
  }

  Widget _venueSection(ThemeData theme) {
    final venue = _venue;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _sectionTitle(theme, 'Where', trailing: 'Optional'),
        const SizedBox(height: 8),
        if (venue == null)
          OutlinedButton.icon(
            onPressed: _saving ? null : _openVenuePicker,
            icon: const Icon(Icons.place_outlined, size: 18),
            label: const Text('Choose a venue'),
            style: _outlinedStyle(theme),
          )
        else
          Align(
            alignment: Alignment.centerLeft,
            child: InputChip(
              key: const Key('venue-chip'),
              avatar: const Icon(Icons.place_outlined, size: 16),
              label: Text(venue.name),
              deleteButtonTooltipMessage: 'Remove venue',
              onDeleted: _saving ? null : () => setState(() => _venue = null),
            ),
          ),
      ],
    );
  }

  Widget _visibilitySection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _sectionTitle(theme, 'Who can see it'),
        const SizedBox(height: 8),
        SodaSegmented<String>(
          options: _visibilities,
          selected: _visibility,
          onChanged: (value) {
            if (_saving) return;
            setState(() => _visibility = value);
          },
        ),
        const SizedBox(height: 8),
        Text(
          _visibilityHint,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }

  Widget _noteSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _sectionTitle(theme, 'Note', trailing: 'Optional'),
        const SizedBox(height: 8),
        TextField(
          key: const Key('checkin-note'),
          controller: _note,
          maxLength: 1000,
          minLines: 2,
          maxLines: 6,
          textCapitalization: TextCapitalization.sentences,
          keyboardType: TextInputType.multiline,
          decoration: const InputDecoration(
            hintText: 'Sharp ginger, low sweetness, would buy again.',
            alignLabelWithHint: true,
          ),
        ),
      ],
    );
  }

  Widget _attachmentSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _sectionTitle(theme, 'Photo or video', trailing: 'Optional'),
        const SizedBox(height: 4),
        Text(
          'One attachment. Photos can wait offline; video needs a connection.',
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 10),
        if (_photoBytes != null)
          _mediaRow(
            theme,
            preview: ClipRRect(
              borderRadius: BorderRadius.circular(10),
              child: Image.memory(
                _photoBytes!,
                width: 64,
                height: 64,
                fit: BoxFit.cover,
                gaplessPlayback: true,
                errorBuilder: (_, _, _) => Container(
                  width: 64,
                  height: 64,
                  color: theme.colorScheme.surfaceContainerHighest,
                  child: const Icon(Icons.broken_image_outlined),
                ),
              ),
            ),
            label: 'Photo attached',
            onRemove: _saving ? null : _removePhoto,
          )
        else if (_hasVideo)
          _mediaRow(
            theme,
            preview: Container(
              width: 64,
              height: 64,
              decoration: BoxDecoration(
                color: theme.colorScheme.primary.withValues(alpha: 0.22),
                borderRadius: BorderRadius.circular(10),
              ),
              child: Icon(
                Icons.videocam_outlined,
                color: theme.colorScheme.onSurface,
              ),
            ),
            label: 'Video attached',
            onRemove: _saving ? null : _removeVideo,
          )
        else
          Row(
            children: [
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: _saving || _picking ? null : _pickPhoto,
                  icon: const Icon(Icons.photo_camera_outlined, size: 18),
                  label: Text(_picking ? 'Opening…' : 'Add a photo'),
                  style: _outlinedStyle(theme),
                ),
              ),
              const SizedBox(width: 8),
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: _saving || _picking ? null : _pickVideo,
                  icon: const Icon(Icons.videocam_outlined, size: 18),
                  label: const Text('Add a video'),
                  style: _outlinedStyle(theme),
                ),
              ),
            ],
          ),
      ],
    );
  }

  Widget _mediaRow(
    ThemeData theme, {
    required Widget preview,
    required String label,
    required VoidCallback? onRemove,
  }) {
    return Container(
      padding: const EdgeInsets.all(8),
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
        borderRadius: BorderRadius.circular(14),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
        ),
      ),
      child: Row(
        children: [
          preview,
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              label,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          TextButton(onPressed: onRemove, child: const Text('Remove')),
        ],
      ),
    );
  }

  Widget _errorBanner(ThemeData theme) {
    return Semantics(
      liveRegion: true,
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: theme.colorScheme.error.withValues(alpha: 0.08),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(
            color: theme.colorScheme.error.withValues(alpha: 0.3),
          ),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              Icons.error_outline_rounded,
              size: 18,
              color: theme.colorScheme.error,
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                _error!,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.error,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _sectionTitle(ThemeData theme, String title, {String? trailing}) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.baseline,
      textBaseline: TextBaseline.alphabetic,
      children: [
        Expanded(
          child: Text(
            title,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w700,
            ),
          ),
        ),
        if (trailing != null)
          Text(
            trailing,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
      ],
    );
  }

  ButtonStyle _outlinedStyle(ThemeData theme) => OutlinedButton.styleFrom(
    minimumSize: const Size(0, 48),
    foregroundColor: theme.colorScheme.onSurface,
    side: BorderSide(
      color: theme.colorScheme.onSurface.withValues(alpha: 0.18),
    ),
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
  );

  static const _topRounded = RoundedRectangleBorder(
    borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
  );

  static int? _statusOf(Object error) {
    if (error is ApiException) return error.statusCode;
    if (error is DioException) return error.response?.statusCode;
    return null;
  }

  static String _nameOr(String raw, String fallback) {
    final name = raw.trim();
    return name.isEmpty ? fallback : name;
  }

  static String _mimeFor(String? declared, String name, {required bool image}) {
    final mime = declared?.trim() ?? '';
    if (mime.contains('/') &&
        (image ? mime.startsWith('image/') : mime.startsWith('video/'))) {
      return mime;
    }
    final dot = name.lastIndexOf('.');
    final ext = dot == -1 ? '' : name.substring(dot + 1).toLowerCase();
    if (image) {
      switch (ext) {
        case 'png':
          return 'image/png';
        case 'webp':
          return 'image/webp';
        case 'gif':
          return 'image/gif';
        default:
          return 'image/jpeg';
      }
    }
    switch (ext) {
      case 'mov':
        return 'video/quicktime';
      case 'webm':
        return 'video/webm';
      default:
        return 'video/mp4';
    }
  }
}

/// Camera or gallery, picked in a small sheet. The chosen source is the
/// sheet's result; dismissing it returns null.
class _PhotoSourceSheet extends StatelessWidget {
  const _PhotoSourceSheet();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 12, 12, 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Center(
              child: Container(
                width: 42,
                height: 4,
                decoration: BoxDecoration(
                  color: theme.dividerColor,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            const SizedBox(height: 10),
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
          ],
        ),
      ),
    );
  }
}

/// A venue result plus the explicit "no venue" choice, which is distinct from
/// dismissing the picker.
class _VenueChoice {
  const _VenueChoice(this.venue);

  final Venue? venue;
}

class _VenuePickerSheet extends ConsumerStatefulWidget {
  const _VenuePickerSheet();

  @override
  ConsumerState<_VenuePickerSheet> createState() => _VenuePickerSheetState();
}

class _VenuePickerSheetState extends ConsumerState<_VenuePickerSheet> {
  final _query = TextEditingController();
  List<Venue> _results = const [];
  bool _searching = false;
  bool _searched = false;
  String? _error;
  String? _hint;

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _search() async {
    final query = _query.text.trim();
    if (query.length < 2) {
      setState(() {
        _hint = 'Type at least two characters.';
        _searched = false;
        _results = const [];
        _error = null;
      });
      return;
    }
    setState(() {
      _searching = true;
      _searched = true;
      _error = null;
      _hint = null;
    });
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

  void _choose(Venue? venue) {
    Navigator.of(context).pop(_VenueChoice(venue));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final media = MediaQuery.of(context);
    final available = media.size.height - media.viewInsets.bottom;
    final height = (available * 0.72).clamp(240.0, 560.0).toDouble();
    return Padding(
      padding: EdgeInsets.only(bottom: media.viewInsets.bottom),
      child: SizedBox(
        height: height,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            const SizedBox(height: 10),
            Center(
              child: Container(
                width: 42,
                height: 4,
                decoration: BoxDecoration(
                  color: theme.dividerColor,
                  borderRadius: BorderRadius.circular(2),
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 8, 8, 0),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      'Choose a venue',
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  TextButton(
                    onPressed: () => _choose(null),
                    child: const Text('No venue'),
                  ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 8, 20, 12),
              child: TextField(
                key: const Key('venue-query'),
                controller: _query,
                autofocus: true,
                textInputAction: TextInputAction.search,
                decoration: InputDecoration(
                  hintText: 'Search by name or city',
                  suffixIcon: IconButton(
                    onPressed: _search,
                    tooltip: 'Search venues',
                    icon: const Icon(Icons.search_rounded),
                  ),
                ),
                onSubmitted: (_) => _search(),
              ),
            ),
            Expanded(child: _body(theme)),
          ],
        ),
      ),
    );
  }

  Widget _body(ThemeData theme) {
    if (_searching) {
      return const SodaLoadingView(dense: true, label: 'Searching venues…');
    }
    if (_error != null) {
      return SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.cloud_off_rounded,
          title: 'Could not search venues',
          message: _error,
          actionLabel: 'Try again',
          onAction: _search,
        ),
      );
    }
    if (!_searched) {
      return SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.place_outlined,
          title: 'Find the place',
          message:
              _hint ?? 'Search by name or city. Results are capped at ten.',
        ),
      );
    }
    if (_results.isEmpty) {
      return const SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.search_off_rounded,
          title: 'No venues matched',
          message: 'Try a shorter name or a nearby city.',
        ),
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.fromLTRB(12, 0, 12, 20),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      itemCount: _results.length,
      separatorBuilder: (_, _) => const SizedBox(height: 4),
      itemBuilder: (context, index) {
        final venue = _results[index];
        final meta = [
          sodaVenueKind(venue.kind),
          venue.city,
        ].where((value) => value.isNotEmpty).join(' · ');
        return ListTile(
          leading: Icon(
            Icons.place_outlined,
            color: theme.colorScheme.onSurfaceVariant,
          ),
          title: Text(
            venue.name,
            style: const TextStyle(fontWeight: FontWeight.w600),
          ),
          subtitle: meta.isEmpty ? null : Text(meta),
          shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(12),
          ),
          onTap: () => _choose(venue),
        );
      },
    );
  }
}
