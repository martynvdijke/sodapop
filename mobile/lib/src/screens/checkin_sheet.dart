import 'dart:async';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';
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

/// What happened when the sheet closed. A queued check-in has no server id yet.
class CheckinSave {
  const CheckinSave.saved(this.id) : queued = false;
  const CheckinSave.queued() : id = '', queued = true;

  final String id;
  final bool queued;

  bool get saved => queued || id.isNotEmpty;
}

Future<CheckinSave?> showCheckinSheet(BuildContext context, Drink drink) async {
  return showModalBottomSheet<CheckinSave>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => CheckinSheet(drink: drink),
  );
}

class CheckinSheet extends ConsumerStatefulWidget {
  const CheckinSheet({
    super.key,
    required this.drink,
    this.pickImage,
    this.pickVideo,
    this.locationProvider,
    this.permissionChecker,
    this.permissionRequester,
    this.serviceChecker,
    this.settingsOpener,
    this.serviceSettingsOpener,
  });

  final Drink drink;

  /// Optional overrides so widget tests can attach media without a camera.
  final CheckinImagePicker? pickImage;
  final CheckinVideoPicker? pickVideo;

  /// Location hooks mirror [NearbyScreen], so tests never touch a platform
  /// channel and permission is only requested from a tap.
  final Future<Position> Function()? locationProvider;
  final Future<LocationPermission> Function()? permissionChecker;
  final Future<LocationPermission> Function()? permissionRequester;
  final Future<bool> Function()? serviceChecker;
  final Future<bool> Function()? settingsOpener;
  final Future<bool> Function()? serviceSettingsOpener;

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
    ('private', 'Private'),
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

