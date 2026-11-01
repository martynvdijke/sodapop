import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';

/// Shows the server's own legal pages inside the app. They are the same HTML
/// the web footer links to, so a policy change never has to be shipped twice.
class LegalScreen extends StatefulWidget {
  const LegalScreen({super.key, required this.title, required this.url});

  final String title;
  final String url;

  static Future<void> open(
    BuildContext context, {
    required String title,
    required String url,
  }) {
    return Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => LegalScreen(title: title, url: url),
      ),
    );
  }

  @override
  State<LegalScreen> createState() => _LegalScreenState();
}

class _LegalScreenState extends State<LegalScreen> {
  late final WebViewController _controller;

  @override
  void initState() {
    super.initState();
    // These pages are static text, so scripts stay off.
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.disabled)
      ..loadRequest(Uri.parse(widget.url));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(widget.title)),
      body: WebViewWidget(controller: _controller),
    );
  }
}
