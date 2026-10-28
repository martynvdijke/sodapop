import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/gtin.dart';
import 'ai_draft_screen.dart';
import 'checkin_sheet.dart';

class ScannerScreen extends ConsumerStatefulWidget {
  const ScannerScreen({super.key, this.draftOpener});

  /// Optional test override for the unknown-barcode draft action. Production
  /// opens [AiDraftScreen] with the scanned code.
  final ValueChanged<String>? draftOpener;

  @override
  ConsumerState<ScannerScreen> createState() => _ScannerScreenState();
}

class _ScannerScreenState extends ConsumerState<ScannerScreen> {
  final _controller = MobileScannerController(
    detectionSpeed: DetectionSpeed.noDuplicates,
    formats: const [BarcodeFormat.ean13, BarcodeFormat.ean8, BarcodeFormat.upcA, BarcodeFormat.upcE, BarcodeFormat.code128],
  );
  final _manual = TextEditingController();
  BarcodeResult? _result;
  String? _gtin;
  String? _error;
  bool _looking = false;

  @override
  void dispose() {
    _controller.dispose();
    _manual.dispose();
    super.dispose();
  }

  Future<void> _onDetect(BarcodeCapture capture) async {
    if (_looking || _result != null) return;
    final raw = capture.barcodes.map((b) => b.rawValue).whereType<String>().firstOrNull;
    if (raw == null) return;
    await _lookup(raw);
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
    await _controller.stop();
    try {
      final result = await ref.read(apiProvider).barcode(gtin);
      if (!mounted) return;
      setState(() {
        _result = result;
        _gtin = gtin;
        _looking = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _looking = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  void _reset() {
    setState(() {
      _result = null;
      _gtin = null;
      _error = null;
    });
    _controller.start();
  }

  void _draftFromBarcode(String gtin) {
    final opener = widget.draftOpener;
    if (opener != null) {
      opener(gtin);
      return;
    }
    AiDraftScreen.open(context, barcode: gtin);
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
                MobileScanner(controller: _controller, onDetect: _onDetect),
                IgnorePointer(
                  child: Center(
                    child: Container(
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
                  const Center(child: CircularProgressIndicator(color: Soda.lime)),
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
                  Text(_error!, style: const TextStyle(color: Color(0xFFFF7A6E))),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(onPressed: _reset, child: const Text('Try again')),
                  ),
                ],
                if (_result != null)
                  _ResultCard(
                    result: _result!,
                    gtin: _gtin ?? '',
                    onDraft: () => _draftFromBarcode(_gtin ?? ''),
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

class _ResultCard extends StatelessWidget {
  const _ResultCard({
    required this.result,
    required this.gtin,
    required this.onDraft,
    required this.onReset,
  });

  final BarcodeResult result;

  /// The normalized GTIN that was looked up, shown as context for the draft.
  final String gtin;
  final VoidCallback onDraft;
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
              Text(drink.name, style: const TextStyle(color: Colors.white, fontSize: 20, fontWeight: FontWeight.w700)),
              Text(drink.subtitle, style: const TextStyle(color: Soda.nightMuted)),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: FilledButton(
                      onPressed: () async {
                        final saved = await showCheckinSheet(context, drink);
                        if (saved && context.mounted) onReset();
                      },
                      child: const Text('Log this soda'),
                    ),
                  ),
                  const SizedBox(width: 8),
                  TextButton(onPressed: onReset, child: const Text('Scan again')),
                ],
              ),
            ],
          ),
        ),
      );
    }
    final code = gtin.trim();
    final suggested = result.suggestedName.trim();
    final copy = suggested.isNotEmpty
        ? 'Open Food Facts suggests “$suggested”. Draft it with AI to shape a '
              'provisional soda, then check the details.'
        : code.isNotEmpty
        ? 'Barcode $code is not in the local catalog yet. Draft it with AI to '
              'shape a provisional soda from the code.'
        : 'This barcode is not in the local catalog yet. Draft it with AI to '
              'shape a provisional soda from what you know.';

    return Card(
      color: Soda.nightRaised,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('New to Sodapop', style: TextStyle(color: Colors.white, fontSize: 18, fontWeight: FontWeight.w700)),
            if (code.isNotEmpty) ...[
              const SizedBox(height: 2),
              Text(
                code,
                style: const TextStyle(color: Soda.nightMuted, fontSize: 12, letterSpacing: 0.4),
              ),
            ],
            const SizedBox(height: 6),
            Text(copy, style: const TextStyle(color: Soda.nightMuted)),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: FilledButton.icon(
                    key: const Key('scanner-draft-ai'),
                    onPressed: onDraft,
                    icon: const Icon(Icons.auto_awesome_rounded, size: 18),
                    label: const Text('Draft it with AI'),
                  ),
                ),
                const SizedBox(width: 8),
                TextButton(onPressed: onReset, child: const Text('Scan again')),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