  final List<FriendUser> _tagged = [];
  double? _lat;
  double? _lng;
  bool _locating = false;
  String? _locationMessage;
  bool _needsSettings = false;
  bool _needsService = false;

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
        return 'Only your friends can see this check-in.';
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
      builder: (_) => _VenuePickerSheet(lat: _lat, lng: _lng),
    );
    if (choice == null || !mounted) return;
    setState(() => _venue = choice.venue);
  }

  Future<void> _openFriendPicker() async {
    if (_saving) return;
    final chosen = await showModalBottomSheet<List<FriendUser>>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: _topRounded,
      builder: (_) => _FriendPickerSheet(
        selectedIds: _tagged.map((friend) => friend.id).toSet(),
      ),
    );
    if (chosen == null || !mounted) return;
    setState(() {
      _tagged
        ..clear()
        ..addAll(chosen);
    });
  }

  static Future<Position> _defaultFix() => Geolocator.getCurrentPosition(
    locationSettings: const LocationSettings(
      accuracy: LocationAccuracy.medium,
      timeLimit: Duration(seconds: 10),
    ),
  );

  /// One deliberate tap asks for permission, then stores a fix for the
  /// check-in and for nearby venue suggestions.
  Future<void> _useLocation() async {
    if (_locating || _saving) return;
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
              'Location services are off. Turn them on to pick nearby venues.';
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
              'Location is blocked for Sodapop. Allow it in settings to pick nearby venues.';
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

      final position = await (widget.locationProvider ?? _defaultFix)();
      if (!mounted) return;
      setState(() {
        _lat = double.parse(position.latitude.toStringAsFixed(5));
        _lng = double.parse(position.longitude.toStringAsFixed(5));
        _locating = false;
        _locationMessage = null;
      });
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
            'Location services are off. Turn them on to pick nearby venues.';
      });
    } on PermissionDeniedException {
      if (!mounted) return;
      setState(() {
        _locating = false;
        _locationMessage =
            'Location permission is needed to pick nearby venues.';
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _locating = false;
        _locationMessage = 'Could not read your location. Try again.';
      });
    }
  }

  void _clearLocation() {
    setState(() {
      _lat = null;
      _lng = null;
      _locationMessage = null;
      _needsSettings = false;
      _needsService = false;
    });
  }

  Future<void> _openSettings() async {
    await (widget.settingsOpener ?? Geolocator.openAppSettings)();
  }

  Future<void> _openLocationSettings() async {
    await (widget.serviceSettingsOpener ?? Geolocator.openLocationSettings)();
  }

  Future<void> _save() async {
    if (_saving) return;
    FocusScope.of(context).unfocus();
    setState(() {
      _saving = true;
      _error = null;
    });

    final api = ref.read(apiProvider);
    final messenger = ScaffoldMessenger.of(context);
    final rating = _notRated ? null : _quarters;
    final key = _key ??= newIdempotencyKey();
    final video = _videoBytes;
    try {
      final id = await api.createCheckin(
        drinkId: widget.drink.id,
        rating: rating,
        note: _note.text,
        venueId: _venue?.id,
        serving: _serving,
        visibility: _visibility,
        idempotencyKey: key,
        lat: _lat,
        lng: _lng,
        friendIds: _tagged.map((friend) => friend.id).toList(),
        photo: video ?? _photoBytes,
        photoFilename: video != null ? _videoName : _photoName,
        photoMime: video != null ? _videoMime : _photoMime,
      );
      if (!mounted) return;
      messenger.showSnackBar(const SnackBar(content: Text('Check-in saved')));
      Navigator.of(context).pop(CheckinSave.saved(id));
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
          if (_lat != null && _lng != null) 'lat': _lat,
          if (_lat != null && _lng != null) 'lng': _lng,
          if (_tagged.isNotEmpty)
            'friend_ids': _tagged.map((friend) => friend.id).toList(),
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
    Navigator.of(context).pop(const CheckinSave.queued());
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
                  _friendsSection(theme),
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
                    onPressed: _saving || _picking || _locating ? null : _save,
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

  Widget _friendsSection(ThemeData theme) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _sectionTitle(theme, 'With friends', trailing: 'Optional'),
        const SizedBox(height: 8),
        if (_tagged.isEmpty)
          OutlinedButton.icon(
            key: const Key('checkin-friends'),
            onPressed: _saving ? null : _openFriendPicker,
            icon: const Icon(Icons.group_add_outlined, size: 18),
            label: const Text('Tag friends'),
            style: _outlinedStyle(theme),
          )
        else
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final friend in _tagged)
                InputChip(
                  key: Key('checkin-friend-${friend.id}'),
                  avatar: const Icon(Icons.person_outline_rounded, size: 16),
                  label: Text(
                    friend.displayName.isEmpty
                        ? '@${friend.handle}'
                        : friend.displayName,
                  ),
                  deleteButtonTooltipMessage: 'Remove tagged friend',
                  onDeleted: _saving
                      ? null
                      : () => setState(
                          () =>
                              _tagged.removeWhere((row) => row.id == friend.id),
                        ),
                ),
              ActionChip(
                key: const Key('checkin-friends-edit'),
                avatar: const Icon(Icons.edit_outlined, size: 16),
                label: const Text('Edit'),
                onPressed: _saving ? null : _openFriendPicker,
              ),
            ],
          ),
        const SizedBox(height: 8),
        Text(
          'They appear on the check-in with you.',
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }

  Widget _venueSection(ThemeData theme) {
    final venue = _venue;
    final hasFix = _lat != null && _lng != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _sectionTitle(theme, 'Where', trailing: 'Optional'),
        const SizedBox(height: 8),
        Text(
          hasFix
              ? 'Nearby venues are sorted by distance. Your location is saved '
                    'with this check-in.'
              : 'Pick a venue, or use your location to see what is nearby.',
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 10),
        if (!hasFix)
          OutlinedButton.icon(
            key: const Key('checkin-location'),
            onPressed: _locating || _saving ? null : _useLocation,
            icon: _locating
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.my_location_rounded, size: 18),
            label: Text(_locating ? 'Finding you…' : 'Use my location'),
            style: _outlinedStyle(theme),
          )
        else
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              InputChip(
                key: const Key('checkin-location-chip'),
                avatar: const Icon(Icons.my_location_rounded, size: 16),
                label: const Text('Location added'),
                deleteButtonTooltipMessage: 'Remove location',
                onDeleted: _saving ? null : _clearLocation,
              ),
              ActionChip(
                key: const Key('checkin-location-update'),
                avatar: const Icon(Icons.refresh_rounded, size: 16),
                label: Text(_locating ? 'Updating…' : 'Update'),
                onPressed: _locating || _saving ? null : _useLocation,
              ),
            ],
          ),
        if (_locationMessage != null) ...[
          const SizedBox(height: 10),
          _locationNotice(theme),
        ],
        const SizedBox(height: 10),
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

  Widget _locationNotice(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 10, 12, 6),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer.withValues(alpha: 0.55),
        borderRadius: BorderRadius.circular(12),
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
                key: const Key('checkin-open-settings'),
                onPressed: _openSettings,
                child: const Text('Open settings'),
              ),
            ),
          if (_needsService)
            Align(
              alignment: Alignment.centerRight,
              child: TextButton(
                key: const Key('checkin-enable-services'),
                onPressed: _openLocationSettings,
                child: const Text('Enable location services'),
              ),
            ),
        ],
      ),
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
  const _VenuePickerSheet({this.lat, this.lng});

  /// A fix lets the picker open on nearby venues and order search results.
  final double? lat;
  final double? lng;

  @override
  ConsumerState<_VenuePickerSheet> createState() => _VenuePickerSheetState();
}

