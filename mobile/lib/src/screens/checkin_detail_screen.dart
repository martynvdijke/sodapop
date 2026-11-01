import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:video_player/video_player.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import '../widgets/report_sheet.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';
import 'drink_detail_screen.dart';
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
  final _commentFocus = FocusNode();
  final _composerKey = GlobalKey();
  CheckinDetail? _detail;
  List<Comment> _comments = const [];
  bool _loading = true;
  bool _commentsLoading = true;
  bool _toastBusy = false;
  bool _saved = false;
  bool _saveBusy = false;
  bool _posting = false;
  bool _deletingComment = false;
  bool _deletingCheckin = false;
  bool _hasEarlier = false;
  bool _loadingEarlier = false;
  String _olderCursor = '';
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
    _commentFocus.dispose();
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
        _saved = detail.saved;
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
      final page = await _newestCommentsPage();
      if (!mounted) return;
      setState(() {
        _comments = page.items;
        _hasEarlier = page.hasMore;
        _olderCursor = page.nextBefore;
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

  /// Paging metadata is a nicety. When the paged read is unavailable the flat
  /// comment list still draws the newest page.
  Future<CommentPage> _newestCommentsPage() async {
    final api = ref.read(apiProvider);
    try {
      return await api.commentPage(widget.checkinId, limit: 50);
    } on ApiException {
      final items = await api.listComments(widget.checkinId);
      return CommentPage(items: items, count: items.length);
    }
  }

  /// Prepends the page before the oldest comment on screen.
  Future<void> _loadEarlier() async {
    if (_loadingEarlier || _olderCursor.isEmpty) return;
    setState(() => _loadingEarlier = true);
    try {
      final page = await ref
          .read(apiProvider)
          .commentPage(widget.checkinId, before: _olderCursor, limit: 50);
      if (!mounted) return;
      final seen = _comments.map((comment) => comment.id).toSet();
      final older = page.items
          .where((comment) => !seen.contains(comment.id))
          .toList();
      setState(() {
        _comments = [...older, ..._comments];
        _hasEarlier = page.hasMore;
        _olderCursor = page.nextBefore;
        _loadingEarlier = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _loadingEarlier = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
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

  /// Flips the bookmark straight away, then reverts if the write fails.
  Future<void> _toggleSave() async {
    final detail = _detail;
    if (detail == null || _saveBusy) return;
    final next = !_saved;
    setState(() {
      _saveBusy = true;
      _saved = next;
    });
    try {
      final api = ref.read(apiProvider);
      if (next) {
        await api.saveCheckin(detail.id);
      } else {
        await api.unsaveCheckin(detail.id);
      }
      if (!mounted) return;
      setState(() => _saveBusy = false);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saveBusy = false;
        _saved = !next;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ApiClient.describe(error).message)),
      );
    }
  }

  /// Prefills the composer with a mention of [comment]'s author.
  void _replyTo(Comment comment) {
    if (comment.handle.isEmpty) return;
    final text = '@${comment.handle} ';
    _comment.value = TextEditingValue(
      text: text,
      selection: TextSelection.collapsed(offset: text.length),
    );
    _commentFocus.requestFocus();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      final composer = _composerKey.currentContext;
      if (composer == null || !mounted) return;
      Scrollable.ensureVisible(
        composer,
        duration: const Duration(milliseconds: 200),
        alignmentPolicy: ScrollPositionAlignmentPolicy.keepVisibleAtEnd,
      );
    });
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

  Future<void> _reportCheckin() async {
    final detail = _detail;
    if (detail == null) return;
    await showReportSheet(
      context,
      entity: 'checkin',
      id: detail.id,
      title: 'Report this check-in?',
    );
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
          else
            PopupMenuButton<String>(
              tooltip: 'Check-in options',
              onSelected: (value) {
                if (value == 'delete') _deleteCheckin();
                if (value == 'report') _reportCheckin();
              },
              itemBuilder: (context) => [
                if (detail?.canDelete == true)
                  const PopupMenuItem(
                    value: 'delete',
                    child: Text('Delete check-in'),
                  ),
                if (detail != null)
                  const PopupMenuItem(
                    value: 'report',
                    child: Text('Report check-in'),
                  ),
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
          if (!_commentsLoading && _commentsError == null && _hasEarlier)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: _loadingEarlier ? null : _loadEarlier,
                icon: _loadingEarlier
                    ? const SizedBox(
                        width: 14,
                        height: 14,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Soda.lime,
                        ),
                      )
                    : const Icon(Icons.history_rounded, size: 18),
                label: const Text('Show earlier comments'),
              ),
            ),
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
            InkWell(
              onTap: detail.drinkSlug.isEmpty
                  ? null
                  : () => DrinkDetailScreen.open(
                      context,
                      detail.drinkSlug,
                      name: detail.drinkName,
                    ),
              borderRadius: BorderRadius.circular(10),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 4),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Expanded(
                          child: Text(
                            detail.drinkName,
                            style: theme.textTheme.headlineSmall?.copyWith(
                              fontWeight: FontWeight.w800,
                            ),
                          ),
                        ),
                        if (detail.drinkSlug.isNotEmpty)
                          Padding(
                            padding: const EdgeInsets.only(left: 6, top: 4),
                            child: Icon(
                              Icons.chevron_right_rounded,
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                      ],
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
                  ],
                ),
              ),
            ),
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
                Expanded(
                  child: Text(
                    _toasts == 0
                        ? 'No toasts yet'
                        : sodaCount(_toasts, 'toast'),
                    maxLines: 2,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
                Semantics(
                  container: true,
                  toggled: _saved,
                  child: Tooltip(
                    message: _saved ? 'Saved' : 'Save',
                    child: TextButton.icon(
                      onPressed: _saveBusy ? null : _toggleSave,
                      icon: Icon(
                        _saved
                            ? Icons.bookmark_rounded
                            : Icons.bookmark_border_rounded,
                        size: 18,
                      ),
                      label: Text(_saved ? 'Saved' : 'Save'),
                    ),
                  ),
                ),
                const SizedBox(width: 4),
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
    final api = ref.read(apiProvider);
    if (!api.configured || detail.photoId.isEmpty) {
      return const SizedBox.shrink();
    }
    if (detail.photoMime.startsWith('video/')) {
      return _CheckinVideo(
        url: api.mediaUrl(detail.photoId),
        headers: api.imageHeaders,
        label: 'Video of ${detail.drinkName}',
      );
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
                MentionText(
                  body: comment.body,
                  onMention: _openAuthor,
                  style: theme.textTheme.bodyMedium,
                ),
                // Every comment gets an action row so the report path is
                // reachable even when the viewer cannot reply to it.
                Row(
                  children: [
                    if (!comment.canDelete && _detail?.canComment == true)
                      TextButton(
                        onPressed: () => _replyTo(comment),
                        child: const Text('Reply'),
                      ),
                    const Spacer(),
                    if (!comment.canDelete)
                      TextButton(
                        onPressed: () => showReportSheet(
                          context,
                          entity: 'comment',
                          id: comment.id,
                          title: 'Report this comment?',
                        ),
                        child: const Text('Report'),
                      ),
                    if (comment.canDelete)
                      TextButton(
                        onPressed: _deletingComment
                            ? null
                            : () => _deleteComment(comment),
                        child: const Text('Delete'),
                      ),
                  ],
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
      key: _composerKey,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextField(
          controller: _comment,
          focusNode: _commentFocus,
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

/// A check-in video, played natively inside a rounded 16:9 card. Playback is
/// paused whenever the app leaves the foreground and always on dispose.
class _CheckinVideo extends StatefulWidget {
  const _CheckinVideo({
    required this.url,
    required this.headers,
    required this.label,
  });

  final String url;
  final Map<String, String> headers;
  final String label;

  @override
  State<_CheckinVideo> createState() => _CheckinVideoState();
}

class _CheckinVideoState extends State<_CheckinVideo>
    with WidgetsBindingObserver {
  VideoPlayerController? _controller;
  bool _ready = false;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _start();
  }

  Future<void> _start() async {
    final controller = VideoPlayerController.networkUrl(
      Uri.parse(widget.url),
      httpHeaders: widget.headers,
    );
    _controller = controller;
    try {
      await controller.initialize();
      if (!mounted) return;
      setState(() => _ready = true);
    } catch (_) {
      if (!mounted) return;
      setState(() => _failed = true);
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      _controller?.pause();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    final controller = _controller;
    _controller = null;
    controller?.pause();
    controller?.dispose();
    super.dispose();
  }

  void _toggle() {
    final controller = _controller;
    if (controller == null || !_ready) return;
    if (controller.value.isPlaying) {
      controller.pause();
    } else {
      controller.play();
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final controller = _controller;
    return Semantics(
      container: true,
      label: widget.label,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(16),
        child: AspectRatio(
          aspectRatio: 16 / 9,
          child: ColoredBox(
            color: Soda.night,
            child: _failed
                ? _message(
                    theme,
                    Icons.videocam_off_outlined,
                    'This video could not play.',
                  )
                : !_ready || controller == null
                ? _message(theme, null, 'Loading video…', loading: true)
                : ValueListenableBuilder<VideoPlayerValue>(
                    valueListenable: controller,
                    builder: (context, value, _) {
                      return Stack(
                        fit: StackFit.expand,
                        children: [
                          VideoPlayer(controller),
                          if (!value.isPlaying)
                            Center(
                              child: IconButton.filled(
                                onPressed: _toggle,
                                tooltip: 'Play video',
                                style: IconButton.styleFrom(
                                  backgroundColor: theme.colorScheme.primary,
                                  foregroundColor: theme.colorScheme.onPrimary,
                                  minimumSize: const Size.square(56),
                                ),
                                icon: const Icon(
                                  Icons.play_arrow_rounded,
                                  size: 32,
                                ),
                              ),
                            ),
                          Positioned(
                            left: 0,
                            right: 0,
                            bottom: 0,
                            child: Container(
                              decoration: BoxDecoration(
                                gradient: LinearGradient(
                                  begin: Alignment.bottomCenter,
                                  end: Alignment.topCenter,
                                  colors: [
                                    Soda.night.withValues(alpha: 0.72),
                                    Colors.transparent,
                                  ],
                                ),
                              ),
                              child: Row(
                                children: [
                                  IconButton(
                                    onPressed: _toggle,
                                    tooltip: value.isPlaying
                                        ? 'Pause video'
                                        : 'Play video',
                                    color: Soda.cream,
                                    icon: Icon(
                                      value.isPlaying
                                          ? Icons.pause_rounded
                                          : Icons.play_arrow_rounded,
                                    ),
                                  ),
                                  Expanded(
                                    child: VideoProgressIndicator(
                                      controller,
                                      allowScrubbing: true,
                                      padding: const EdgeInsets.symmetric(
                                        horizontal: 4,
                                        vertical: 12,
                                      ),
                                      colors: const VideoProgressColors(
                                        playedColor: Soda.lime,
                                        bufferedColor: Colors.white24,
                                        backgroundColor: Colors.white12,
                                      ),
                                    ),
                                  ),
                                  const SizedBox(width: 10),
                                ],
                              ),
                            ),
                          ),
                        ],
                      );
                    },
                  ),
          ),
        ),
      ),
    );
  }

  Widget _message(
    ThemeData theme,
    IconData? icon,
    String text, {
    bool loading = false,
  }) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (loading)
            const SizedBox(
              width: 26,
              height: 26,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: Soda.lime,
              ),
            )
          else if (icon != null)
            Icon(icon, color: Soda.cream, size: 28),
          const SizedBox(height: 10),
          Text(
            text,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall?.copyWith(
              color: Soda.cream.withValues(alpha: 0.8),
            ),
          ),
        ],
      ),
    );
  }
}

