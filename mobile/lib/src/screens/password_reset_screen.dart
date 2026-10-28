import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';

/// Requests a password reset link for the connected server.
///
/// The confirmation is deliberately the same whether or not the address has an
/// account, so the screen cannot be used to discover who is registered.
class PasswordResetScreen extends ConsumerStatefulWidget {
  const PasswordResetScreen({super.key});

  static Future<void> open(BuildContext context) {
    return Navigator.of(context).push<void>(
      MaterialPageRoute<void>(builder: (_) => const PasswordResetScreen()),
    );
  }

  @override
  ConsumerState<PasswordResetScreen> createState() =>
      _PasswordResetScreenState();
}

class _PasswordResetScreenState extends ConsumerState<PasswordResetScreen> {
  static final _emailPattern = RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$');

  final _formKey = GlobalKey<FormState>();
  final _email = TextEditingController();
  final _emailFocus = FocusNode();

  bool _submitted = false;
  bool _sending = false;
  bool _sent = false;
  String? _notice;
  String _address = '';

  @override
  void dispose() {
    _email.dispose();
    _emailFocus.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_sending) return;
    setState(() => _submitted = true);
    if (!(_formKey.currentState?.validate() ?? false)) return;

    final email = _email.text.trim();
    FocusScope.of(context).unfocus();
    setState(() {
      _sending = true;
      _notice = null;
    });
    try {
      await ref.read(apiProvider).requestPasswordReset(email);
      if (!mounted) return;
      setState(() {
        _sending = false;
        _sent = true;
        _address = email;
      });
    } catch (error) {
      if (!mounted) return;
      final failure = ApiClient.describe(error);
      setState(() {
        _sending = false;
        _sent = true;
        _address = email;
        _notice = failure.statusCode == 429
            ? 'Too many attempts. Wait a few minutes and try again.'
            : 'The request did not go through just now. You can try again in a moment.';
      });
    }
  }

  void _retry() {
    setState(() {
      _sent = false;
      _submitted = false;
      _notice = null;
    });
    _email.clear();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) _emailFocus.requestFocus();
    });
  }

  String? _validateEmail(String? value) {
    final email = (value ?? '').trim();
    if (email.isEmpty) return 'Enter your email address.';
    if (!_emailPattern.hasMatch(email)) return 'Enter a valid email address.';
    return null;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final dark = theme.brightness == Brightness.dark;
    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          onPressed: () => Navigator.of(context).pop(),
          tooltip: 'Back to sign in',
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: Stack(
        fit: StackFit.expand,
        children: [
          Positioned(
            top: -110,
            right: -70,
            child: IgnorePointer(
              child: Container(
                width: 260,
                height: 260,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: Soda.tangerine.withValues(alpha: dark ? 0.12 : 0.14),
                ),
              ),
            ),
          ),
          SafeArea(
            top: false,
            child: _sent ? _confirmation(theme) : _form(theme),
          ),
        ],
      ),
    );
  }

  Widget _form(ThemeData theme) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 4, 24, 40),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      children: [
        _medallion(Icons.lock_reset_rounded),
        const SizedBox(height: 20),
        Text(
          'Reset your password',
          style: theme.textTheme.headlineMedium?.copyWith(
            fontWeight: FontWeight.w800,
            letterSpacing: -0.6,
            height: 1.1,
          ),
        ),
        const SizedBox(height: 8),
        Text(
          'Enter the email address for your account. We will send a link to choose a new password.',
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 20),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Form(
              key: _formKey,
              autovalidateMode: _submitted
                  ? AutovalidateMode.onUserInteraction
                  : AutovalidateMode.disabled,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  _label('Email'),
                  TextFormField(
                    key: const Key('reset-email'),
                    controller: _email,
                    focusNode: _emailFocus,
                    keyboardType: TextInputType.emailAddress,
                    autocorrect: false,
                    enableSuggestions: false,
                    textInputAction: TextInputAction.done,
                    validator: _validateEmail,
                    onFieldSubmitted: (_) => _submit(),
                    decoration: const InputDecoration(
                      hintText: 'you@example.com',
                    ),
                  ),
                  const SizedBox(height: 20),
                  FilledButton(
                    key: const Key('reset-submit'),
                    onPressed: _sending ? null : _submit,
                    child: _sending
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('Send reset link'),
                  ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 16),
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              Icons.open_in_new_rounded,
              size: 16,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                "The link opens your server's web page, where you choose a new password.",
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ),
          ],
        ),
      ],
    );
  }

  Widget _confirmation(ThemeData theme) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 4, 24, 40),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      children: [
        _medallion(Icons.mark_email_read_rounded),
        const SizedBox(height: 20),
        Text(
          'Check your email',
          style: theme.textTheme.headlineMedium?.copyWith(
            fontWeight: FontWeight.w800,
            letterSpacing: -0.6,
            height: 1.1,
          ),
        ),
        const SizedBox(height: 12),
        Text(
          'If that address has an account, a reset link is on its way.',
          style: theme.textTheme.bodyLarge?.copyWith(
            fontWeight: FontWeight.w600,
          ),
        ),
        const SizedBox(height: 12),
        _addressChip(theme, _address),
        const SizedBox(height: 12),
        Text(
          "The link opens your server's web page to set a new password.",
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        if (_notice != null) ...[
          const SizedBox(height: 16),
          _noticeBox(_notice!),
        ],
        const SizedBox(height: 24),
        OutlinedButton(
          key: const Key('reset-retry'),
          onPressed: _retry,
          child: const Text('Use a different email'),
        ),
        const SizedBox(height: 8),
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          style: TextButton.styleFrom(
            foregroundColor: theme.colorScheme.onSurface,
          ),
          child: const Text('Back to sign in'),
        ),
      ],
    );
  }

  Widget _medallion(IconData icon) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: 64,
      height: 64,
      decoration: BoxDecoration(color: scheme.primary, shape: BoxShape.circle),
      child: Icon(icon, size: 30, color: scheme.onPrimary),
    );
  }

  Widget _label(String text) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Text(
        text,
        style: Theme.of(
          context,
        ).textTheme.labelLarge?.copyWith(fontWeight: FontWeight.w700),
      ),
    );
  }

  Widget _addressChip(ThemeData theme, String address) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.12),
        ),
      ),
      child: Text(
        address,
        style: theme.textTheme.bodyMedium?.copyWith(
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }

  Widget _noticeBox(String message) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondary.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.cloud_off_rounded,
            size: 18,
            color: theme.colorScheme.onSurface,
          ),
          const SizedBox(width: 8),
          Expanded(child: Text(message, style: theme.textTheme.bodySmall)),
        ],
      ),
    );
  }
}