class _VenuePickerSheetState extends ConsumerState<_VenuePickerSheet> {
  static const _searchRadiusM = 25000;

  final _query = TextEditingController();
  Timer? _debounce;
  List<Venue> _results = const [];
  bool _loading = false;
  bool _browsingNearby = false;
  String? _error;
  String? _hint;
  int _requestId = 0;

  bool get _hasFix => widget.lat != null && widget.lng != null;
  String get _queryText => _query.text.trim();
  String get _radiusLabel => '${_searchRadiusM ~/ 1000} km';

  String get _modeTitle {
    if (_browsingNearby) return 'Venues near you';
    if (_queryText.isNotEmpty) return 'Search results';
    return 'Search venues';
  }

  String get _modeSubtitle {
    if (_browsingNearby) return 'Nearest first · within $_radiusLabel';
    if (_queryText.isNotEmpty) {
      return _hasFix
          ? 'Nearest matches first, with distance'
          : 'Results are capped at ten';
    }
    return _hasFix
        ? 'Nearest first · within $_radiusLabel'
        : 'Turn on location in the check-in to see nearby venues.';
  }

  @override
  void initState() {
    super.initState();
    if (_hasFix) _loadNearby();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _query.dispose();
    super.dispose();
  }

  /// The browse state: every venue within range, nearest first. Only runs when
  /// the sheet was opened with a fix.
  Future<void> _loadNearby() async {
    if (!_hasFix) return;
    _debounce?.cancel();
    final request = ++_requestId;
    setState(() {
      _loading = true;
      _browsingNearby = true;
      _error = null;
      _hint = null;
    });
    try {
      final results = await ref
          .read(apiProvider)
          .venues(
            '',
            lat: widget.lat,
            lng: widget.lng,
            radiusM: _searchRadiusM,
          );
      if (!mounted || request != _requestId) return;
      setState(() {
        _results = results;
        _loading = false;
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      setState(() {
        _loading = false;
        _results = const [];
        _error = ApiClient.describe(error).message;
      });
    }
  }

  void _onQueryChanged(String _) {
    _debounce?.cancel();
    final query = _query.text.trim();
    setState(() => _hint = null);
    if (query.isEmpty) {
      if (_hasFix) {
        _loadNearby();
      } else {
        setState(() {
          _results = const [];
          _error = null;
          _loading = false;
          _browsingNearby = false;
        });
      }
      return;
    }
    if (query.length < 2) {
      setState(() => _hint = 'Type at least two characters.');
      return;
    }
    // Short debounce keeps each keystroke from hitting the server.
    _debounce = Timer(const Duration(milliseconds: 300), _search);
  }

  Future<void> _search() async {
    _debounce?.cancel();
    final query = _query.text.trim();
    if (query.isEmpty) {
      if (_hasFix) {
        await _loadNearby();
      } else {
        setState(() {
          _results = const [];
          _error = null;
          _hint = null;
          _loading = false;
          _browsingNearby = false;
        });
      }
      return;
    }
    if (query.length < 2) {
      setState(() => _hint = 'Type at least two characters.');
      return;
    }
    final request = ++_requestId;
    setState(() {
      _loading = true;
      _browsingNearby = false;
      _error = null;
      _hint = null;
    });
    try {
      final results = await ref
          .read(apiProvider)
          .venues(
            query,
            lat: widget.lat,
            lng: widget.lng,
            radiusM: _hasFix ? _searchRadiusM : null,
          );
      if (!mounted || request != _requestId) return;
      setState(() {
        _results = results;
        _loading = false;
      });
    } catch (error) {
      if (!mounted || request != _requestId) return;
      setState(() {
        _loading = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  void _choose(Venue? venue) {
    Navigator.of(context).pop(_VenueChoice(venue));
  }

  Future<void> _openCreate() async {
    final venue = await showModalBottomSheet<Venue>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: _CheckinSheetState._topRounded,
      builder: (_) => _CreateVenueSheet(
        initialName: _query.text.trim(),
        lat: widget.lat,
        lng: widget.lng,
      ),
    );
    if (venue != null && mounted) _choose(venue);
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
              padding: const EdgeInsets.fromLTRB(20, 8, 20, 4),
              child: TextField(
                key: const Key('venue-query'),
                controller: _query,
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
                onChanged: _onQueryChanged,
              ),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 0, 8, 4),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          _modeTitle,
                          style: theme.textTheme.labelLarge?.copyWith(
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        Text(
                          _modeSubtitle,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                  ),
                  TextButton.icon(
                    key: const Key('venue-create'),
                    onPressed: _openCreate,
                    icon: const Icon(Icons.add_location_alt_outlined, size: 18),
                    label: const Text('Create venue'),
                  ),
                ],
              ),
            ),
            Expanded(child: _body(theme)),
          ],
        ),
      ),
    );
  }

  Widget _body(ThemeData theme) {
    if (_loading) {
      return SodaLoadingView(
        dense: true,
        label: _browsingNearby
            ? 'Finding venues near you…'
            : 'Searching venues…',
      );
    }
    if (_error != null) {
      return SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.cloud_off_rounded,
          title: _browsingNearby
              ? 'Could not load nearby venues'
              : 'Could not search venues',
          message: _error,
          actionLabel: 'Try again',
          onAction: () {
            if (_browsingNearby) {
              _loadNearby();
            } else {
              _search();
            }
          },
        ),
      );
    }
    final query = _queryText;
    if (!_hasFix && query.isEmpty) {
      return SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.place_outlined,
          title: 'Find the place',
          message:
              _hint ?? 'Search by name or city. Results are capped at ten.',
          actionLabel: 'Create a venue',
          onAction: _openCreate,
        ),
      );
    }
    if (_results.isEmpty) {
      if (_hint != null) {
        return SingleChildScrollView(
          child: SodaStatusView(
            dense: true,
            icon: Icons.keyboard_outlined,
            title: 'Keep typing',
            message: _hint,
            actionLabel: 'Create a venue',
            onAction: _openCreate,
          ),
        );
      }
      final nearbyEmpty = _browsingNearby && query.isEmpty;
      return SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: nearbyEmpty
              ? Icons.location_searching_rounded
              : Icons.search_off_rounded,
          title: nearbyEmpty
              ? 'No venues within $_radiusLabel'
              : 'No venues matched',
          message: nearbyEmpty
              ? 'Nothing is close enough yet. Create the place you are at, or '
                    'search by name.'
              : 'Try a shorter name, or add the place yourself.',
          actionLabel: 'Create a venue',
          onAction: _openCreate,
        ),
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (_hint != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 6),
            child: Text(
              _hint!,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        Expanded(
          child: ListView.separated(
            padding: const EdgeInsets.fromLTRB(12, 0, 12, 20),
            keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
            itemCount: _results.length,
            separatorBuilder: (_, _) => const SizedBox(height: 4),
            itemBuilder: (context, index) {
              final venue = _results[index];
              final meta = [
                if (venue.distanceM > 0) _distanceLabel(venue.distanceM),
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
          ),
        ),
      ],
    );
  }

  static String _distanceLabel(int metres) {
    if (metres < 1000) return '$metres m';
    final km = metres / 1000;
    return '${km.toStringAsFixed(km < 10 ? 1 : 0)} km';
  }
}