/// Comment or note text with tappable `@handle` links.
///
/// The API sends plain text only, so nothing here parses markup: mentions are
/// matched with a narrow pattern and everything else stays literal. The
/// recognisers live with the state and are disposed with it.
class MentionText extends StatefulWidget {
  const MentionText({
    super.key,
    required this.body,
    required this.onMention,
    this.style,
  });

  final String body;
  final ValueChanged<String> onMention;
  final TextStyle? style;

  @override
  State<MentionText> createState() => _MentionTextState();
}

class _MentionTextState extends State<MentionText> {
  static final _pattern = RegExp(r'@([a-z0-9_]{3,24})', caseSensitive: false);
  static const _mentionStyle = TextStyle(
    color: Soda.tangerine,
    fontWeight: FontWeight.w700,
    decoration: TextDecoration.underline,
    decorationColor: Soda.tangerine,
  );

  final List<TapGestureRecognizer> _recognizers = [];
  List<InlineSpan> _spans = const [];

  @override
  void initState() {
    super.initState();
    _buildSpans();
  }

  @override
  void didUpdateWidget(covariant MentionText oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.body != widget.body ||
        oldWidget.onMention != widget.onMention) {
      _buildSpans();
    }
  }

  @override
  void dispose() {
    _clearRecognizers();
    super.dispose();
  }

  void _clearRecognizers() {
    for (final recognizer in _recognizers) {
      recognizer.dispose();
    }
    _recognizers.clear();
  }

  void _buildSpans() {
    _clearRecognizers();
    final text = widget.body;
    final spans = <InlineSpan>[];
    var index = 0;
    for (final match in _pattern.allMatches(text)) {
      if (match.start > index) {
        spans.add(TextSpan(text: text.substring(index, match.start)));
      }
      final handle = (match.group(1) ?? '').toLowerCase();
      final recognizer = TapGestureRecognizer()
        ..onTap = () => widget.onMention(handle);
      _recognizers.add(recognizer);
      spans.add(
        TextSpan(
          text: match.group(0),
          style: _mentionStyle,
          recognizer: recognizer,
        ),
      );
      index = match.end;
    }
    if (index < text.length) {
      spans.add(TextSpan(text: text.substring(index)));
    }
    _spans = spans;
  }

  @override
  Widget build(BuildContext context) {
    return Text.rich(TextSpan(children: _spans), style: widget.style);
  }
}
