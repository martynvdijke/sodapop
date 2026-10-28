import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/auth_controller.dart';
import '../auth/oidc_webview_screen.dart';
import '../theme/soda_theme.dart';
import 'password_reset_screen.dart';
import 'register_screen.dart';

class OnboardingScreen extends ConsumerStatefulWidget {
  const OnboardingScreen({super.key});

  @override
  ConsumerState<OnboardingScreen> createState() => _OnboardingScreenState();
}

class _OnboardingScreenState extends ConsumerState<OnboardingScreen> {
  final _server = TextEditingController();
  final _token = TextEditingController();
  final _login = TextEditingController();
  final _password = TextEditingController();
  bool _showToken = false;
  bool _showPassword = false;
  bool _connected = false;

  @override
  void initState() {
    super.initState();
    _server.text = ref.read(authProvider).serverUrl;
    _connected = _server.text.isNotEmpty;
  }

  @override
  void dispose() {
    _server.dispose();
    _token.dispose();
    _login.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    final ok = await ref.read(authProvider.notifier).connect(_server.text);
    if (ok && mounted) setState(() => _connected = true);
  }

  Future<void> _oidc() async {
    final result = await Navigator.of(context).push<Map<String, String>>(
      MaterialPageRoute(builder: (_) => OidcWebViewScreen(serverUrl: ref.read(authProvider).serverUrl)),
    );
    if (result == null || !mounted) return;
    await ref.read(authProvider.notifier).completeOidc(result['cookie']!, result['csrf']!);
  }

  Future<void> _openRegister() async {
    final created = await RegisterScreen.open(context);
    if (!mounted || created != true) return;
    setState(() => _showPassword = true);
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(
        content: Text('Account created. Sign in with your email and password.'),
      ),
    );
  }

  Future<void> _openReset() => PasswordResetScreen.open(context);

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    final theme = Theme.of(context);
    final link = TextButton.styleFrom(
      foregroundColor: theme.colorScheme.onSurface,
      textStyle: const TextStyle(fontWeight: FontWeight.w700),
      visualDensity: VisualDensity.compact,
      padding: const EdgeInsets.symmetric(horizontal: 8),
    );
    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(24, 40, 24, 32),
          children: [
            Container(
              width: 64,
              height: 64,
              decoration: const BoxDecoration(color: Soda.lime, shape: BoxShape.circle),
              child: const Icon(Icons.bolt_rounded, color: Soda.forest, size: 36),
            ),
            const SizedBox(height: 24),
            Text('Sodapop', style: theme.textTheme.displaySmall?.copyWith(fontWeight: FontWeight.w800)),
            const SizedBox(height: 8),
            Text(
              'A quiet place for loud fizz. Scan a can, rate it, and see what your friends are drinking.',
              style: theme.textTheme.bodyLarge?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
            const SizedBox(height: 32),
            Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Your server', style: theme.textTheme.titleMedium),
                    const SizedBox(height: 12),
                    TextField(
                      controller: _server,
                      keyboardType: TextInputType.url,
                      autocorrect: false,
                      decoration: const InputDecoration(hintText: 'https://soda.example.com'),
                      onSubmitted: (_) => _connect(),
                    ),
                    const SizedBox(height: 12),
                    SizedBox(
                      width: double.infinity,
                      child: OutlinedButton.icon(
                        onPressed: auth.busy ? null : _connect,
                        icon: const Icon(Icons.cable_rounded),
                        label: Text(_connected ? 'Check again' : 'Connect'),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            if (_connected) ...[
              const SizedBox(height: 20),
              if (auth.oidcEnabled) ...[
                FilledButton.icon(
                  onPressed: auth.busy ? null : _oidc,
                  icon: const Icon(Icons.lock_open_rounded),
                  label: const Text('Sign in with Authelia'),
                ),
                const SizedBox(height: 12),
              ],
              if (auth.passwordFallback) ...[
                OutlinedButton(
                  onPressed: () => setState(() => _showPassword = !_showPassword),
                  child: const Text('Use email and password'),
                ),
                const SizedBox(height: 4),
                Wrap(
                  alignment: WrapAlignment.center,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    Text(
                      'New here?',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                    TextButton(
                      onPressed: auth.busy ? null : _openRegister,
                      style: link,
                      child: const Text('Create an account'),
                    ),
                    TextButton(
                      onPressed: auth.busy ? null : _openReset,
                      style: link,
                      child: const Text('Forgot password?'),
                    ),
                  ],
                ),
                if (_showPassword) ...[
                  const SizedBox(height: 12),
                  TextField(
                    controller: _login,
                    decoration: const InputDecoration(hintText: 'Email or handle'),
                  ),
                  const SizedBox(height: 8),
                  TextField(
                    controller: _password,
                    obscureText: true,
                    decoration: const InputDecoration(hintText: 'Password'),
                  ),
                  const SizedBox(height: 8),
                  FilledButton(
                    onPressed: auth.busy
                        ? null
                        : () => ref
                            .read(authProvider.notifier)
                            .signInWithPassword(_login.text, _password.text),
                    child: const Text('Sign in'),
                  ),
                ],
              ],
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: () => setState(() => _showToken = !_showToken),
                child: const Text('Use a personal API token'),
              ),
              if (_showToken) ...[
                const SizedBox(height: 12),
                TextField(
                  controller: _token,
                  autocorrect: false,
                  decoration: const InputDecoration(hintText: 'sd_…'),
                ),
                const SizedBox(height: 8),
                FilledButton(
                  onPressed: auth.busy
                      ? null
                      : () => ref.read(authProvider.notifier).signInWithToken(_token.text),
                  child: const Text('Use token'),
                ),
              ],
            ],
            if (auth.error != null) ...[
              const SizedBox(height: 16),
              Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: theme.colorScheme.errorContainer,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(auth.error!, style: TextStyle(color: theme.colorScheme.onErrorContainer)),
              ),
            ],
            const SizedBox(height: 24),
            Text(
              'Tokens are stored in Android encrypted storage. The app never sends your password anywhere except your own server.',
              style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
          ],
        ),
      ),
    );
  }
}
