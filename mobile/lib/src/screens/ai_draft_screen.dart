import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/soda_ui.dart';
import 'drink_detail_screen.dart';

/// Opens the drink that a committed draft created. Injected by tests so they
/// can assert navigation without loading the catalog.
typedef AiDraftDrinkOpener =
    Future<void> Function(BuildContext context, String slug, {String name});

enum _Phase { start, conversation, unavailable }

enum _Role { user, assistant }

class _ChatEntry {
  const _ChatEntry(this.role, this.text, {this.revision = false});

  final _Role role;
  final String text;
  final bool revision;
}

/// Turns rough notes into a structured soda draft, one conversation at a time.
/// The draft stays provisional until the drinker commits it.
class AiDraftScreen extends ConsumerStatefulWidget {
  const AiDraftScreen({super.key, this.barcode = '', this.openDrink});

  final String barcode;
  final AiDraftDrinkOpener? openDrink;

  static Future<void> open(BuildContext context, {String barcode = ''}) {
    return Navigator.of(context).push<void>(
      MaterialPageRoute<void>(builder: (_) => AiDraftScreen(barcode: barcode)),
    );
  }

  @override
  ConsumerState<AiDraftScreen> createState() => _AiDraftScreenState();
}

class _AiDraftScreenState extends ConsumerState<AiDraftScreen> {
  final _notes = TextEditingController();
  final _message = TextEditingController();
  final _scroll = ScrollController();
  final List<_ChatEntry> _chat = [];

  _Phase _phase = _Phase.start;
  AiDraftTurn? _turn;
  bool _busy = false;
  bool _committing = false;
  String? _startError;
  String? _error;

  @override
  void dispose() {
    _notes.dispose();
    _message.dispose();
    _scroll.dispose();
    super.dispose();
  }

  bool get _canCommit {
    final turn = _turn;
    return turn != null && turn.sessionId.isNotEmpty && turn.canCommit;
  }

  Future<void> _start() async {
    if (_busy) return;
    final notes = _notes.text.trim();
    final barcode = widget.barcode.trim();
    if (notes.isEmpty && barcode.isEmpty) {
      setState(() => _startError = 'Add a few notes or a barcode to start.');
      return;
    }
    FocusScope.of(context).unfocus();
    setState(() {
      _busy = true;
      _startError = null;
    });
    try {
      final turn = await ref
          .read(apiProvider)
          .aiDraft(notes: notes, barcode: barcode);
      if (!mounted) return;
      setState(() {
        _busy = false;
        _phase = _Phase.conversation;
        _turn = turn;
        _chat
          ..clear()
          ..addAll([
            if (notes.isNotEmpty) _ChatEntry(_Role.user, notes),
            if (turn.message.isNotEmpty)
              _ChatEntry(_Role.assistant, turn.message),
          ]);
      });
      _scrollToEnd();
    } catch (error) {
      if (!mounted) return;
      final described = ApiClient.describe(error);
      setState(() {
        _busy = false;
        if (described.statusCode == 503) {
          // The server has no usable AI endpoint. Nothing is broken for the
          // rest of the app, so offer a quiet way back instead of an error.
          _phase = _Phase.unavailable;
        } else {
          _startError = described.message;
        }
      });
    }
  }

  Future<void> _send() async {
    if (_busy) return;
    final text = _message.text.trim();
    if (text.isEmpty) return;
    _message.clear();
    await _advance(
      _ChatEntry(_Role.user, text),
      (api, sessionId) => api.aiDraftMessage(sessionId, text),
      restore: text,
    );
  }

  Future<void> _revise() async {
    if (_busy) return;
    final instruction = await _ReviseDialog.show(context);
    if (instruction == null || !mounted) return;
    await _advance(
      _ChatEntry(_Role.user, instruction, revision: true),
      (api, sessionId) => api.aiDraftRevise(sessionId, instruction),
    );
  }

