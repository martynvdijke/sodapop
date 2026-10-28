import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/ai_draft_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

const _firstDraft = AiDraftTurn(
  sessionId: 's1',
  status: 'ready',
  message: 'Here is a first draft of the cherry cola.',
  draft: {
    'name': 'Cherry Cola',
    'brand': 'Soda Co',
    'style': 'Cola',
    'container': 'can',
    'volume_ml': 355,
    'sugar_g_per_100ml': 10.6,
    'caffeine_mg_per_100ml': 12,
    'calories_kcal_per_100ml': 44,
    'description': 'Sharp cherry and a long, dry finish.',
    'flavours': [
      {'name': 'Cherry', 'intensity': 3},
    ],
    'reply': 'internal reply',
    '_meta': {'upstream_session_id': 'up-1'},
    'unlisted_field': 'should never be shown',
  },
);

const _sweeterDraft = AiDraftTurn(
  sessionId: 's1',
  status: 'ready',
  message: 'I lowered the sugar a little.',
  draft: {
    'name': 'Cherry Cola',
    'brand': 'Soda Co',
    'style': 'Cola',
    'container': 'can',
    'volume_ml': 355,
    'sugar_g_per_100ml': 8.2,
    'caffeine_mg_per_100ml': 12,
    'calories_kcal_per_100ml': 36,
    'description': 'Sharp cherry and a long, dry finish.',
    'flavours': [
      {'name': 'Cherry', 'intensity': 3},
    ],
  },
);

const _revisedDraft = AiDraftTurn(
  sessionId: 's1',
  status: 'ready',
  message: 'Named it as a limited run.',
  draft: {
    'name': 'Cherry Cola Limited',
    'brand': 'Soda Co',
    'style': 'Cola',
    'container': 'can',
    'volume_ml': 355,
    'sugar_g_per_100ml': 8.2,
    'caffeine_mg_per_100ml': 12,
    'calories_kcal_per_100ml': 36,
    'description': 'Sharp cherry and a long, dry finish.',
    'flavours': [
      {'name': 'Cherry', 'intensity': 3},
    ],
  },
);

const _commitResult = AiCommit(
  drinkId: 12,
  slug: 'cherry-cola',
  name: 'Cherry Cola Limited',
  status: 'provisional',
  sessionId: 's1',
  message: 'provisional drink committed',
);

class _FakeAiApi extends ApiClient {
  _FakeAiApi({
    this.startError,
    this.commitError,
    this.startTurn = _firstDraft,
  });

  final Object? startError;
  Object? commitError;
  final AiDraftTurn startTurn;

  Completer<AiDraftTurn>? messageGate;

  int startCalls = 0;
  int commitCalls = 0;
  final List<String> startNotes = [];
  final List<String> startBarcodes = [];
  final List<String> messages = [];
  final List<String> instructions = [];
  final List<String> sessions = [];

  @override
  Future<AiDraftTurn> aiDraft({String notes = '', String barcode = ''}) async {
    startCalls += 1;
    final failure = startError;
    if (failure != null) throw failure;
    startNotes.add(notes);
    startBarcodes.add(barcode);
    return startTurn;
  }

  @override
  Future<AiDraftTurn> aiDraftMessage(String sessionId, String content) async {
    sessions.add(sessionId);
    messages.add(content);
    final gate = messageGate;
    if (gate != null) return gate.future;
    return _sweeterDraft;
  }

  @override
  Future<AiDraftTurn> aiDraftRevise(
    String sessionId,
    String instruction,
  ) async {
    sessions.add(sessionId);
    instructions.add(instruction);
    return _revisedDraft;
  }

  @override
  Future<AiCommit> aiDraftCommit(String sessionId) async {
    commitCalls += 1;
    sessions.add(sessionId);
    final failure = commitError;
    if (failure != null) throw failure;
    return _commitResult;
  }
}

class _Host extends StatelessWidget {
  const _Host({required this.api, this.openDrink});

  final ApiClient api;
  final AiDraftDrinkOpener? openDrink;