/// Multi-select of accepted friends. The list is loaded lazily so opening the
/// check-in sheet never waits on the friends endpoint.
class _FriendPickerSheet extends ConsumerStatefulWidget {
  const _FriendPickerSheet({required this.selectedIds});

  final Set<String> selectedIds;

  @override
  ConsumerState<_FriendPickerSheet> createState() => _FriendPickerSheetState();
}

class _FriendPickerSheetState extends ConsumerState<_FriendPickerSheet> {
  static const _searchThreshold = 6;

  final _query = TextEditingController();
  final Set<String> _selected = {};
  List<FriendUser> _friends = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _selected.addAll(widget.selectedIds);
    _load();
  }

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final directory = await ref.read(apiProvider).friends();
      if (!mounted) return;
      setState(() {
        _friends = directory.friends;
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

  List<FriendUser> get _visible {
    final query = _query.text.trim().toLowerCase();
    if (query.isEmpty) return _friends;
    return _friends
        .where(
          (friend) =>
              friend.displayName.toLowerCase().contains(query) ||
              friend.handle.toLowerCase().contains(query),
        )
        .toList();
  }

  void _done() {
    Navigator.of(
      context,
    ).pop(_friends.where((friend) => _selected.contains(friend.id)).toList());
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
                      'Tag friends',
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                  TextButton(
                    key: const Key('friend-picker-done'),
                    onPressed: _done,
                    child: const Text('Done'),
                  ),
                ],
              ),
            ),
            if (_friends.length > _searchThreshold)
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 8, 20, 8),
                child: TextField(
                  key: const Key('friend-search'),
                  controller: _query,
                  decoration: const InputDecoration(
                    hintText: 'Search friends',
                    prefixIcon: Icon(Icons.search_rounded, size: 20),
                  ),
                  onChanged: (_) => setState(() {}),
                ),
              ),
            Expanded(child: _body(theme)),
          ],
        ),
      ),
    );
  }

  Widget _body(ThemeData theme) {
    if (_loading) {
      return const SodaLoadingView(dense: true, label: 'Loading friends…');
    }
    if (_error != null) {
      return SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.cloud_off_rounded,
          title: 'Could not load friends',
          message: _error,
          actionLabel: 'Try again',
          onAction: _load,
        ),
      );
    }
    if (_friends.isEmpty) {
      return SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.group_outlined,
          title: 'No friends yet',
          message: 'Add someone from the Friends tab, then tag them next time.',
          actionLabel: 'Close',
          onAction: () => Navigator.of(context).pop(const <FriendUser>[]),
        ),
      );
    }
    final visible = _visible;
    if (visible.isEmpty) {
      return const SingleChildScrollView(
        child: SodaStatusView(
          dense: true,
          icon: Icons.search_off_rounded,
          title: 'No friends matched',
          message: 'Try part of a name or handle.',
        ),
      );
    }
    return ListView.builder(
      padding: const EdgeInsets.fromLTRB(12, 0, 12, 20),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      itemCount: visible.length,
      itemBuilder: (context, index) {
        final friend = visible[index];
        final name = friend.displayName.isEmpty
            ? '@${friend.handle}'
            : friend.displayName;
        return CheckboxListTile(
          key: Key('friend-pick-${friend.id}'),
          value: _selected.contains(friend.id),
          onChanged: (checked) {
            setState(() {
              if (checked ?? false) {
                _selected.add(friend.id);
              } else {
                _selected.remove(friend.id);
              }
            });
          },
          controlAffinity: ListTileControlAffinity.trailing,
          secondary: SodaAvatar(
            initial: friend.displayName.isEmpty
                ? friend.handle
                : friend.displayName,
            size: 32,
          ),
          title: Text(
            name,
            style: const TextStyle(fontWeight: FontWeight.w600),
          ),
          subtitle: friend.handle.isEmpty ? null : Text('@${friend.handle}'),
        );
      },
    );
  }
}

