import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../api/api_client.dart';
import '../theme/soda_theme.dart';

/// Hosts the Authelia sign-in flow. The Sodapop session cookie is HttpOnly, so
/// it is read through the native Android CookieManager bridge instead of JS.
class OidcWebViewScreen extends StatefulWidget {
  const OidcWebViewScreen({super.key, required this.serverUrl});

  final String serverUrl;

  @override
  State<OidcWebViewScreen> createState() => _OidcWebViewScreenState();
}

class _OidcWebViewScreenState extends State<OidcWebViewScreen> {
  static const _channel = MethodChannel('xyz.vandijke.sodapop/cookies');

  late final WebViewController _controller;
  bool _capturing = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageFinished: (url) => _maybeCapture(url),
          onWebResourceError: (error) {
            if (error.isForMainFrame ?? true) {
              setState(() => _error = error.description);
            }
          },
        ),
      )
      ..loadRequest(Uri.parse('${widget.serverUrl}/auth/oidc/login'));
  }

  bool _isAppUrl(String url) {
    final uri = Uri.tryParse(url);
    final base = Uri.parse(widget.serverUrl);
    return uri != null &&
        uri.scheme == base.scheme &&
        uri.host == base.host &&
        uri.port == base.port &&
        !uri.path.startsWith('/auth/oidc');
  }

  Future<void> _maybeCapture(String url) async {
    if (_capturing || !_isAppUrl(url)) return;
    _capturing = true;
    try {
      final cookie = await _channel.invokeMethod<String>('getCookies', {'url': widget.serverUrl});
      if (cookie == null || !cookie.contains('sd_session=')) {
        _capturing = false;
        return;
      }
      // Every signed-in page renders a CSRF hidden input in the logout form.
      final raw = await _controller.runJavaScriptReturningResult(
        "document.querySelector('input[name=csrf_token]')?.value || ''",
      );
      final csrf = raw.toString().replaceAll('"', '');
      if (csrf.isEmpty) {
        throw ApiException('Signed in, but the security token was not found.');
      }
      if (!mounted) return;
      Navigator.of(context).pop<Map<String, String>>({'cookie': cookie, 'csrf': csrf});
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _capturing = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Sign in with Authelia')),
      body: Column(
        children: [
          if (_error != null)
            Container(
              width: double.infinity,
              color: Theme.of(context).colorScheme.errorContainer,
              padding: const EdgeInsets.all(12),
              child: Text(_error!),
            ),
          Expanded(child: WebViewWidget(controller: _controller)),
          if (_capturing)
            const Padding(
              padding: EdgeInsets.all(16),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2, color: Soda.lime),
                  ),
                  SizedBox(width: 12),
                  Text('Finishing sign-in…'),
                ],
              ),
            ),
        ],
      ),
    );
  }
}
