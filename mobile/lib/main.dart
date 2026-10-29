import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'src/app.dart';
import 'src/push/firebase_push.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await initialisePush();
  runApp(const ProviderScope(child: SodapopApp()));
}