/// Creates a venue without leaving the check-in flow. The current fix, when
/// there is one, is attached so the new place can be found again.
class _CreateVenueSheet extends ConsumerStatefulWidget {
  const _CreateVenueSheet({this.initialName = '', this.lat, this.lng});

  final String initialName;
  final double? lat;
  final double? lng;

  @override
  ConsumerState<_CreateVenueSheet> createState() => _CreateVenueSheetState();
}

class _CreateVenueSheetState extends ConsumerState<_CreateVenueSheet> {
  final _formKey = GlobalKey<FormState>();
  late final _name = TextEditingController(text: widget.initialName);
  final _address = TextEditingController();
  final _city = TextEditingController();
  final _country = TextEditingController();
  bool _saving = false;
  String? _error;

  bool get _hasFix => widget.lat != null && widget.lng != null;

  @override
  void dispose() {
    _name.dispose();
    _address.dispose();
    _city.dispose();
    _country.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (_saving) return;
    if (!(_formKey.currentState?.validate() ?? false)) return;
    FocusScope.of(context).unfocus();
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final venue = await ref
          .read(apiProvider)
          .createVenue(
            name: _name.text.trim(),
            address: _address.text.trim(),
            city: _city.text.trim(),
            country: _country.text.trim(),
            lat: widget.lat,
            lng: widget.lng,
          );
      if (!mounted) return;
      Navigator.of(context).pop(venue);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: SafeArea(
        top: false,
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 24),
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 520),
              child: Form(
                key: _formKey,
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
                      'Create a venue',
                      style: theme.textTheme.titleLarge?.copyWith(
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      _hasFix
                          ? 'It joins the shared venue list with your current location.'
                          : 'It joins the shared venue list. Add a name and the place you know.',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                    const SizedBox(height: 16),
                    TextFormField(
                      key: const Key('venue-create-name'),
                      controller: _name,
                      textCapitalization: TextCapitalization.words,
                      decoration: const InputDecoration(
                        labelText: 'Name',
                        hintText: 'Corner Shop',
                      ),
                      validator: (value) {
                        if ((value ?? '').trim().isEmpty) {
                          return 'Add a name for the venue.';
                        }
                        return null;
                      },
                    ),
                    const SizedBox(height: 12),
                    TextFormField(
                      key: const Key('venue-create-address'),
                      controller: _address,
                      textCapitalization: TextCapitalization.words,
                      decoration: const InputDecoration(
                        labelText: 'Address',
                        hintText: '12 High Street',
                      ),
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: TextFormField(
                            key: const Key('venue-create-city'),
                            controller: _city,
                            textCapitalization: TextCapitalization.words,
                            decoration: const InputDecoration(
                              labelText: 'City',
                              hintText: 'Leeds',
                            ),
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: TextFormField(
                            key: const Key('venue-create-country'),
                            controller: _country,
                            textCapitalization: TextCapitalization.words,
                            decoration: const InputDecoration(
                              labelText: 'Country',
                              hintText: 'United Kingdom',
                            ),
                          ),
                        ),
                      ],
                    ),
                    if (_error != null) ...[
                      const SizedBox(height: 12),
                      Container(
                        padding: const EdgeInsets.all(12),
                        decoration: BoxDecoration(
                          color: theme.colorScheme.error.withValues(
                            alpha: 0.08,
                          ),
                          borderRadius: BorderRadius.circular(12),
                        ),
                        child: Text(
                          _error!,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.error,
                          ),
                        ),
                      ),
                    ],
                    const SizedBox(height: 18),
                    FilledButton(
                      key: const Key('venue-create-submit'),
                      onPressed: _saving ? null : _save,
                      child: Text(_saving ? 'Creating…' : 'Create venue'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
