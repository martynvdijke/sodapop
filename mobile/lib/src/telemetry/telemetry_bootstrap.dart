import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/auth_controller.dart';
import '../auth/server_defaults.dart';
import 'telemetry.dart';

/// Starts the OpenTelemetry exporter with the endpoint published by the active
/// server. It watches auth state only to learn which server is in use: the
/// fetch is fire-and-forget, so it never blocks the first frame and an offline
/// device simply keeps the compile-time fallback (or no exporter at all).
class TelemetryBootstrap extends ConsumerStatefulWidget {
  const TelemetryBootstrap({super.key, required this.child});

  final Widget child;

  @override
  ConsumerState<TelemetryBootstrap> createState() => _TelemetryBootstrapState();
}

class _TelemetryBootstrapState extends ConsumerState<TelemetryBootstrap> {
  String? _startedFor;

  @override
  void initState() {
    super.initState();
    ref.listenManual<AuthState>(
      authProvider,
      (_, next) => _resolve(next.serverUrl),
      fireImmediately: true,
    );
  }

  void _resolve(String authServerUrl) {
    final configured = ref.read(apiProvider).server;
    final candidate = (configured ?? authServerUrl).trim();
    final url = candidate.isEmpty ? defaultServerUrl : candidate;
    if (url == _startedFor) return;
    _startedFor = url;
    unawaited(Telemetry.instance.startFromServer(url));
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
