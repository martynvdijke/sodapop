import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/gtin.dart';
import 'add_drink_screen.dart';
import 'ai_draft_screen.dart';
import 'checkin_detail_screen.dart';
import 'checkin_sheet.dart';

class ScannerScreen extends ConsumerStatefulWidget {
  const ScannerScreen({super.key, this.draftOpener, this.addOpener});

  /// Optional test overrides for the unknown-barcode actions. Production opens
  /// [AiDraftScreen] or [AddDrinkScreen] with the scanned code.
  final ValueChanged<String>? draftOpener;

  /// Receives the normalized code and the server's Open Food Facts guess, if
  /// there was one.
  final void Function(String barcode, BarcodeSuggestion? suggested)? addOpener;

  @override
  ConsumerState<ScannerScreen> createState() => _ScannerScreenState();
}

class _ScannerScreenState extends ConsumerState<ScannerScreen> {
  final _controller = MobileScannerController(
    detectionSpeed: DetectionSpeed.noDuplicates,
    // Android asks ML Kit for a zoom suggestion, which helps with small and
    // curved EU cans held close to the lens.
    autoZoom: true,
    // UPC-E is deliberately absent: this app turns an 8 digit code into a
    // GTIN-14 by zero padding, which does not describe the same product.
    // EAN-13 covers Europe, UPC-A is still common on imported cans, and
    // Code 128 shows up on multipacks.
    formats: const [
      BarcodeFormat.ean13,
      BarcodeFormat.ean8,
      BarcodeFormat.upcA,
      BarcodeFormat.code128,
    ],
  );
  final _manual = TextEditingController();
  BarcodeResult? _result;
  String? _gtin;
  String? _error;
  bool _looking = false;

