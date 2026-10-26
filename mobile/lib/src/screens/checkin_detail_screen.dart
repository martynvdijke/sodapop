import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'user_profile_screen.dart';

/// One check-in with its photo, note, toasts and comments.
class CheckinDetailScreen extends ConsumerStatefulWidget {
  const CheckinDetailScreen({super.key, required this.checkinId});

  final String checkinId;

  static Future<void> open(BuildContext context, String checkinId) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => CheckinDetailScreen(checkinId: checkinId),
      ),
    );
  }

  @override
  ConsumerState<CheckinDetailScreen> createState() =>
      _CheckinDetailScreenState();
}

class _CheckinDetailScreenState extends ConsumerState<CheckinDetailScreen> {
  final _comment = TextEditingController();
  CheckinDetail? _detail;
  List<Comment> _comments = const [];
  bool _loading = true;
  bool _commentsLoading = true;
  bool _toastBusy = false;
  bool _posting = false;
  bool _deletingComment = false;
  bool _deletingCheckin = false;
  int _toasts = 0;
  bool _toasted = false;
  String? _error;
  String? _commentsError;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _comment.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
      _commentsLoading = true;
      _commentsError = null;
    });
    try {
      final detail = await ref.read(apiProvider).getCheckin(widget.checkinId);
      if (!mounted) return;
      setState(() {
        _detail = detail;
        _toasts = detail.toasts;
        _toasted = detail.toasted;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = ApiClient.describe(error).message;
      });
      return;
    }
    await _loadComments();
  }

  Future<void> _loadComments() async {
    setState(() {
      _commentsLoading = true;
      _commentsError = null;
    });
    try {
      final comments = await ref
          .read(apiProvider)
          .listComments(widget.checkinId);
      if (!mounted) return;
      setState(() {
        _comments = comments;
        _commentsLoading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _commentsLoading = false;
        _commentsError = ApiClient.describe(error).message;
      });
    }
  }

  Future<void> _toggleToast() async {
    final detail = _detail;
    if (detail == null || _toastBusy) return;
    setState(() => _toastBusy = true);
    try {
      if (_toasted) {
        await ref.read(apiProvider).untoast(detail.id);
        if (!mounted) return;
        setState(() {
          _toasted = false;
          _toasts = _toasts > 0 ? _toasts - 1 : 0;
          _toastBusy = false;
        });
      } else {
        await ref.read(apiProvider).toast(detail.id);
        if (!mounted) return;
        setState(() {
          _toasted = true;
          _toasts += 1;
          _toastBusy = false;
        });
      }
    } catch (error) {
      if (!mounted) return;
      setState(() => _toastBusy = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _postComment() async {
    final detail = _detail;
    final body = _comment.text.trim();
    if (detail == null || body.isEmpty || _posting) return;
    setState(() => _posting = true);
    try {
      final created = await ref
          .read(apiProvider)
          .createComment(detail.id, body);
      if (!mounted) return;
      setState(() {
        _comments = [..._comments, created];
        _posting = false;
      });
      _comment.clear();
      FocusManager.instance.primaryFocus?.unfocus();
    } catch (error) {
      if (!mounted) return;
      setState(() => _posting = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  Future<void> _deleteComment(Comment comment) async {
    if (_deletingComment) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete comment?'),
        content: const Text('This removes the comment for everyone.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _deletingComment = true);
    try {
      await ref.read(apiProvider).deleteComment(comment.id);
      if (!mounted) return;
      setState(() {
        _comments = _comments.where((row) => row.id != comment.id).toList();
        _deletingComment = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _deletingComment = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  void _openAuthor(String handle) {
    if (handle.isEmpty) return;
    UserProfileScreen.open(context, handle);
  }

  Future<void> _deleteCheckin() async {
    final detail = _detail;
    if (detail == null || !detail.canDelete || _deletingCheckin) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete this check-in?'),
        content: const Text(
          'This removes the photo, note and rating for everyone. It cannot be undone.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _deletingCheckin = true);
    try {
      await ref.read(apiProvider).deleteCheckin(detail.id);
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() => _deletingCheckin = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final detail = _detail;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Check-in'),
        actions: [
          if (_deletingCheckin)
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
            )
          else if (detail?.canDelete == true)
            PopupMenuButton<String>(
              tooltip: 'Check-in options',
              onSelected: (value) {
                if (value == 'delete') _deleteCheckin();
              },
              itemBuilder: (context) => const [
                PopupMenuItem(value: 'delete', child: Text('Delete check-in')),
              ],
            ),
        ],
      ),
      body: _body(detail),
    );
  }

  Widget _body(CheckinDetail? detail) {
    if (_loading) return const SodaLoadingView(label: 'Loading check-in…');
    if (_error != null || detail == null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load this check-in',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }
    return RefreshIndicator(
      color: Soda.lime,
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: [
          _detailCard(detail),
          const SizedBox(height: 26),
          SodaSectionTitle(
            title: 'Comments',
            trailing: _commentsError != null
                ? (detail.commentsCount > 0
                      ? sodaCount(detail.commentsCount, 'comment')
                      : null)
                : (_comments.isEmpty
                      ? null
                      : sodaCount(_comments.length, 'comment')),
          ),
          const SizedBox(height: 6),
          if (_commentsLoading)
            const SodaLoadingView(dense: true)
          else if (_commentsError != null)
            SodaStatusView(
              dense: true,
              icon: Icons.cloud_off_rounded,
              title: 'Comments did not load',
              message: _commentsError,
              actionLabel: 'Try again',
              onAction: _loadComments,
            )
          else if (_comments.isEmpty)
            const SodaStatusView(
              dense: true,
              icon: Icons.chat_bubble_outline_rounded,
              title: 'No comments yet',
              message: 'Be the first to say something.',
            )
          else
            for (final comment in _comments) _commentTile(comment),
          if (detail.canComment) ...[const SizedBox(height: 18), _composer()],
        ],
      ),
    );
  }

  Widget _detailCard(CheckinDetail detail) {
    final theme = Theme.of(context);
    final visibleRating = detail.rating != null && detail.rating! > 0;
    final quarters = (detail.rating ?? 0).round();
    final visibility = sodaVisibility(detail.visibility);
    final canToast = detail.canToast || _toasted;
    final authorName = detail.displayName.isEmpty
        ? '@${detail.handle}'
        : detail.displayName;
    final date = sodaDate(detail.createdAt);

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                SodaAvatar(
                  initial: detail.initial.isEmpty
                      ? (detail.displayName.isEmpty
                            ? detail.handle
                            : detail.displayName)
                      : detail.initial,
                  size: 40,
                  onTap: detail.handle.isEmpty
                      ? null
                      : () => _openAuthor(detail.handle),
                  semanticLabel: 'Open @${detail.handle} profile',
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: InkWell(
                    onTap: detail.handle.isEmpty
                        ? null
                        : () => _openAuthor(detail.handle),
                    borderRadius: BorderRadius.circular(8),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(vertical: 4),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            authorName,
                            style: theme.textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          if (detail.handle.isNotEmpty)
                            Text(
                              '@${detail.handle}',
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: theme.colorScheme.onSurfaceVariant,
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
                if (date.isNotEmpty)
                  Text(
                    date,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
              ],
            ),
            if (detail.photoId.isNotEmpty) ...[
              const SizedBox(height: 16),
              _photo(detail),
            ],
            const SizedBox(height: 18),
            Text(
              detail.drinkName,
              style: theme.textTheme.headlineSmall?.copyWith(
                fontWeight: FontWeight.w800,
              ),
            ),
            if (detail.brand.isNotEmpty || detail.style.isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                [
                  detail.brand,
                  detail.style,
                ].where((v) => v.isNotEmpty).join(' · '),
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            if (visibility.isNotEmpty || detail.venueName.isNotEmpty) ...[
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  if (visibility.isNotEmpty) SodaPill(label: visibility),
                  if (detail.venueName.isNotEmpty)
                    SodaPill(
                      icon: Icons.place_outlined,
                      label: detail.venueName,
                    ),
                ],
              ),
            ],
            const SizedBox(height: 16),
            if (visibleRating)
              Row(
                children: [
                  SodaRating(quarters: quarters),
                  const SizedBox(width: 10),
                  Text(
                    ratingLabel(quarters),
                    style: theme.textTheme.titleMedium?.copyWith(
                      color: Soda.tangerine,
                      fontWeight: FontWeight.w800,
                      fontFeatures: const [FontFeature.tabularFigures()],
                    ),
                  ),
                ],
              )
            else
              Text(
                'Not rated',
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            if (detail.note.isNotEmpty) ...[
              const SizedBox(height: 14),
              Text(detail.note, style: theme.textTheme.bodyLarge),
            ],
            const SizedBox(height: 16),
            Divider(
              color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
              height: 1,
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                Text(
                  _toasts == 0 ? 'No toasts yet' : sodaCount(_toasts, 'toast'),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
                const Spacer(),
                if (canToast)
                  Tooltip(
                    message: _toasted
                        ? 'Remove your toast'
                        : 'Toast this check-in',
                    child: TextButton.icon(
                      onPressed: _toastBusy ? null : _toggleToast,
                      icon: Icon(
                        _toasted
                            ? Icons.check_circle_rounded
                            : Icons.celebration_outlined,
                        size: 18,
                      ),
                      label: Text(_toasted ? 'Toasted' : 'Toast'),
                    ),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _photo(CheckinDetail detail) {
    final theme = Theme.of(context);
    if (detail.photoMime.startsWith('video/')) {
      return Container(
        width: double.infinity,
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
          borderRadius: BorderRadius.circular(12),
        ),
        child: Row(
          children: [
            Icon(
              Icons.videocam_outlined,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                'This check-in has a video. Open it on the web app to play it.',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ),
          ],
        ),
      );
    }

    final api = ref.read(apiProvider);
    if (!api.configured || detail.photoId.isEmpty) {
      return const SizedBox.shrink();
    }
    final url = api.mediaUrl(detail.photoId);
    return ClipRRect(
      borderRadius: BorderRadius.circular(12),
      child: AspectRatio(
        aspectRatio: 4 / 3,
        child: Image.network(
          url,
          headers: api.imageHeaders,
          fit: BoxFit.cover,
          semanticLabel: 'Photo of ${detail.drinkName}',
          errorBuilder: (context, error, stackTrace) => Container(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
            alignment: Alignment.center,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  Icons.image_not_supported_outlined,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
                const SizedBox(height: 6),
                Text(
                  'Photo unavailable',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _commentTile(Comment comment) {
    final theme = Theme.of(context);
    final name = comment.displayName.isEmpty
        ? '@${comment.handle}'
        : comment.displayName;
    final date = sodaDate(comment.createdAt);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SodaAvatar(
            initial: comment.initial.isEmpty
                ? (comment.displayName.isEmpty
                      ? comment.handle
                      : comment.displayName)
                : comment.initial,
            size: 32,
            onTap: comment.handle.isEmpty
                ? null
                : () => _openAuthor(comment.handle),
            semanticLabel: 'Open @${comment.handle} profile',
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Text(
                        name,
                        style: theme.textTheme.titleSmall?.copyWith(
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                    if (date.isNotEmpty)
                      Text(
                        date,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                  ],
                ),
                const SizedBox(height: 3),
                Text(comment.body, style: theme.textTheme.bodyMedium),
                if (comment.canDelete)
                  Align(
                    alignment: Alignment.centerRight,
                    child: TextButton(
                      onPressed: _deletingComment
                          ? null
                          : () => _deleteComment(comment),
                      child: const Text('Delete'),
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _composer() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextField(
          controller: _comment,
          enabled: !_posting,
          minLines: 1,
          maxLines: 4,
          textCapitalization: TextCapitalization.sentences,
          inputFormatters: [LengthLimitingTextInputFormatter(1000)],
          decoration: const InputDecoration(hintText: 'Add a comment'),
        ),
        const SizedBox(height: 8),
        Align(
          alignment: Alignment.centerRight,
          child: FilledButton.icon(
            onPressed: _posting ? null : _postComment,
            icon: const Icon(Icons.send_rounded, size: 18),
            label: Text(_posting ? 'Posting…' : 'Post'),
          ),
        ),
      ],
    );
  }
}
