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

Future<bool> showCheckinSheet(BuildContext context, Drink drink) async {
  final result = await showModalBottomSheet<bool>(
    context: context,
    isScrollControlled: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => CheckinSheet(drink: drink),
  );
  return result ?? false;
}

class CheckinSheet extends ConsumerStatefulWidget {
  const CheckinSheet({super.key, required this.drink});

  final Drink drink;

  @override
  ConsumerState<CheckinSheet> createState() => _CheckinSheetState();
}

class _CheckinSheetState extends ConsumerState<CheckinSheet> {
  final _note = TextEditingController();
  int _quarters = 0;
  bool _saving = false;
  bool _notRated = true;
  bool _picking = false;
  Uint8List? _photoBytes;
  String? _photoName;
  String? _photoMime;

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  Future<void> _pickPhoto() async {
    if (_picking) return;
    setState(() => _picking = true);
    try {
      final picked = await ImagePicker().pickImage(
        source: ImageSource.gallery,
        maxWidth: 1600,
        imageQuality: 85,
      );
      if (picked == null) return;
      final bytes = await picked.readAsBytes();
      if (!mounted) return;
      setState(() {
        _photoBytes = bytes;
        _photoName = picked.name;
        _photoMime = picked.mimeType;
      });
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Could not open the photo picker.')),
      );
    } finally {
      if (mounted) setState(() => _picking = false);
    }
  }

  void _removePhoto() {
    setState(() {
      _photoBytes = null;
      _photoName = null;
      _photoMime = null;
    });
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    final api = ref.read(apiProvider);
    final rating = _notRated ? null : _quarters;
    final key = newIdempotencyKey();
    try {
      await api.createCheckin(
        drinkId: widget.drink.id,
        rating: rating,
        note: _note.text,
        idempotencyKey: key,
        photo: _photoBytes,
        photoFilename: _photoName,
        photoMime: _photoMime,
      );
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } on DioException catch (error) {
      final status = error.response?.statusCode;
      if (status != null && status >= 400 && status < 500) {
        if (!mounted) return;
        setState(() => _saving = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(ApiClient.describe(error).message)),
        );
        return;
      }
      await _queue(rating, key);
    } catch (_) {
      await _queue(rating, key);
    }
  }

  Future<void> _queue(int? rating, String key) async {
    final photoKept = await CheckinOutbox().enqueue(
      {
        'drink_id': widget.drink.id,
        'rating': rating,
        'note': _note.text,
        'idempotency_key': key,
      },
      photo: _photoBytes,
      photoFilename: _photoName,
      photoMime: _photoMime,
    );
    if (!mounted) return;
    final droppedPhoto = _photoBytes != null && !photoKept;
    final messenger = ScaffoldMessenger.of(context);
    Navigator.of(context).pop(true);
    if (droppedPhoto) {
      messenger.showSnackBar(
        const SnackBar(
          content: Text(
            'Photo was too large to save offline, so the check-in was queued without it.',
          ),
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 28),
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
              widget.drink.name,
              style: theme.textTheme.headlineSmall?.copyWith(
                fontWeight: FontWeight.w800,
              ),
            ),
            Text(
              widget.drink.subtitle,
              style: theme.textTheme.bodyMedium?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 24),
            Row(
              children: [
                Text('Your rating', style: theme.textTheme.titleMedium),
                const Spacer(),
                Text(
                  _notRated ? 'Not rated' : ratingLabel(_quarters),
                  style: theme.textTheme.titleLarge?.copyWith(
                    color: Soda.tangerine,
                    fontWeight: FontWeight.w800,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
              ],
            ),
            Slider(
              value: _quarters.toDouble(),
              min: 0,
              max: 20,
              divisions: 20,
              activeColor: Soda.tangerine,
              label: ratingLabel(_quarters),
              onChanged: (value) => setState(() {
                _quarters = value.round();
                _notRated = false;
              }),
            ),
            const SizedBox(height: 8),
            TextField(
              controller: _note,
              maxLength: 280,
              minLines: 2,
              maxLines: 4,
              decoration: const InputDecoration(
                hintText: 'A short tasting note (optional)',
              ),
            ),
            const SizedBox(height: 8),
            if (_photoBytes != null)
              Row(
                children: [
                  ClipRRect(
                    borderRadius: BorderRadius.circular(12),
                    child: Image.memory(
                      _photoBytes!,
                      width: 84,
                      height: 84,
                      fit: BoxFit.cover,
                      gaplessPlayback: true,
                      errorBuilder: (_, _, _) => Container(
                        width: 84,
                        height: 84,
                        color: theme.colorScheme.surfaceContainerHighest,
                        child: const Icon(Icons.broken_image_outlined),
                      ),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Text(
                      'Photo attached',
                      style: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                  TextButton(
                    onPressed: _saving ? null : _removePhoto,
                    child: const Text('Remove'),
                  ),
                ],
              )
            else
              OutlinedButton.icon(
                onPressed: _saving || _picking ? null : _pickPhoto,
                icon: const Icon(Icons.photo_library_outlined),
                label: Text(_picking ? 'Opening…' : 'Add a photo'),
              ),
            const SizedBox(height: 12),
            FilledButton(
              onPressed: _saving ? null : _save,
              child: Text(_saving ? 'Saving…' : 'Save check-in'),
            ),
          ],
        ),
      ),
    );
  }
}