  /// Runs one turn, keeping the user's message on screen until the reply
  /// arrives. A failure restores what they typed so the turn can be retried.
  Future<void> _advance(
    _ChatEntry entry,
    Future<AiDraftTurn> Function(ApiClient api, String sessionId) send, {
    String? restore,
  }) async {
    final turn = _turn;
    if (_busy || turn == null || turn.sessionId.isEmpty) return;
    final api = ref.read(apiProvider);
    FocusScope.of(context).unfocus();
    setState(() {
      _busy = true;
      _error = null;
      _chat.add(entry);
    });
    _scrollToEnd();
    try {
      final next = await send(api, turn.sessionId);
      if (!mounted) return;
      setState(() {
        _busy = false;
        _turn = next;
        if (next.message.isNotEmpty) {
          _chat.add(_ChatEntry(_Role.assistant, next.message));
        }
      });
      _scrollToEnd();
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _chat.remove(entry);
        _error = ApiClient.describe(error).message;
        if (restore != null && _message.text.isEmpty) _message.text = restore;
      });
    }
  }

  Future<void> _commit() async {
    final turn = _turn;
    if (_busy || _committing || turn == null || !_canCommit) return;
    final api = ref.read(apiProvider);
    final navigator = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    final route = ModalRoute.of(context);
    final opener = widget.openDrink ?? DrinkDetailScreen.open;
    setState(() {
      _committing = true;
      _error = null;
    });
    try {
      final commit = await api.aiDraftCommit(turn.sessionId);
      if (!mounted) return;
      setState(() => _committing = false);
      messenger.showSnackBar(
        const SnackBar(content: Text('Provisional soda created')),
      );
      // Open the new soda, then drop this screen from under it so Back lands
      // where drafting began instead of on a committed draft.
      final opening = opener(context, commit.slug, name: commit.name);
      if (route != null && !route.isCurrent) navigator.removeRoute(route);
      await opening;
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _committing = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || !_scroll.hasClients) return;
      _scroll.animateTo(
        _scroll.position.maxScrollExtent,
        duration: const Duration(milliseconds: 240),
        curve: Curves.easeOutCubic,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Draft a soda')),
      body: SafeArea(
        top: false,
        child: switch (_phase) {
          _Phase.start => _startView(),
          _Phase.conversation => _conversationView(),
          _Phase.unavailable => _unavailableView(),
        },
      ),
    );
  }

  // ---------------------------------------------------------------- start

  Widget _startView() {
    final theme = Theme.of(context);
    final barcode = widget.barcode.trim();
    return ListView(
      padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
      children: [
        _HeroCard(barcode: barcode),
        const SizedBox(height: 26),
        _FieldLabel('Notes'),
        TextField(
          key: const Key('ai-notes'),
          controller: _notes,
          minLines: 4,
          maxLines: 7,
          textCapitalization: TextCapitalization.sentences,
          inputFormatters: [LengthLimitingTextInputFormatter(4000)],
          onChanged: (_) {
            if (_startError != null) setState(() => _startError = null);
          },
          decoration: InputDecoration(
            hintText:
                'A cherry cola in a 330 ml can. Sharp, not too sweet, '
                'with a dry finish.',
            helperMaxLines: 2,
            helperText: barcode.isEmpty
                ? 'Required without a barcode. Describe the flavour, brand '
                      'or can you have in mind.'
                : 'Optional. Add anything the barcode does not tell us.',
          ),
        ),
        if (_startError != null) ...[
          const SizedBox(height: 14),
          _Notice(message: _startError!),
        ],
        const SizedBox(height: 18),
        FilledButton(
          key: const Key('ai-start'),
          onPressed: _busy ? null : _start,
          child: Text(_busy ? 'Starting…' : 'Start draft'),
        ),
        const SizedBox(height: 30),
        Text(
          'How it works',
          style: theme.textTheme.titleSmall?.copyWith(
            fontWeight: FontWeight.w800,
          ),
        ),
        const SizedBox(height: 12),
        const _Step(
          number: 1,
          title: 'Start with what you know',
          body: 'Notes, a label, or the barcode already in hand.',
        ),
        const _Step(
          number: 2,
          title: 'Revise as it takes shape',
          body: 'Correct the style, the sweetness or the description.',
        ),
        const _Step(
          number: 3,
          title: 'Commit it as a provisional soda',
          body: 'The drink joins the catalog and can be refined later.',
        ),
      ],
    );
  }

  // --------------------------------------------------------- conversation

  Widget _conversationView() {
    final theme = Theme.of(context);
    final turn = _turn;
    final status = _statusLabel(turn?.status ?? '');
    final extra = (_busy ? 1 : 0) + 1; // draft card + optional progress bubble
    return Column(
      children: [
        Expanded(
          child: ListView.builder(
            controller: _scroll,
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 16),
            itemCount: _chat.length + extra,
            itemBuilder: (context, index) {
              if (index < _chat.length) {
                return Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: _ChatBubble(entry: _chat[index]),
                );
              }
              if (index == _chat.length) {
                return Padding(
                  padding: const EdgeInsets.only(top: 4, bottom: 10),
                  child: _DraftCard(turn: turn, statusLabel: status),
                );
              }
              return const _ProgressBubble();
            },
          ),
        ),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 10),
            child: _Notice(message: _error!),
          ),
        if (turn != null && turn.draft.isNotEmpty && !turn.canCommit)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 8),
            child: Text(
              'Add a name before committing.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 10),
          child: Row(
            children: [
              Expanded(
                child: OutlinedButton.icon(
                  key: const Key('ai-revise'),
                  onPressed: _busy ? null : _revise,
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size(44, 48),
                    foregroundColor: theme.colorScheme.onSurface,
                    side: BorderSide(
                      color: theme.colorScheme.onSurface.withValues(alpha: 0.2),
                    ),
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(14),
                    ),
                  ),
                  icon: const Icon(Icons.tune_rounded, size: 18),
                  label: const Text('Revise'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: FilledButton(
                  key: const Key('ai-commit'),
                  onPressed: _busy || _committing || !_canCommit
                      ? null
                      : _commit,
                  child: Text(_committing ? 'Committing…' : 'Commit'),
                ),
              ),
            ],
          ),
        ),
        _Composer(
          controller: _message,
          busy: _busy,
          onSend: _send,
        ),
      ],
    );
  }

  // ---------------------------------------------------------- unavailable

  Widget _unavailableView() {
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(28, 64, 28, 36),
      children: [
        Center(
          child: Container(
            width: 72,
            height: 72,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
              shape: BoxShape.circle,
              border: Border.all(
                color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
              ),
            ),
            child: Icon(
              Icons.cloud_off_rounded,
              size: 30,
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ),
        const SizedBox(height: 22),
        Text(
          'AI drafting is not available on this server',
          textAlign: TextAlign.center,
          style: theme.textTheme.titleMedium?.copyWith(
            fontWeight: FontWeight.w800,
          ),
        ),
        const SizedBox(height: 10),
        Text(
          'Everything else works as usual. You can still scan a barcode or '
          'log a soda from the catalog.',
          textAlign: TextAlign.center,
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
            height: 1.5,
          ),
        ),
        const SizedBox(height: 26),
        Center(
          child: OutlinedButton(
            onPressed: () => Navigator.of(context).maybePop(),
            child: const Text('Back'),
          ),
        ),
      ],
    );
  }
}

