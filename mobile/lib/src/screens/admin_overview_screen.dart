import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_format.dart';
import '../widgets/soda_ui.dart';

/// A read-only look at what happened on this server, for admins and
/// moderators. Nothing here changes data: tapping a row only shows the event
/// and its web link.
class AdminOverviewScreen extends ConsumerStatefulWidget {
  const AdminOverviewScreen({super.key});

  /// Matches the API default. The cursor decides whether there is more.
  static const pageSize = 50;

  static Future<void> open(BuildContext context) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => const AdminOverviewScreen()),
    );
  }

  @override
  ConsumerState<AdminOverviewScreen> createState() =>
      _AdminOverviewScreenState();
}

class _AdminOverviewScreenState extends ConsumerState<AdminOverviewScreen> {
  /// The filter row, in display order. An empty value asks for every kind.
  static const _filters = <(String, String)>[
    ('', 'All'),
    ('checkin', 'Check-ins'),
    ('drink', 'Sodas'),
    ('user', 'Members'),
    ('venue', 'Venues'),
    ('report', 'Reports'),
    ('comment', 'Comments'),
    ('toast', 'Toasts'),
  ];

  final _scroll = ScrollController();

  List<AdminOverviewEvent> _events = const [];
  AdminOverviewCounts? _counts;
  String _kind = '';
  String _nextBefore = '';
  bool _loading = true;
  bool _loadingOlder = false;
  bool _forbidden = false;
  String? _error;

