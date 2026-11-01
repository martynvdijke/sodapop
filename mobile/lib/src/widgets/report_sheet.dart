import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';

/// Google Play requires every app that shows user-generated content to offer a
/// way to report it from inside the app. This sheet is that path for check-ins,
/// comments and profiles; it files the report and tells the person what
/// happens next without promising a specific outcome.
Future<void> showReportSheet(
  BuildContext context, {
  required String entity,
  String? id,
  String? handle,
  required String title,
}) {
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    showDragHandle: true,
    builder: (_) =>
        ReportSheet(entity: entity, id: id, handle: handle, title: title),
  );
}

class ReportSheet extends ConsumerStatefulWidget {
  const ReportSheet({
    super.key,
    required this.entity,
    required this.title,
    this.id,
    this.handle,
  });

  final String entity;
  final String title;
  final String? id;
  final String? handle;

  @override
  ConsumerState<ReportSheet> createState() => _ReportSheetState();
}

class _ReportSheetState extends ConsumerState<ReportSheet> {
  static const _reasons = <(String, String)>[
    ('spam', 'Spam or advertising'),
    ('harassment', 'Harassment or bullying'),
    ('hate', 'Hate speech'),
    ('sexual', 'Sexual content'),
    ('violence', 'Violence or threats'),
    ('self-harm', 'Self-harm'),
    ('other', 'Something else'),
  ];

  String _reason = 'spam';
  final _details = TextEditingController();
  bool _sending = false;
  String? _error;

  @override
  void dispose() {
    _details.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_sending) return;
    setState(() {
      _sending = true;
      _error = null;
    });
    try {
      await ref
          .read(apiProvider)
          .report(
            entity: widget.entity,
            id: widget.id,
            handle: widget.handle,
            reason: _reason,
            details: _details.text,
          );
      if (!mounted) return;
      Navigator.of(context).pop();
      ScaffoldMessenger.maybeOf(context)
        ?..hideCurrentSnackBar()
        ..showSnackBar(
          const SnackBar(
            content: Text('Report sent. A moderator will review it.'),
          ),
        );
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _sending = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: EdgeInsets.only(
        left: 20,
        right: 20,
        bottom: MediaQuery.viewInsetsOf(context).bottom + 20,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(widget.title, style: theme.textTheme.titleLarge),
          const SizedBox(height: 4),
          Text(
            'Reports go to the people who run this server. We never tell the '
            'other account who reported them.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 8),
          RadioGroup<String>(
            groupValue: _reason,
            onChanged: (picked) {
              if (_sending) return;
              setState(() => _reason = picked ?? _reason);
            },
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final (value, label) in _reasons)
                  RadioListTile<String>(
                    value: value,
                    contentPadding: EdgeInsets.zero,
                    dense: true,
                    title: Text(label),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          TextField(
            controller: _details,
            maxLength: 500,
            minLines: 2,
            maxLines: 4,
            enabled: !_sending,
            decoration: const InputDecoration(
              labelText: 'Anything else we should know? (optional)',
              border: OutlineInputBorder(),
            ),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(bottom: 8),
              child: Text(
                _error!,
                style: TextStyle(color: theme.colorScheme.error),
              ),
            ),
          Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              TextButton(
                onPressed: _sending ? null : () => Navigator.of(context).pop(),
                child: const Text('Cancel'),
              ),
              const SizedBox(width: 8),
              FilledButton(
                onPressed: _sending ? null : _submit,
                child: _sending
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Soda.lime,
                        ),
                      )
                    : const Text('Report'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}