// ------------------------------------------------------------------ pieces

class _HeroCard extends StatelessWidget {
  const _HeroCard({required this.barcode});

  final String barcode;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final dark = theme.brightness == Brightness.dark;
    final accent = theme.colorScheme.primary;
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(24),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            accent.withValues(alpha: dark ? 0.24 : 0.45),
            theme.colorScheme.surface,
          ],
        ),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 42,
                height: 42,
                alignment: Alignment.center,
                decoration: BoxDecoration(
                  color: accent,
                  shape: BoxShape.circle,
                ),
                child: Icon(
                  Icons.auto_awesome_rounded,
                  size: 20,
                  color: theme.colorScheme.onPrimary,
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  'Turn notes into a soda',
                  style: theme.textTheme.titleLarge?.copyWith(
                    fontWeight: FontWeight.w800,
                    letterSpacing: -0.6,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          Text(
            'Describe the soda in your own words. The assistant proposes a '
            'provisional drink you can revise before committing it.',
            style: theme.textTheme.bodyMedium?.copyWith(height: 1.5),
          ),
          if (barcode.isNotEmpty) ...[
            const SizedBox(height: 16),
            SodaPill(label: 'Barcode $barcode', icon: Icons.qr_code_2_rounded),
          ],
        ],
      ),
    );
  }
}