  List<String> get _kinds => _kind.isEmpty ? const [] : [_kind];

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    _load();
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (!_scroll.hasClients) return;
    final position = _scroll.position;
    if (position.pixels >= position.maxScrollExtent - 320) _loadOlder();
  }

  /// Loads the first page for the current filter and refreshes the counters.
  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
      _forbidden = false;
    });
    await _loadCounts();
    try {
      final page = await ref
          .read(apiProvider)
          .adminOverview(limit: AdminOverviewScreen.pageSize, kinds: _kinds);
      if (!mounted) return;
      setState(() {
        _events = page.items;
        _nextBefore = page.nextBefore;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      final described = ApiClient.describe(error);
      setState(() {
        _loading = false;
        _forbidden = described.statusCode == 404;
        _error = described.message;
      });
    }
  }

  /// Counters are a convenience: if they fail the feed still works, so the
  /// header simply loses the tile card.
  Future<void> _loadCounts() async {
    try {
      final counts = await ref.read(apiProvider).adminOverviewCounts();
      if (!mounted) return;
      setState(() => _counts = counts);
    } catch (_) {
      if (!mounted) return;
      setState(() => _counts = null);
    }
  }

  /// Appends the page before the oldest row on screen.
  Future<void> _loadOlder() async {
    if (_loading || _loadingOlder || _nextBefore.isEmpty) return;
    setState(() => _loadingOlder = true);
    try {
      final page = await ref
          .read(apiProvider)
          .adminOverview(
            before: _nextBefore,
            limit: AdminOverviewScreen.pageSize,
            kinds: _kinds,
          );
      if (!mounted) return;
      final seen = _events.map((event) => event.id).toSet();
      final fresh = page.items
          .where((event) => !seen.contains(event.id))
          .toList();
      setState(() {
        _events = [..._events, ...fresh];
        _nextBefore = page.nextBefore;
        _loadingOlder = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _loadingOlder = false);
      ScaffoldMessenger.maybeOf(context)
        ?..hideCurrentSnackBar()
        ..showSnackBar(
          SnackBar(content: Text(ApiClient.describe(error).message)),
        );
    }
  }

  Future<void> _selectKind(String kind) async {
    if (_kind == kind) return;
    setState(() {
      _kind = kind;
      _events = const [];
      _nextBefore = '';
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    await _load();
  }

  Future<void> _showEvent(AdminOverviewEvent event) async {
    final server = ref.read(apiProvider).server;
    final base = (server ?? '').replaceFirst(RegExp(r'/+$'), '');
    // A relative path is not something the member can paste elsewhere, so the
    // link is only offered when the server origin is known.
    final url = base.isEmpty || event.href.isEmpty ? '' : '$base${event.href}';
    final copied = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => _EventSheet(event: event, url: url),
    );
    if (copied != true || !mounted) return;
    ScaffoldMessenger.maybeOf(context)
      ?..hideCurrentSnackBar()
      ..showSnackBar(const SnackBar(content: Text('Link copied.')));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Admin overview')),
      body: SafeArea(
        top: false,
        child: RefreshIndicator(
          color: Soda.lime,
          onRefresh: _load,
          child: _body(),
        ),
      ),
    );
  }

  Widget _body() {
    if (_forbidden) {
      return SodaStatusView(
        icon: Icons.lock_outline_rounded,
        title: 'Admin access required',
        message: 'This account cannot see moderation activity.',
        actionLabel: 'Back',
        onAction: () => Navigator.of(context).maybePop(),
      );
    }
    if (_loading) {
      return const SodaLoadingView(label: 'Loading activity…');
    }
    if (_error != null) {
      return SodaStatusView(
        icon: Icons.cloud_off_rounded,
        title: 'Could not load activity',
        message: _error,
        actionLabel: 'Try again',
        onAction: _load,
      );
    }

    final empty = _events.isEmpty;
    return ListView.separated(
      controller: _scroll,
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
      itemCount: empty ? 2 : _events.length + 2,
      separatorBuilder: (_, index) => SizedBox(height: index == 0 ? 16 : 10),
      itemBuilder: (context, index) {
        if (index == 0) return _header();
        if (empty) return _emptyState();
        final position = index - 1;
        if (position >= _events.length) return _footer();
        return _eventCard(_events[position]);
      },
    );
  }

  // ------------------------------------------------------------------ header

  Widget _header() {
    final theme = Theme.of(context);
    final hint = theme.textTheme.bodySmall?.copyWith(
      color: theme.colorScheme.onSurfaceVariant,
    );
    final counts = _counts;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SodaSectionTitle(title: 'At a glance'),
        const SizedBox(height: 10),
        if (counts == null)
          Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Text('Counts are unavailable right now.', style: hint),
            ),
          )
        else
          _countsCard(counts),
        const SizedBox(height: 18),
        _filterRow(),
        const SizedBox(height: 18),
        SodaSectionTitle(
          title: 'Activity',
          trailing: _events.isEmpty ? null : 'Newest first',
        ),
      ],
    );
  }

  Widget _countsCard(AdminOverviewCounts counts) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Column(
          children: [
            _countRow([
              (counts.checkins24h, 'Check-ins', '24 h', false),
              (counts.drinks24h, 'New sodas', '24 h', false),
              (counts.users7d, 'New members', '7 d', false),
            ]),
            const Divider(height: 1, indent: 12, endIndent: 12),
            _countRow([
              (counts.openReports, 'Open reports', '', true),
              (counts.pendingDrinks, 'Pending sodas', '', true),
            ]),
          ],
        ),
      ),
    );
  }

  Widget _countRow(List<(int, String, String, bool)> cells) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final (value, label, window, attention) in cells)
          _countCell(value, label, window, attention),
      ],
    );
  }

  Widget _countCell(int value, String label, String window, bool attention) {
    final theme = Theme.of(context);
    final waiting = attention && value > 0;
    return Expanded(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 12, 12, 10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.baseline,
              textBaseline: TextBaseline.alphabetic,
              children: [
                Text(
                  '$value',
                  style: theme.textTheme.titleLarge?.copyWith(
                    fontWeight: FontWeight.w800,
                    fontFeatures: const [FontFeature.tabularFigures()],
                    color: waiting ? Soda.tangerine : null,
                  ),
                ),
                if (window.isNotEmpty) ...[
                  const SizedBox(width: 4),
                  Text(
                    window,
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ],
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
      ),
    );
  }

  Widget _filterRow() {
    final theme = Theme.of(context);
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      physics: const BouncingScrollPhysics(),
      child: Row(
        children: [
          for (var index = 0; index < _filters.length; index++)
            Padding(
              padding: EdgeInsets.only(
                right: index == _filters.length - 1 ? 0 : 8,
              ),
              child: _filterChip(theme, _filters[index].$1, _filters[index].$2),
            ),
        ],
      ),
    );
  }

  Widget _filterChip(ThemeData theme, String value, String label) {
    final selected = _kind == value;
    return ChoiceChip(
      key: Key('admin-filter-${value.isEmpty ? 'all' : value}'),
      label: Text(label),
      selected: selected,
      showCheckmark: false,
      selectedColor: theme.colorScheme.primary,
      side: BorderSide(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.16),
      ),
      labelStyle: theme.textTheme.labelLarge?.copyWith(
        fontWeight: FontWeight.w600,
        color: selected
            ? theme.colorScheme.onPrimary
            : theme.colorScheme.onSurfaceVariant,
      ),
      onSelected: (_) => _selectKind(value),
    );
  }

  // -------------------------------------------------------------------- rows

  Widget _eventCard(AdminOverviewEvent event) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final metaStyle = theme.textTheme.bodySmall?.copyWith(
      color: scheme.onSurfaceVariant,
    );
    final actor = event.actorHandle.isEmpty
        ? 'system'
        : '@${event.actorHandle}';
    final visibility = sodaVisibility(event.visibility);

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: event.href.isEmpty ? null : () => _showEvent(event),
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  _kindPill(event.kind),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      actor,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: metaStyle?.copyWith(fontWeight: FontWeight.w600),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Text(_relative(event.createdAt), style: metaStyle),
                ],
              ),
              const SizedBox(height: 8),
              Text(
                event.title.isEmpty ? 'Activity' : event.title,
                style: theme.textTheme.titleSmall?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              if (event.detail.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(
                  event.detail,
                  maxLines: 3,
                  overflow: TextOverflow.ellipsis,
                  style: metaStyle,
                ),
              ],
              if (event.status.isNotEmpty || visibility.isNotEmpty) ...[
                const SizedBox(height: 10),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: [
                    if (event.status.isNotEmpty)
                      SodaPill(
                        label: _statusLabel(event.status),
                        icon: Icons.info_outline_rounded,
                      ),
                    if (visibility.isNotEmpty)
                      SodaPill(
                        label: visibility,
                        icon: Icons.visibility_outlined,
                      ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  Widget _kindPill(String kind) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final (label, icon) = switch (kind) {
      'checkin' => ('Check-in', Icons.local_drink_outlined),
      'drink' => ('Soda', Icons.add_circle_outline_rounded),
      'user' => ('Member', Icons.person_outline_rounded),
      'venue' => ('Venue', Icons.place_outlined),
      'report' => ('Report', Icons.flag_outlined),
      'comment' => ('Comment', Icons.chat_bubble_outline_rounded),
      'toast' => ('Toast', Icons.favorite_border_rounded),
      _ => (kind.isEmpty ? 'Event' : kind, Icons.bolt_outlined),
    };
    final positive = kind == 'checkin' || kind == 'toast';
    final attention = kind == 'report';
    return SodaPill(
      label: label,
      icon: icon,
      background: positive
          ? scheme.primary.withValues(alpha: 0.30)
          : attention
          ? Soda.tangerine.withValues(alpha: 0.14)
          : null,
      foreground: attention
          ? Soda.tangerine
          : positive
          ? scheme.onSurface
          : null,
    );
  }

  Widget _emptyState() {
    final filtered = _kind.isNotEmpty;
    return SodaStatusView(
      dense: true,
      icon: Icons.receipt_long_outlined,
      title: filtered ? 'No activity for this filter' : 'No activity yet',
      message: filtered
          ? 'Nothing of this kind has happened on this server.'
          : 'Check-ins, new sodas and reports will appear here.',
      actionLabel: filtered ? 'Show everything' : null,
      onAction: filtered ? () => _selectKind('') : null,
    );
  }

  Widget _footer() {
    if (_nextBefore.isEmpty) return const SizedBox.shrink();
    return Center(
      child: TextButton.icon(
        key: const Key('admin-load-older'),
        onPressed: _loadingOlder ? null : _loadOlder,
        icon: _loadingOlder
            ? const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: Soda.lime,
                ),
              )
            : const Icon(Icons.history_rounded, size: 18),
        label: Text(_loadingOlder ? 'Loading…' : 'Load older activity'),
      ),
    );
  }

  // ----------------------------------------------------------------- wording

  /// "just now", "12 min ago", "3 h ago", "yesterday", else a short date.
  static String _relative(String raw) {
    final value = DateTime.tryParse(raw.trim())?.toLocal();
    if (value == null) return raw;
    final now = DateTime.now();
    final difference = now.difference(value);
    if (difference.inSeconds < 60) return 'just now';
    if (difference.inMinutes < 60) return '${difference.inMinutes} min ago';
    if (difference.inHours < 24) return '${difference.inHours} h ago';
    final today = DateTime(now.year, now.month, now.day);
    final day = DateTime(value.year, value.month, value.day);
    if (today.difference(day).inDays == 1) return 'yesterday';
    return sodaDate(raw);
  }

  static String _statusLabel(String status) {
    switch (status.toLowerCase()) {
      case 'pending':
        return 'Pending review';
      case 'open':
        return 'Open';
      case 'removed':
        return 'Removed';
      case 'approved':
        return 'Approved';
      default:
        return status;
    }
  }
}