  /// Copied from the controller so the lime frame can step aside when the
  /// camera fails. The controller notifies while `MobileScanner` is building,
  /// so this is synced on the next frame rather than during build.
  MobileScannerException? _cameraError;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_syncCameraError);
  }

  @override
  void dispose() {
    _controller.removeListener(_syncCameraError);
    _controller.dispose();
    _manual.dispose();
    super.dispose();
  }

  void _syncCameraError() {
    final error = _controller.value.error;
    if (identical(error, _cameraError)) return;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      final next = _controller.value.error;
      if (identical(next, _cameraError)) return;
      setState(() => _cameraError = next);
    });
  }

  Future<void> _onDetect(BarcodeCapture capture) async {
    if (!mounted || _looking || _result != null) return;
    final raw = capture.barcodes
        .map((b) => b.rawValue)
        .whereType<String>()
        .firstOrNull;
    if (raw == null) return;
    await _lookup(raw);
  }

  /// Plain wording for a camera failure. A denied permission is the common
  /// case on Android 13+, so it gets directions the user can act on.
  String _cameraMessage(Object error, {bool starting = true}) {
    if (error is MobileScannerException) {
      switch (error.errorCode) {
        case MobileScannerErrorCode.permissionDenied:
          return 'Sodapop needs camera access to read cans. Allow the camera '
              'in your system settings, then try again.';
        case MobileScannerErrorCode.unsupported:
          return 'This device cannot use a camera to scan. Enter the barcode '
              'below instead.';
        case MobileScannerErrorCode.controllerDisposed:
          return 'The camera was closed. Open the scanner again to use it.';
        default:
          break;
      }
    }
    return starting
        ? 'The camera could not start. Tap Try again, or enter the barcode '
              'below.'
        : 'The camera stopped unexpectedly. Tap Try again, or enter the '
              'barcode below.';
  }

  Future<void> _lookup(String raw) async {
    final gtin = normalizeGtin(raw);
    if (gtin == null) {
      setState(() => _error = 'That barcode is not a valid GTIN.');
      return;
    }
    setState(() {
      _looking = true;
      _error = null;
      _result = null;
      _gtin = null;
    });

    // Stop the preview before the lookup so a code is only read once. A stop
    // failure must never block the lookup or strand the spinner: CameraX can
    // throw while the camera is being torn down on some devices.
    var cameraError = '';
    try {
      await _controller.stop();
    } catch (error) {
      cameraError = _cameraMessage(error, starting: false);
    }

    try {
      final result = await ref.read(apiProvider).barcode(gtin);
      if (!mounted) return;
      setState(() {
        _result = result;
        _gtin = gtin;
        if (cameraError.isNotEmpty) _error = cameraError;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = ApiClient.describe(error).message);
    } finally {
      // Whatever happened above, the spinner has to come back off.
      if (mounted) setState(() => _looking = false);
    }
  }

  Future<void> _reset() async {
    if (!mounted) return;
    setState(() {
      _result = null;
      _gtin = null;
      _error = null;
    });
    try {
      await _controller.start();
    } catch (error) {
      if (!mounted) return;
      setState(() => _error = _cameraMessage(error));
    }
  }

  void _draftFromBarcode(String gtin) {
    final opener = widget.draftOpener;
    if (opener != null) {
      opener(gtin);
      return;
    }
    AiDraftScreen.open(context, barcode: gtin);
  }

  void _addFromBarcode(String gtin, BarcodeSuggestion? suggested) {
    final opener = widget.addOpener;
    if (opener != null) {
      opener(gtin, suggested);
      return;
    }
    AddDrinkScreen.open(
      context,
      barcode: gtin,
      name: suggested?.name ?? '',
      brand: suggested?.brand ?? '',
      volumeMl: suggested?.volumeMl,
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Soda.night,
      appBar: AppBar(
        backgroundColor: Soda.night,
        foregroundColor: Colors.white,
        title: const Text('Scan a soda'),
        actions: [
          IconButton(
            tooltip: 'Torch',
            onPressed: () => _controller.toggleTorch(),
            icon: const Icon(Icons.flashlight_on_outlined),
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(
            child: Stack(
              fit: StackFit.expand,
              children: [
                MobileScanner(
                  controller: _controller,
                  onDetect: _onDetect,
                  placeholderBuilder: (context) => const _CameraStarting(),
                  errorBuilder: (context, error) => _CameraFailure(
                    message: _cameraMessage(error),
                    icon:
                        error.errorCode ==
                            MobileScannerErrorCode.permissionDenied
                        ? Icons.no_photography_outlined
                        : Icons.videocam_off_outlined,
                    onRetry: _reset,
                  ),
                ),
                // The lime frame means "aim the camera at a can". When the
                // camera failed there is nothing to aim, so the message gets
                // the space instead of sitting under the frame.
                if (_cameraError == null)
                  IgnorePointer(
                    child: Center(
                      child: Container(
                        key: const Key('scanner-frame'),
                        width: 280,
                        height: 160,
                        decoration: BoxDecoration(
                          border: Border.all(color: Soda.lime, width: 3),
                          borderRadius: BorderRadius.circular(16),
                        ),
                      ),
                    ),
                  ),
                if (_looking)
                  const Center(
                    child: CircularProgressIndicator(color: Soda.lime),
                  ),
              ],
            ),
          ),
          Container(
            width: double.infinity,
            color: Soda.night,
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 28),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (_error != null) ...[
                  Text(
                    _error!,
                    style: const TextStyle(color: Color(0xFFFF7A6E)),
                  ),
                  // A result card carries its own "Scan again", so the error
                  // only needs a retry action when there is nothing else on
                  // screen to restart the camera.
                  if (_result == null)
                    Align(
                      alignment: Alignment.centerLeft,
                      child: TextButton(
                        onPressed: _reset,
                        child: const Text('Try again'),
                      ),
                    ),
                ],
                if (_result != null)
                  _ResultCard(
                    result: _result!,
                    gtin: _gtin ?? '',
                    onDraft: () => _draftFromBarcode(_gtin ?? ''),
                    onManual: () =>
                        _addFromBarcode(_gtin ?? '', _result!.suggested),
                    onReset: _reset,
                  ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _manual,
                        keyboardType: TextInputType.number,
                        style: const TextStyle(color: Colors.white),
                        decoration: const InputDecoration(
                          hintText: 'Enter a barcode',
                          hintStyle: TextStyle(color: Soda.nightMuted),
                          fillColor: Soda.nightRaised,
                        ),
                        onSubmitted: _lookup,
                      ),
                    ),
                    const SizedBox(width: 8),
                    IconButton.filled(
                      onPressed: () => _lookup(_manual.text),
                      icon: const Icon(Icons.arrow_forward_rounded),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                const Text(
                  'Point the camera at the can. The code is checked against your server first.',
                  style: TextStyle(color: Soda.nightMuted, fontSize: 12),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Shown while the camera starts. The lime frame sits around this, so the
/// content stays compact and centred.
class _CameraStarting extends StatelessWidget {
  const _CameraStarting();

  @override
  Widget build(BuildContext context) {
    return const ColoredBox(
      color: Soda.night,
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            SizedBox(
              width: 26,
              height: 26,
              child: CircularProgressIndicator(
                color: Soda.lime,
                strokeWidth: 3,
              ),
            ),
            SizedBox(height: 12),
            Text(
              'Starting camera…',
              style: TextStyle(color: Soda.nightMuted, fontSize: 13),
            ),
          ],
        ),
      ),
    );
  }
}

/// Shown when the camera cannot start, with the one action that can help.
class _CameraFailure extends StatelessWidget {
  const _CameraFailure({
    required this.message,
    required this.icon,
    required this.onRetry,
  });

  final String message;
  final IconData icon;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: Soda.night,
      child: Center(
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 28),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(icon, size: 34, color: Soda.nightMuted),
              const SizedBox(height: 12),
              Text(
                message,
                textAlign: TextAlign.center,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 14,
                  height: 1.4,
                ),
              ),
              const SizedBox(height: 16),
              FilledButton.icon(
                key: const Key('scanner-camera-retry'),
                onPressed: onRetry,
                icon: const Icon(Icons.refresh_rounded, size: 18),
                label: const Text('Try again'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _ResultCard extends StatelessWidget {
  const _ResultCard({
    required this.result,
    required this.gtin,
    required this.onDraft,
    required this.onManual,
    required this.onReset,
  });

  final BarcodeResult result;

  /// The normalized GTIN that was looked up, shown as context for the draft.
  final String gtin;
  final VoidCallback onDraft;
  final VoidCallback onManual;
  final VoidCallback onReset;

  @override
  Widget build(BuildContext context) {
    if (result.matched) {
      final drink = result.drink!;
      return Card(
        color: Soda.nightRaised,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                drink.name,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 20,
                  fontWeight: FontWeight.w700,
                ),
              ),
              Text(
                drink.subtitle,
                style: const TextStyle(color: Soda.nightMuted),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: FilledButton(
                      onPressed: () async {
                        final saved = await showCheckinSheet(context, drink);
                        if (!context.mounted || saved == null) return;
                        // A scan-led check-in should end somewhere that shows
                        // what was saved, not back on the camera.
                        if (saved.id.isNotEmpty) {
                          await CheckinDetailScreen.open(context, saved.id);
                        }
                        if (context.mounted) onReset();
                      },
                      child: const Text('Log this soda'),
                    ),
                  ),
                  const SizedBox(width: 8),
                  TextButton(
                    onPressed: onReset,
                    child: const Text('Scan again'),
                  ),
                ],
              ),
            ],
          ),
        ),
      );
    }
    final code = gtin.trim();
    final suggestion = result.suggested;
    final suggestedName = suggestion?.name.trim() ?? '';
    final brand = suggestion?.brand.trim() ?? '';
    final quantity = suggestion?.quantity.trim() ?? '';
    final details = [
      if (brand.isNotEmpty) 'by $brand',
      if (quantity.isNotEmpty) quantity,
    ].join(' · ');
    final copy = suggestedName.isNotEmpty
        ? 'Open Food Facts suggests “$suggestedName”${details.isEmpty ? '' : ' $details'}. '
              'Draft it with AI, or add the details yourself.'
        : code.isNotEmpty
        ? 'Barcode $code is not in the local catalog yet. Draft it with AI, or '
              'add the details yourself.'
        : 'This barcode is not in the local catalog yet. Draft it with AI, or '
              'add the details yourself.';

    return Card(
      color: Soda.nightRaised,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              'New to Sodapop',
              style: TextStyle(
                color: Colors.white,
                fontSize: 18,
                fontWeight: FontWeight.w700,
              ),
            ),
            if (code.isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                code,
                style: const TextStyle(
                  color: Soda.nightMuted,
                  fontSize: 12,
                  letterSpacing: 0.4,
                ),
              ),
            ],
            const SizedBox(height: 6),
            Text(copy, style: const TextStyle(color: Soda.nightMuted)),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                key: const Key('scanner-draft-ai'),
                onPressed: onDraft,
                icon: const Icon(Icons.auto_awesome_rounded, size: 18),
                label: const Text('Draft it with AI'),
              ),
            ),
            const SizedBox(height: 6),
            SizedBox(
              width: double.infinity,
              child: OutlinedButton.icon(
                key: const Key('scanner-add-manual'),
                onPressed: onManual,
                style: OutlinedButton.styleFrom(
                  foregroundColor: Colors.white,
                  side: const BorderSide(color: Soda.nightMuted),
                  minimumSize: const Size(44, 48),
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(14),
                  ),
                ),
                icon: const Icon(Icons.edit_outlined, size: 18),
                label: const Text('Add it manually'),
              ),
            ),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton(
                onPressed: onReset,
                child: const Text('Scan again'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