  @override
  Widget build(BuildContext context) {
    return ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: MaterialApp(
        theme: sodaTheme(Brightness.light),
        home: Scaffold(
          body: Center(
            child: Builder(
              builder: (context) => FilledButton(
                onPressed: () => Navigator.of(context).push<void>(
                  MaterialPageRoute<void>(
                    builder: (_) => AiDraftScreen(openDrink: openDrink),
                  ),
                ),
                child: const Text('Open draft'),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _BarcodeHost extends StatelessWidget {
  const _BarcodeHost({required this.api});

  final ApiClient api;

  @override
  Widget build(BuildContext context) {
    return ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: MaterialApp(
        theme: sodaTheme(Brightness.light),
        home: Scaffold(
          body: Center(
            child: Builder(
              builder: (context) => FilledButton(
                onPressed: () => AiDraftScreen.open(
                  context,
                  barcode: '5449000000996',
                ),
                child: const Text('Open draft'),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

Future<void> _openScreen(
  WidgetTester tester,
  ApiClient api, {
  AiDraftDrinkOpener? openDrink,
  Widget? host,
}) async {
  tester.view.physicalSize = const Size(420, 1000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    host ?? _Host(api: api, openDrink: openDrink),
  );
  await tester.tap(find.text('Open draft'));
  await tester.pumpAndSettle();
}

Future<void> _startDraft(
  WidgetTester tester, {
  String notes = 'A cherry cola in a 355 ml can.',
}) async {
  await tester.enterText(find.byKey(const Key('ai-notes')), notes);
  await tester.tap(find.byKey(const Key('ai-start')));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('notes are required when there is no barcode', (tester) async {
    final api = _FakeAiApi();
    await _openScreen(tester, api);

    await tester.tap(find.byKey(const Key('ai-start')));
    await tester.pump();

    expect(
      find.text('Add a few notes or a barcode to start.'),
      findsOneWidget,
    );
    expect(api.startCalls, 0);
    expect(find.text('Draft so far'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a barcode lets the draft start without notes', (tester) async {
    final api = _FakeAiApi();
    await _openScreen(tester, api, host: _BarcodeHost(api: api));

    expect(find.text('Barcode 5449000000996'), findsOneWidget);
    expect(
      find.text('Optional. Add anything the barcode does not tell us.'),
      findsOneWidget,
    );

    await tester.tap(find.byKey(const Key('ai-start')));
    await tester.pumpAndSettle();

    expect(api.startCalls, 1);
    expect(api.startNotes.single, '');
    expect(api.startBarcodes.single, '5449000000996');
    expect(find.text('Draft so far'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('drafts, sends a message, revises and commits', (tester) async {
    final api = _FakeAiApi();
    String? openedSlug;
    String? openedName;

    await _openScreen(
      tester,
      api,
      openDrink: (context, slug, {name = ''}) async {
        openedSlug = slug;
        openedName = name;
        await Navigator.of(context).push<void>(
          MaterialPageRoute<void>(
            builder: (_) => const Scaffold(body: Text('Drink detail')),
          ),
        );
      },
    );

    await _startDraft(tester);
    expect(api.startNotes.single, 'A cherry cola in a 355 ml can.');

    // The first turn: user notes, assistant reply and the draft card.
    expect(
      find.text('Here is a first draft of the cherry cola.'),
      findsOneWidget,
    );
    expect(find.text('Draft so far'), findsOneWidget);
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.text('Soda Co · Cola · Can · 355 ml'), findsOneWidget);
    expect(find.text('10.6 g / 100 ml'), findsOneWidget);
    expect(find.text('12 mg / 100 ml'), findsOneWidget);
    expect(find.text('44 kcal / 100 ml'), findsOneWidget);
    expect(find.text('Cherry'), findsOneWidget);
    expect(find.text('Ready to commit'), findsOneWidget);
    expect(
      find.text('Sharp cherry and a long, dry finish.'),
      findsOneWidget,
    );
    // Unknown and meta draft keys are ignored rather than dumped as JSON.
    expect(find.text('should never be shown'), findsNothing);
    expect(find.text('internal reply'), findsNothing);
    expect(find.textContaining('_meta'), findsNothing);

    // A follow-up message replaces the draft card.
    await tester.enterText(
      find.byKey(const Key('ai-message')),
      'Less sweet please',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('ai-send')));
    await tester.pumpAndSettle();

    expect(api.messages.single, 'Less sweet please');
    expect(api.sessions.single, 's1');
    expect(find.text('I lowered the sugar a little.'), findsOneWidget);
    expect(find.text('8.2 g / 100 ml'), findsOneWidget);
    expect(find.text('36 kcal / 100 ml'), findsOneWidget);
    expect(find.text('10.6 g / 100 ml'), findsNothing);

    // Revise through the capped instruction dialog.
    await tester.tap(find.byKey(const Key('ai-revise')));
    await tester.pumpAndSettle();
    expect(find.text('Revise the draft'), findsOneWidget);

    await tester.enterText(
      find.byKey(const Key('ai-revise-field')),
      'Call it a limited run',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('ai-revise-submit')));
    await tester.pumpAndSettle();

    expect(api.instructions.single, 'Call it a limited run');
    expect(find.text('Revision'), findsOneWidget);
    expect(find.text('Cherry Cola Limited'), findsOneWidget);
    expect(find.text('Named it as a limited run.'), findsOneWidget);

    // Commit opens the new soda and leaves the AI screen behind.
    await tester.tap(find.byKey(const Key('ai-commit')));
    await tester.pumpAndSettle();

    expect(api.commitCalls, 1);
    expect(api.sessions.last, 's1');
    expect(openedSlug, 'cherry-cola');
    expect(openedName, 'Cherry Cola Limited');
    expect(find.text('Provisional soda created'), findsOneWidget);
    expect(find.text('Drink detail'), findsOneWidget);
    expect(find.text('Draft a soda'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a busy draft shows progress and ignores a second send', (
    tester,
  ) async {
    final api = _FakeAiApi()..messageGate = Completer<AiDraftTurn>();
    await _openScreen(tester, api);
    await _startDraft(tester);

    await tester.enterText(
      find.byKey(const Key('ai-message')),
      'Less sweet please',
    );
    await tester.pump();
    await tester.tap(find.byKey(const Key('ai-send')));
    await tester.pump();

    expect(find.text('Working on the draft…'), findsOneWidget);
    expect(api.messages, ['Less sweet please']);

    await tester.tap(find.byKey(const Key('ai-send')), warnIfMissed: false);
    await tester.pump();
    expect(api.messages, hasLength(1));

    api.messageGate!.complete(_sweeterDraft);
    await tester.pumpAndSettle();

    expect(find.text('I lowered the sugar a little.'), findsOneWidget);
    expect(find.text('Working on the draft…'), findsNothing);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a server without AI drafting offers a quiet way back', (
    tester,
  ) async {
    final api = _FakeAiApi(
      startError: ApiException(
        'AI features are not configured',
        statusCode: 503,
      ),
    );
    await _openScreen(tester, api);
    await _startDraft(tester);

    expect(
      find.text('AI drafting is not available on this server'),
      findsOneWidget,
    );
    expect(find.text('AI features are not configured'), findsNothing);
    expect(find.byKey(const Key('ai-commit')), findsNothing);

    await tester.tap(find.text('Back'));
    await tester.pumpAndSettle();

    expect(find.text('Open draft'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a failed commit keeps the draft and shows the error', (
    tester,
  ) async {
    final api = _FakeAiApi(
      commitError: ApiException('The draft needs a name', statusCode: 400),
    );
    await _openScreen(tester, api);
    await _startDraft(tester);

    await tester.tap(find.byKey(const Key('ai-commit')));
    await tester.pumpAndSettle();

    expect(api.commitCalls, 1);
    expect(find.text('The draft needs a name'), findsOneWidget);
    expect(find.text('Draft so far'), findsOneWidget);
    expect(find.text('Cherry Cola'), findsOneWidget);
    expect(find.byKey(const Key('ai-commit')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('a failed first turn returns to the start form', (tester) async {
    final api = _FakeAiApi(
      startError: ApiException('The server did not answer', statusCode: 500),
    );
    await _openScreen(tester, api);
    await _startDraft(tester);

    expect(find.text('The server did not answer'), findsOneWidget);
    expect(find.byKey(const Key('ai-notes')), findsOneWidget);
    expect(find.text('Draft a soda'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an empty draft disables commit and says why', (tester) async {
    final api = _FakeAiApi(
      startTurn: const AiDraftTurn(
        sessionId: 's1',
        status: 'chatting',
        message: 'Tell me more about the soda.',
      ),
    );
    await _openScreen(tester, api);
    await _startDraft(tester);

    expect(find.text('Untitled draft'), findsOneWidget);
    expect(
      find.text('No fields yet. Add a note below to describe the soda.'),
      findsOneWidget,
    );
    expect(find.text('Still drafting'), findsOneWidget);
    final commit = tester.widget<FilledButton>(
      find.byKey(const Key('ai-commit')),
    );
    expect(commit.onPressed, isNull);
    expect(tester.takeException(), isNull);
  });
}