/// The read-only summary behind a tapped row. Copying closes the sheet and
/// lets the screen show the confirmation, because a snack bar sits under the
/// sheet's barrier and would not be visible.
class _EventSheet extends StatelessWidget {
  const _EventSheet({required this.event, required this.url});

  final AdminOverviewEvent event;
  final String url;

  Future<void> _copy(BuildContext context) async {
    await Clipboard.setData(ClipboardData(text: url));
    if (!context.mounted) return;
    Navigator.of(context).pop(true);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final metaStyle = theme.textTheme.bodySmall?.copyWith(
      color: scheme.onSurfaceVariant,
    );
    final actor = event.actorName.isEmpty
        ? (event.actorHandle.isEmpty ? 'system' : '@${event.actorHandle}')
        : '${event.actorName} (${event.actorHandle.isEmpty ? 'system' : '@${event.actorHandle}'})';

    return Padding(
      padding: EdgeInsets.only(
        left: 20,
        right: 20,
        bottom: MediaQuery.viewInsetsOf(context).bottom + 20,
      ),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              event.title.isEmpty ? 'Activity' : event.title,
              style: theme.textTheme.titleLarge,
            ),
            const SizedBox(height: 4),
            Text(actor, style: metaStyle),
            const SizedBox(height: 2),
            Text(sodaDateTime(event.createdAt), style: metaStyle),
            if (event.detail.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(event.detail, style: theme.textTheme.bodyMedium),
            ],
            const SizedBox(height: 16),
            Text(
              'Web link',
              style: theme.textTheme.labelLarge?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 4),
            SelectableText(
              url.isEmpty ? 'No link for this event.' : url,
              style: theme.textTheme.bodySmall?.copyWith(
                color: scheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 18),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(
                  onPressed: () => Navigator.of(context).pop(),
                  child: const Text('Close'),
                ),
                const SizedBox(width: 8),
                FilledButton.icon(
                  key: const Key('admin-copy-link'),
                  onPressed: url.isEmpty ? null : () => _copy(context),
                  icon: const Icon(Icons.link_rounded, size: 18),
                  label: const Text('Copy link'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
