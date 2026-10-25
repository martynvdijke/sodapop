import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

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

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
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
    await CheckinOutbox().enqueue({
      'drink_id': widget.drink.id,
      'rating': rating,
      'note': _note.text,
      'idempotency_key': key,
    });
    if (!mounted) return;
    Navigator.of(context).pop(true);
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
                decoration: BoxDecoration(color: theme.dividerColor, borderRadius: BorderRadius.circular(2)),
              ),
            ),
            const SizedBox(height: 18),
            Text(widget.drink.name, style: theme.textTheme.headlineSmall?.copyWith(fontWeight: FontWeight.w800)),
            Text(widget.drink.subtitle, style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
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
              decoration: const InputDecoration(hintText: 'A short tasting note (optional)'),
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
