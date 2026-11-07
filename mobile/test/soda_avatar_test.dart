import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sodapop/src/widgets/soda_ui.dart';

Widget _app(Widget child) => MaterialApp(
  home: Scaffold(body: Center(child: child)),
);

void main() {
  testWidgets('the initial is used when there is no photo', (tester) async {
    await tester.pumpWidget(_app(const SodaAvatar(initial: 'ada')));

    expect(find.text('A'), findsOneWidget);
    expect(find.byType(Image), findsNothing);
  });

  testWidgets(
    'a photo request carries the auth headers and covers the circle',
    (tester) async {
      await tester.pumpWidget(
        _app(
          const SodaAvatar(
            initial: 'Ada',
            imageUrl: 'https://soda.example/avatar/a.png',
            headers: {'Authorization': 'Bearer sd_test'},
          ),
        ),
      );
      await tester.pump();

      final image = tester.widget<Image>(find.byType(Image));
      expect(image.fit, BoxFit.cover);
      final provider = image.image as NetworkImage;
      expect(provider.headers, {'Authorization': 'Bearer sd_test'});
      expect(provider.url, 'https://soda.example/avatar/a.png');
      await tester.pumpAndSettle();
    },
  );

  testWidgets('a relative avatar path is resolved against the server', (
    tester,
  ) async {
    await tester.pumpWidget(
      _app(
        const SodaAvatar(
          initial: 'Ada',
          imageUrl: '/api/v1/media/abc',
          baseUrl: 'https://soda.example',
        ),
      ),
    );
    await tester.pump();

    final provider =
        tester.widget<Image>(find.byType(Image)).image as NetworkImage;
    expect(provider.url, 'https://soda.example/api/v1/media/abc');
    await tester.pumpAndSettle();
  });

  testWidgets('a failed image falls back to the initial', (tester) async {
    await tester.pumpWidget(
      _app(
        const SodaAvatar(
          initial: 'Ada',
          imageUrl: 'https://soda.example/missing.png',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('A'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