class _Step extends StatelessWidget {
  const _Step({required this.number, required this.title, required this.body});

  final int number;
  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 14),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 26,
            height: 26,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: theme.colorScheme.primary.withValues(alpha: 0.45),
              shape: BoxShape.circle,
            ),
            child: Text(
              '$number',
              style: theme.textTheme.labelSmall?.copyWith(
                fontWeight: FontWeight.w800,
                color: theme.colorScheme.onSurface,
              ),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 2),
                Text(
                  body,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    height: 1.4,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _FieldLabel extends StatelessWidget {
  const _FieldLabel(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
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
}

class _Notice extends StatelessWidget {
  const _Notice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
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

class _ChatBubble extends StatelessWidget {
  const _ChatBubble({required this.entry});

  final _ChatEntry entry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final mine = entry.role == _Role.user;
    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.sizeOf(context).width * 0.84,
        ),
        child: Container(
          padding: const EdgeInsets.fromLTRB(14, 11, 14, 12),
          decoration: BoxDecoration(
            color: mine
                ? theme.colorScheme.primary.withValues(alpha: 0.28)
                : theme.colorScheme.surface,
            borderRadius: BorderRadius.only(
              topLeft: const Radius.circular(18),
              topRight: const Radius.circular(18),
              bottomLeft: Radius.circular(mine ? 18 : 6),
              bottomRight: Radius.circular(mine ? 6 : 18),
            ),
            border: mine
                ? null
                : Border.all(
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
                  ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (!mine)
                Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Text(
                    'Draft assistant',
                    style: theme.textTheme.labelSmall?.copyWith(
                      fontWeight: FontWeight.w800,
                      letterSpacing: 0.4,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
              if (entry.revision)
                Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        Icons.tune_rounded,
                        size: 12,
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                      const SizedBox(width: 4),
                      Text(
                        'Revision',
                        style: theme.textTheme.labelSmall?.copyWith(
                          fontWeight: FontWeight.w800,
                          letterSpacing: 0.4,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ),
              Text(
                entry.text,
                style: theme.textTheme.bodyMedium?.copyWith(height: 1.45),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _DraftCard extends StatelessWidget {
  const _DraftCard({required this.turn, required this.statusLabel});

  final AiDraftTurn? turn;
  final String statusLabel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final draft = turn?.draft ?? const <String, dynamic>{};
    final name = _valueText(draft['name']);
    final brand = _valueText(draft['brand']);
    final style = _valueText(draft['style']);
    final container = _valueText(draft['container']);
    final volume = _amount(draft['volume_ml']);
    final description = _valueText(draft['description']);
    final sugar = _amount(draft['sugar_g_per_100ml']);
    final caffeine = _amount(draft['caffeine_mg_per_100ml']);
    final calories = _amount(draft['calories_kcal_per_100ml']);
    final flavours = _flavourNames(draft['flavours']);

    final subtitle = [
      brand,
      style,
      if (container.isNotEmpty) _capitalised(container),
      if (volume != null && volume > 0) '${_whole(volume)} ml',
    ].where((part) => part.isNotEmpty).join(' · ');

    final facts = <Widget>[
      if (sugar != null && sugar > 0)
        _Fact(value: '${_decimal(sugar)} g / 100 ml', label: 'Sugar'),
      if (caffeine != null && caffeine > 0)
        _Fact(value: '${_decimal(caffeine)} mg / 100 ml', label: 'Caffeine'),
      if (calories != null && calories > 0)
        _Fact(value: '${_whole(calories)} kcal / 100 ml', label: 'Calories'),
    ];

    final hasContent =
        name.isNotEmpty ||
        subtitle.isNotEmpty ||
        description.isNotEmpty ||
        facts.isNotEmpty ||
        flavours.isNotEmpty;

    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 240),
      switchInCurve: Curves.easeOutCubic,
      switchOutCurve: Curves.easeIn,
      transitionBuilder: (child, animation) => FadeTransition(
        opacity: animation,
        child: SlideTransition(
          position: Tween<Offset>(
            begin: const Offset(0, 0.02),
            end: Offset.zero,
          ).animate(animation),
          child: child,
        ),
      ),
      child: Container(
        key: ValueKey(_draftSignature(turn)),
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: theme.colorScheme.surface,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.12),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  Icons.auto_awesome_rounded,
                  size: 15,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    'Draft so far',
                    style: theme.textTheme.labelSmall?.copyWith(
                      fontWeight: FontWeight.w800,
                      letterSpacing: 0.7,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
                if (statusLabel.isNotEmpty) _StatusPill(label: statusLabel),
              ],
            ),
            const SizedBox(height: 12),
            Text(
              name.isEmpty ? 'Untitled draft' : name,
              style: theme.textTheme.titleLarge?.copyWith(
                fontWeight: FontWeight.w800,
                letterSpacing: -0.4,
              ),
            ),
            if (subtitle.isNotEmpty) ...[
              const SizedBox(height: 3),
              Text(
                subtitle,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            if (facts.isNotEmpty) ...[
              const SizedBox(height: 14),
              Wrap(spacing: 8, runSpacing: 8, children: facts),
            ],
            if (description.isNotEmpty) ...[
              const SizedBox(height: 14),
              Text(
                description,
                style: theme.textTheme.bodySmall?.copyWith(height: 1.45),
              ),
            ],
            if (flavours.isNotEmpty) ...[
              const SizedBox(height: 14),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final flavour in flavours) SodaPill(label: flavour),
                ],
              ),
            ],
            if (!hasContent) ...[
              const SizedBox(height: 8),
              Text(
                'No fields yet. Add a note below to describe the soda.',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                  height: 1.4,
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({required this.value, required this.label});

  final String value;
  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            value,
            style: theme.textTheme.labelMedium?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
          const SizedBox(height: 1),
          Text(
            label,
            style: theme.textTheme.labelSmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
      decoration: BoxDecoration(
        color: theme.colorScheme.primary.withValues(alpha: 0.32),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: theme.textTheme.labelSmall?.copyWith(
          fontWeight: FontWeight.w700,
          color: theme.colorScheme.onSurface,
        ),
      ),
    );
  }
}

class _ProgressBubble extends StatelessWidget {
  const _ProgressBubble();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Align(
      alignment: Alignment.centerLeft,
      child: Container(
        padding: const EdgeInsets.fromLTRB(14, 11, 16, 11),
        decoration: BoxDecoration(
          color: theme.colorScheme.surface,
          borderRadius: BorderRadius.circular(16),
          border: Border.all(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
          ),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            const SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: Soda.lime,
              ),
            ),
            const SizedBox(width: 10),
            Text(
              'Working on the draft…',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Composer extends StatelessWidget {
  const _Composer({
    required this.controller,
    required this.busy,
    required this.onSend,
  });

  final TextEditingController controller;
  final bool busy;
  final VoidCallback onSend;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 8, 12, 8),
      decoration: BoxDecoration(
        color: theme.scaffoldBackgroundColor,
        border: Border(
          top: BorderSide(
            color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
          ),
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: TextField(
              key: const Key('ai-message'),
              controller: controller,
              enabled: !busy,
              minLines: 1,
              maxLines: 4,
              textCapitalization: TextCapitalization.sentences,
              inputFormatters: [LengthLimitingTextInputFormatter(2000)],
              textInputAction: TextInputAction.send,
              onSubmitted: (_) {
                if (!busy) onSend();
              },
              decoration: const InputDecoration(
                hintText: 'Add a note or correction',
                isDense: true,
              ),
            ),
          ),
          const SizedBox(width: 8),
          ValueListenableBuilder<TextEditingValue>(
            valueListenable: controller,
            builder: (context, value, _) {
              final ready = !busy && value.text.trim().isNotEmpty;
              return IconButton.filled(
                key: const Key('ai-send'),
                tooltip: 'Send',
                onPressed: ready ? onSend : null,
                icon: const Icon(Icons.arrow_upward_rounded),
              );
            },
          ),
        ],
      ),
    );
  }
}

/// A 2000-character instruction box, matching the server's per-turn limit.
class _ReviseDialog extends StatefulWidget {
  const _ReviseDialog();

  static Future<String?> show(BuildContext context) {
    return showDialog<String>(
      context: context,
      builder: (_) => const _ReviseDialog(),
    );
  }

  @override
  State<_ReviseDialog> createState() => _ReviseDialogState();
}

class _ReviseDialogState extends State<_ReviseDialog> {
  final _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    Navigator.of(context).pop(text);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: const Text('Revise the draft'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Tell the assistant what to change.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 12),
          TextField(
            key: const Key('ai-revise-field'),
            controller: _controller,
            autofocus: true,
            minLines: 3,
            maxLines: 5,
            maxLength: 2000,
            textCapitalization: TextCapitalization.sentences,
            inputFormatters: [LengthLimitingTextInputFormatter(2000)],
            decoration: const InputDecoration(
              hintText: 'Make it less sweet and call it a limited run.',
            ),
            onChanged: (_) => setState(() {}),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('ai-revise-submit'),
          onPressed: _controller.text.trim().isEmpty ? null : _submit,
          child: const Text('Revise'),
        ),
      ],
    );
  }
}

// --------------------------------------------------------------- formatting

String _valueText(Object? value) {
  if (value is String) return value.trim();
  if (value is num) return _decimal(value.toDouble());
  return '';
}

double? _amount(Object? value) {
  if (value is num) return value.toDouble();
  if (value is String) return double.tryParse(value.trim());
  return null;
}

/// One decimal place is all a label or a nutrition figure needs; whole
/// numbers lose their trailing `.0`.
String _decimal(double value) {
  final rounded = (value * 10).round() / 10;
  if (rounded == rounded.roundToDouble()) return '${rounded.round()}';
  return rounded.toStringAsFixed(1);
}

String _whole(double value) => '${value.round()}';

String _capitalised(String value) =>
    value.isEmpty ? value : value[0].toUpperCase() + value.substring(1);

List<String> _flavourNames(Object? value) {
  if (value is! List) return const [];
  final names = <String>[];
  for (final item in value) {
    final name = item is Map ? _valueText(item['name']) : _valueText(item);
    if (name.isNotEmpty) names.add(name);
  }
  return names;
}

String _statusLabel(String status) {
  switch (status) {
    case 'ready':
      return 'Ready to commit';
    case 'committed':
      return 'Committed';
    case 'chatting':
      return 'Still drafting';
    default:
      return '';
  }
}

/// Rebuilds the draft card only when its visible fields change.
String _draftSignature(AiDraftTurn? turn) {
  if (turn == null) return 'empty';
  final draft = turn.draft;
  return [
    turn.sessionId,
    turn.status,
    _valueText(draft['name']),
    _valueText(draft['brand']),
    _valueText(draft['style']),
    _valueText(draft['container']),
    '${_amount(draft['volume_ml'])}',
    '${_amount(draft['sugar_g_per_100ml'])}',
    '${_amount(draft['caffeine_mg_per_100ml'])}',
    '${_amount(draft['calories_kcal_per_100ml'])}',
    _valueText(draft['description']),
    _flavourNames(draft['flavours']).join(','),
  ].join('|');
}
