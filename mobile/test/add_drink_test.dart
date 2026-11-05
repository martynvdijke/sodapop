import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/api/api_client.dart';
import 'package:sodapop/src/api/models.dart';
import 'package:sodapop/src/auth/auth_controller.dart';
import 'package:sodapop/src/screens/add_drink_screen.dart';
import 'package:sodapop/src/theme/soda_theme.dart';

class _CreateCall {
  _CreateCall({
    required this.name,
    required this.brand,
    required this.styleSlug,
    required this.container,
    required this.volumeMl,
    required this.sweetener,
    required this.carbonation,
    required this.acid,
    required this.barcode,
    required this.description,
  });

  final String name;
  final String brand;
  final String styleSlug;
  final String container;
  final int volumeMl;
  final String sweetener;
  final String carbonation;
  final String acid;
  final String barcode;
  final String description;
}

class _FakeApi extends ApiClient {
  _FakeApi({this.result, this.failure});

  DrinkCreation? result;
  Object? failure;
  Completer<DrinkCreation>? gate;
  final calls = <_CreateCall>[];

  @override
  Future<DrinkCreation> createDrink({
    required String name,
    String brand = '',
    String styleSlug = '',
    String container = 'can',
    int volumeMl = 355,
    String sweetener = '',
    String carbonation = '',
    String acid = '',
    String barcode = '',
    String description = '',
  }) async {
    calls.add(
      _CreateCall(
        name: name,
        brand: brand,
        styleSlug: styleSlug,
        container: container,
        volumeMl: volumeMl,
        sweetener: sweetener,
        carbonation: carbonation,
        acid: acid,
        barcode: barcode,
        description: description,
      ),
    );
    final waiting = gate;
    if (waiting != null) return waiting.future;
    final error = failure;
    if (error != null) throw error;
    return result ?? const DrinkCreation();
  }
}

class _Opened {
  _Opened(this.slug, this.name);

  final String slug;
  final String name;
}

