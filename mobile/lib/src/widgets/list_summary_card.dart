import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import 'soda_format.dart';
import 'soda_ui.dart';

/// One list summary, shared by the Lists screen and the profile lists section.
/// [onEdit] and [onDelete] add an overflow menu; leave them null for lists the
/// viewer cannot change.
class ListSummaryCard extends StatelessWidget {
  const ListSummaryCard({
    super.key,
    required this.summary,
    required this.onTap,
    this.onEdit,
    this.onDelete,
  });

  final ListSummary summary;
  final VoidCallback onTap;
  final VoidCallback? onEdit;
  final VoidCallback? onDelete;

  bool get _hasMenu => onEdit != null || onDelete != null;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final visibility = sodaListVisibility(summary.visibility);
    final meta = [
      if (visibility.isNotEmpty) visibility,
      sodaCount(summary.itemCount, 'soda'),
    ].join(' · ');

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: EdgeInsets.fromLTRB(16, 12, _hasMenu ? 4 : 16, 12),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      summary.name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (summary.description.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        summary.description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                    const SizedBox(height: 6),
                    Text(
                      meta,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              if (summary.isSystem) ...[
                const SizedBox(width: 10),
                const SodaPill(label: 'Built in'),
              ],
              const SizedBox(width: 4),
              if (_hasMenu)
                PopupMenuButton<String>(
                  tooltip: 'Options for ${summary.name}',
                  icon: const Icon(Icons.more_horiz_rounded),
                  onSelected: (value) {
                    if (value == 'edit') onEdit?.call();
                    if (value == 'delete') onDelete?.call();
                  },
                  itemBuilder: (context) => [
                    if (onEdit != null)
                      const PopupMenuItem(value: 'edit', child: Text('Edit')),
                    if (onDelete != null)
                      const PopupMenuItem(
                        value: 'delete',
                        child: Text('Delete'),
                      ),
                  ],
                )
              else
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

const _visibilityHelp = {
  'private': 'Only you can see this list.',
  'friends': 'Your friends can see this list.',
  'public': 'Anyone can see this list.',
};

/// Opens the create or edit sheet. Pass [initial] to edit an existing list.
/// Returns the saved list, or null when the sheet is dismissed.
Future<ListSummary?> showListEditorSheet(
  BuildContext context, {
  ListSummary? initial,
}) {
  return showModalBottomSheet<ListSummary>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    backgroundColor: Theme.of(context).colorScheme.surface,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    builder: (_) => _ListEditorSheet(initial: initial),
  );
}

class _ListEditorSheet extends ConsumerStatefulWidget {
  const _ListEditorSheet({this.initial});

  final ListSummary? initial;

  @override
  ConsumerState<_ListEditorSheet> createState() => _ListEditorSheetState();
}

class _ListEditorSheetState extends ConsumerState<_ListEditorSheet> {
  late final TextEditingController _name = TextEditingController(
    text: widget.initial?.name ?? '',
  );
  late final TextEditingController _description = TextEditingController(
    text: widget.initial?.description ?? '',
  );
  late String _visibility = _knownVisibility(widget.initial?.visibility);
  bool _saving = false;
  String? _error;

  bool get _editing => widget.initial != null;

  static String _knownVisibility(String? value) =>
      const {'private', 'friends', 'public'}.contains(value)
      ? value!
      : 'private';

  @override
  void dispose() {
    _name.dispose();
    _description.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_saving) return;
    final name = _name.text.trim();
    final description = _description.text.trim();
    final nameLength = name.runes.length;
    if (nameLength == 0) {
      setState(() => _error = 'Give the list a name.');
      return;
    }
    if (nameLength > 80) {
      setState(() => _error = 'Keep the name to 80 characters or fewer.');
      return;
    }
    if (description.runes.length > 500) {
      setState(
        () => _error = 'Keep the description to 500 characters or fewer.',
      );
      return;
    }

    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final api = ref.read(apiProvider);
      final initial = widget.initial;
      final saved = initial == null
          ? await api.createList(
              name: name,
              description: description,
              visibility: _visibility,
            )
          : await api.updateList(
              initial.id,
              name: name,
              description: description,
              visibility: _visibility,
            );
      if (!mounted) return;
      Navigator.of(context).pop(saved);
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
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 10, 20, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Center(
              child: Container(
                width: 40,
                height: 4,
                decoration: BoxDecoration(
                  color: theme.colorScheme.onSurface.withValues(alpha: 0.16),
                  borderRadius: BorderRadius.circular(999),
                ),
              ),
            ),
            const SizedBox(height: 14),
            Row(
              children: [
                Expanded(
                  child: Text(
                    _editing ? 'Edit list' : 'New list',
                    style: theme.textTheme.titleLarge?.copyWith(
                      fontWeight: FontWeight.w800,
                      letterSpacing: -0.3,
                    ),
                  ),
                ),
                IconButton(
                  onPressed: () => Navigator.of(context).maybePop(),
                  tooltip: 'Close',
                  icon: const Icon(Icons.close_rounded),
                ),
              ],
            ),
            const SizedBox(height: 8),
            _label('Name'),
            TextField(
              key: const Key('list-name-field'),
              controller: _name,
              autofocus: !_editing,
              textCapitalization: TextCapitalization.sentences,
              textInputAction: TextInputAction.next,
              inputFormatters: [LengthLimitingTextInputFormatter(80)],
              decoration: const InputDecoration(hintText: 'Summer bottles'),
            ),
            const SizedBox(height: 16),
            _label('Description'),
            TextField(
              key: const Key('list-description-field'),
              controller: _description,
              minLines: 2,
              maxLines: 4,
              textCapitalization: TextCapitalization.sentences,
              inputFormatters: [LengthLimitingTextInputFormatter(500)],
              decoration: const InputDecoration(
                hintText: 'What belongs on this list',
              ),
            ),
            const SizedBox(height: 20),
            _label('Who can see it'),
            const SizedBox(height: 8),
            SodaSegmented<String>(
              options: const [
                ('private', 'Private'),
                ('friends', 'Friends'),
                ('public', 'Public'),
              ],
              selected: _visibility,
              onChanged: (value) => setState(() => _visibility = value),
            ),
            const SizedBox(height: 8),
            Text(
              _visibilityHelp[_visibility] ?? '',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 18),
              _errorBanner(_error!),
            ],
            const SizedBox(height: 22),
            FilledButton(
              key: const Key('list-save-button'),
              onPressed: _saving ? null : _submit,
              child: Text(
                _saving
                    ? 'Saving…'
                    : (_editing ? 'Save changes' : 'Create list'),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _label(String text) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Text(
        text,
        style: Theme.of(
          context,
        ).textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w700),
      ),
    );
  }

  Widget _errorBanner(String message) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.error.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.error_outline_rounded,
            size: 18,
            color: theme.colorScheme.error,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.error,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