Future<List<_Opened>> _pumpForm(
  WidgetTester tester,
  ApiClient api, {
  String barcode = '',
  String initialName = '',
  String initialBrand = '',
  int? initialVolumeMl,
}) async {
  tester.view.physicalSize = const Size(420, 1400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final opened = <_Opened>[];
  await tester.pumpWidget(
    ProviderScope(
      overrides: [apiProvider.overrideWithValue(api)],
      child: MaterialApp(
        theme: sodaTheme(Brightness.light),
        home: AddDrinkScreen(
          barcode: barcode,
          initialName: initialName,
          initialBrand: initialBrand,
          initialVolumeMl: initialVolumeMl,
          openDrink: (context, slug, {name = ''}) async {
            opened.add(_Opened(slug, name));
          },
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
  return opened;
}

Future<void> _choose(WidgetTester tester, Key key, String label) async {
  await tester.tap(find.byKey(key));
  await tester.pumpAndSettle();
  await tester.tap(find.text(label).last);
  await tester.pumpAndSettle();
}

String _text(WidgetTester tester, Key key) =>
    tester.widget<TextFormField>(find.byKey(key)).controller!.text;

Future<void> _submit(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('add-submit')));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 50));
}

void main() {
  testWidgets('the form maps every field onto createDrink', (tester) async {
    final api = _FakeApi(
      result: const DrinkCreation(
        id: 12,
        slug: 'soda-co-cherry-cola',
        name: 'Cherry Cola',
        status: 'provisional',
        created: true,
      ),
    );
    final opened = await _pumpForm(tester, api, barcode: '05449000000996');

    // The scanned code is prefilled and editable.
    expect(
      tester
          .widget<TextFormField>(find.byKey(const Key('add-barcode')))
          .controller!
          .text,
      '05449000000996',
    );
    expect(find.text('Required.'), findsOneWidget);

    await tester.enterText(find.byKey(const Key('add-name')), 'Cherry Cola');
    await tester.enterText(find.byKey(const Key('add-brand')), 'Soda Co');
    await tester.enterText(find.byKey(const Key('add-volume')), '330');
    await tester.enterText(
      find.byKey(const Key('add-notes')),
      'Sharp cherry and a dry finish.',
    );
    await _choose(tester, const Key('add-style'), 'Cola');
    await _choose(tester, const Key('add-container'), 'Glass bottle');
    await _choose(tester, const Key('add-sweetener'), 'Sugar');
    await _choose(tester, const Key('add-carbonation'), 'Standard');
    await _choose(tester, const Key('add-acid'), 'Citric');

    await _submit(tester);

    expect(api.calls, hasLength(1));
    final call = api.calls.single;
    expect(call.name, 'Cherry Cola');
    expect(call.brand, 'Soda Co');
    expect(call.styleSlug, 'cola');
    expect(call.container, 'glass');
    expect(call.volumeMl, 330);
    expect(call.sweetener, 'sugar');
    expect(call.carbonation, 'standard');
    expect(call.acid, 'citric');
    expect(call.barcode, '05449000000996');
    expect(call.description, 'Sharp cherry and a dry finish.');

    expect(find.text('Added as provisional'), findsOneWidget);
    expect(opened.single.slug, 'soda-co-cherry-cola');
    expect(opened.single.name, 'Cherry Cola');
  });

  testWidgets('an Open Food Facts suggestion prefills the form', (
    tester,
  ) async {
    final api = _FakeApi();
    await _pumpForm(
      tester,
      api,
      barcode: '05449000000996',
      initialName: 'Cherry Cola',
      initialBrand: 'Soda Co',
      initialVolumeMl: 330,
    );

    expect(_text(tester, const Key('add-barcode')), '05449000000996');
    expect(_text(tester, const Key('add-name')), 'Cherry Cola');
    expect(_text(tester, const Key('add-brand')), 'Soda Co');
    expect(_text(tester, const Key('add-volume')), '330');
  });

  testWidgets('an existing soda says so and opens the catalog row', (
    tester,
  ) async {
    final api = _FakeApi(
      result: const DrinkCreation(
        id: 3,
        slug: 'coca-cola',
        name: 'Coca-Cola',
        status: 'active',
        created: false,
      ),
    );
    final opened = await _pumpForm(tester, api);

    await tester.enterText(find.byKey(const Key('add-name')), 'Coca-Cola');
    await _submit(tester);

    expect(find.text('This soda was already in the catalog'), findsOneWidget);
    expect(find.text('Added as provisional'), findsNothing);
    expect(opened.single.slug, 'coca-cola');
    expect(opened.single.name, 'Coca-Cola');
  });

  testWidgets('a missing name is caught before calling the server', (
    tester,
  ) async {
    final api = _FakeApi();
    await _pumpForm(tester, api);

    await _submit(tester);

    expect(find.text('Add a name for the soda.'), findsOneWidget);
    expect(api.calls, isEmpty);
  });

  testWidgets('an invalid barcode is caught before calling the server', (
    tester,
  ) async {
    final api = _FakeApi();
    await _pumpForm(tester, api);

    await tester.enterText(find.byKey(const Key('add-name')), 'Cherry Cola');
    await tester.enterText(find.byKey(const Key('add-barcode')), '12345');
    await _submit(tester);

    expect(
      find.text('That barcode is not valid. Check the digits.'),
      findsOneWidget,
    );
    expect(api.calls, isEmpty);
  });

  testWidgets('a server error stays inline and the form can retry', (
    tester,
  ) async {
    final api = _FakeApi(
      failure: ApiException('That barcode is not a valid GTIN.'),
    );
    await _pumpForm(tester, api);

    await tester.enterText(find.byKey(const Key('add-name')), 'Cherry Cola');
    await _submit(tester);

    expect(find.text('That barcode is not a valid GTIN.'), findsOneWidget);
    expect(find.text('Add soda'), findsOneWidget);

    api
      ..failure = null
      ..result = const DrinkCreation(
        id: 5,
        slug: 'cherry-cola',
        name: 'Cherry Cola',
        created: true,
      );
    await _submit(tester);

    expect(api.calls, hasLength(2));
    expect(find.text('Added as provisional'), findsOneWidget);
  });

  testWidgets('the button is disabled while the request is in flight', (
    tester,
  ) async {
    final gate = Completer<DrinkCreation>();
    final api = _FakeApi()..gate = gate;
    await _pumpForm(tester, api);

    await tester.enterText(find.byKey(const Key('add-name')), 'Cherry Cola');
    await tester.tap(find.byKey(const Key('add-submit')));
    await tester.pump();

    expect(find.text('Adding…'), findsOneWidget);
    final button = tester.widget<FilledButton>(
      find.byKey(const Key('add-submit')),
    );
    expect(button.onPressed, isNull);

    gate.complete(
      const DrinkCreation(
        id: 5,
        slug: 'cherry-cola',
        name: 'Cherry Cola',
        created: true,
      ),
    );
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 50));

    expect(find.text('Added as provisional'), findsOneWidget);
  });
}
