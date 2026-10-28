import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';

enum _Stage { form, inbox, created }

/// Creates a local account on the connected server.
///
/// Pops `true` once an account exists so the caller can reveal the password
/// sign-in controls; pops `false` when the visitor backs out of the form.
class RegisterScreen extends ConsumerStatefulWidget {
  const RegisterScreen({super.key});

  static Future<bool?> open(BuildContext context) {
    return Navigator.of(context).push<bool>(
      MaterialPageRoute<bool>(builder: (_) => const RegisterScreen()),
    );
  }

  @override
  ConsumerState<RegisterScreen> createState() => _RegisterScreenState();
}

class _RegisterScreenState extends ConsumerState<RegisterScreen> {
  static final _handlePattern = RegExp(r'^[a-z0-9_]{3,24}$');
  static final _emailPattern = RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$');

  final _formKey = GlobalKey<FormState>();
  final _displayName = TextEditingController();
  final _handle = TextEditingController();
  final _email = TextEditingController();
  final _password = TextEditingController();
  final _confirm = TextEditingController();

  Timer? _cooldownTimer;
  _Stage _stage = _Stage.form;
  bool _showPassword = false;
  bool _submitted = false;
  bool _sending = false;
  bool _resending = false;
  int _cooldown = 0;
  String? _error;
  String? _resendNotice;
  String _address = '';

  @override
  void dispose() {
    _cooldownTimer?.cancel();
    _displayName.dispose();
    _handle.dispose();
    _email.dispose();
    _password.dispose();
    _confirm.dispose();
    super.dispose();
  }

  static String _normalizeHandle(String value) =>
      value.trim().replaceFirst(RegExp(r'^@+'), '').toLowerCase();

  void _close() {
    if (!mounted) return;
    Navigator.of(context).pop(_stage != _Stage.form);
  }

  Future<void> _submit() async {
    if (_sending) return;
    FocusScope.of(context).unfocus();
    setState(() => _submitted = true);
    if (!(_formKey.currentState?.validate() ?? false)) return;

    final email = _email.text.trim();
    setState(() {
      _sending = true;
      _error = null;
    });
    try {
      final result = await ref
          .read(apiProvider)
          .register(
            email: email,
            handle: _normalizeHandle(_handle.text),
            displayName: _displayName.text.trim(),
            password: _password.text,
          );
      if (!mounted) return;
      setState(() {
        _sending = false;
        _address = email;
        _stage = result.verificationRequired ? _Stage.inbox : _Stage.created;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _sending = false;
        _error = _registerError(error);
      });
    }
  }

  Future<void> _resend() async {
    if (_cooldown > 0 || _resending) return;
    setState(() {
      _resending = true;
      _resendNotice = null;
    });
    try {
      await ref.read(apiProvider).resendVerification(_address);
      if (!mounted) return;
      setState(() {
        _resending = false;
        _resendNotice =
            'If that address needs verification, a new email is on its way.';
      });
      _startCooldown();
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _resending = false;
        _resendNotice = 'The server did not answer. Try again in a moment.';
      });
    }
  }

  void _startCooldown() {
    _cooldownTimer?.cancel();
    setState(() => _cooldown = 60);
    _cooldownTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) {
        timer.cancel();
        return;
      }
      if (_cooldown <= 1) {
        timer.cancel();
        setState(() => _cooldown = 0);
      } else {
        setState(() => _cooldown -= 1);
      }
    });
  }

  /// Turns transport failures into plain copy without naming a field, so a
  /// conflict never confirms which of the two values already exists.
  static String _registerError(Object error) {
    final failure = ApiClient.describe(error);
    final text = failure.message.toLowerCase();
    if (failure.statusCode == 409) {
      return 'That handle or email is already in use.';
    }
    if (failure.statusCode == 403) {
      if (text.contains('closed')) {
        return 'Registration is closed on this server.';
      }
      if (text.contains('email')) {
        return 'That email address is not allowed on this server.';
      }
      if (text.contains('password')) {
        return 'Password sign-in is turned off on this server.';
      }
      return 'Registration is not available on this server.';
    }
    if (failure.statusCode == 400) {
      return 'Check your details and try again.';
    }
    if (failure.statusCode == 429) {
      return 'Too many attempts. Wait a few minutes and try again.';
    }
    return _sentence(failure.message);
  }

  static String _sentence(String text) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) return 'Something went wrong. Try again.';
    final capitalized = trimmed[0].toUpperCase() + trimmed.substring(1);
    return capitalized.endsWith('.') || capitalized.endsWith('?')
        ? capitalized
        : '$capitalized.';
  }

  String? _validateDisplayName(String? value) {
    if ((value ?? '').trim().length > 80) {
      return 'Keep this to 80 characters or fewer.';
    }
    return null;
  }

  String? _validateHandle(String? value) {
    final handle = _normalizeHandle(value ?? '');
    if (handle.isEmpty) return 'Pick a handle.';
    if (!_handlePattern.hasMatch(handle)) {
      return 'Use 3 to 24 lowercase letters, numbers or underscores.';
    }
    return null;
  }

  String? _validateEmail(String? value) {
    final email = (value ?? '').trim();
    if (email.isEmpty) return 'Enter your email address.';
    if (!_emailPattern.hasMatch(email)) return 'Enter a valid email address.';
    return null;
  }

  String? _validatePassword(String? value) {
    if ((value ?? '').length < 10) return 'Use at least 10 characters.';
    return null;
  }

  String? _validateConfirm(String? value) {
    if (value != _password.text) return 'Passwords do not match.';
    return null;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final dark = theme.brightness == Brightness.dark;
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) return;
        _close();
      },
      child: Scaffold(
        appBar: AppBar(
          leading: IconButton(
            onPressed: _close,
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
                    color: Soda.lime.withValues(alpha: dark ? 0.1 : 0.18),
                  ),
                ),
              ),
            ),
            SafeArea(
              top: false,
              child: switch (_stage) {
                _Stage.form => _formBody(theme),
                _Stage.inbox => _inboxBody(theme),
                _Stage.created => _createdBody(theme),
              },
            ),
          ],
        ),
      ),
    );
  }

  Widget _formBody(ThemeData theme) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 4, 24, 40),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      children: [
        _medallion(Icons.person_add_alt_1_rounded),
        const SizedBox(height: 20),
        Text(
          'Create your account',
          style: theme.textTheme.headlineMedium?.copyWith(
            fontWeight: FontWeight.w800,
            letterSpacing: -0.6,
            height: 1.1,
          ),
        ),
        const SizedBox(height: 8),
        Text(
          'This account lives on your own server. Pick a handle and confirm your email to finish.',
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
                  if (_error != null) ...[
                    _errorBanner(_error!),
                    const SizedBox(height: 16),
                  ],
                  _label('Display name'),
                  TextFormField(
                    key: const Key('register-display-name'),
                    controller: _displayName,
                    textCapitalization: TextCapitalization.words,
                    textInputAction: TextInputAction.next,
                    inputFormatters: [LengthLimitingTextInputFormatter(80)],
                    validator: _validateDisplayName,
                    decoration: const InputDecoration(
                      hintText: 'Optional. Shown on your profile.',
                    ),
                  ),
                  const SizedBox(height: 16),
                  _label('Handle'),
                  TextFormField(
                    key: const Key('register-handle'),
                    controller: _handle,
                    autocorrect: false,
                    enableSuggestions: false,
                    textInputAction: TextInputAction.next,
                    validator: _validateHandle,
                    decoration: const InputDecoration(
                      hintText: 'ada_lovelace',
                      helperText:
                          'Lowercase letters, numbers or underscores, 3 to 24.',
                    ),
                  ),
                  const SizedBox(height: 16),
                  _label('Email'),
                  TextFormField(
                    key: const Key('register-email'),
                    controller: _email,
                    keyboardType: TextInputType.emailAddress,
                    autocorrect: false,
                    enableSuggestions: false,
                    textInputAction: TextInputAction.next,
                    validator: _validateEmail,
                    decoration: const InputDecoration(
                      hintText: 'you@example.com',
                    ),
                  ),
                  const SizedBox(height: 16),
                  _label('Password'),
                  TextFormField(
                    key: const Key('register-password'),
                    controller: _password,
                    obscureText: !_showPassword,
                    autocorrect: false,
                    enableSuggestions: false,
                    textInputAction: TextInputAction.next,
                    validator: _validatePassword,
                    decoration: InputDecoration(
                      hintText: 'At least 10 characters',
                      suffixIcon: IconButton(
                        onPressed: () =>
                            setState(() => _showPassword = !_showPassword),
                        tooltip: _showPassword
                            ? 'Hide password'
                            : 'Show password',
                        icon: Icon(
                          _showPassword
                              ? Icons.visibility_off_rounded
                              : Icons.visibility_rounded,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  _label('Confirm password'),
                  TextFormField(
                    key: const Key('register-confirm'),
                    controller: _confirm,
                    obscureText: !_showPassword,
                    autocorrect: false,
                    enableSuggestions: false,
                    textInputAction: TextInputAction.done,
                    validator: _validateConfirm,
                    onFieldSubmitted: (_) => _submit(),
                    decoration: const InputDecoration(
                      hintText: 'Type it once more',
                    ),
                  ),
                  const SizedBox(height: 20),
                  FilledButton(
                    key: const Key('register-submit'),
                    onPressed: _sending ? null : _submit,
                    child: _sending
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('Create account'),
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
              Icons.lock_outline_rounded,
              size: 16,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                'Your email is only used to verify this account and reset its password.',
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

  Widget _inboxBody(ThemeData theme) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 4, 24, 40),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      children: [
        _medallion(Icons.mark_email_read_rounded),
        const SizedBox(height: 20),
        Text(
          'Check your inbox',
          style: theme.textTheme.headlineMedium?.copyWith(
            fontWeight: FontWeight.w800,
            letterSpacing: -0.6,
            height: 1.1,
          ),
        ),
        const SizedBox(height: 12),
        Text(
          'We sent a verification link to',
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 8),
        _addressChip(theme, _address),
        const SizedBox(height: 12),
        Text(
          "Open the link to finish setting up your account. The link opens your server's web page.",
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 24),
        Card(
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  'Did not get it? Check your spam folder, then ask for another.',
                  style: theme.textTheme.bodyMedium,
                ),
                if (_resendNotice != null) ...[
                  const SizedBox(height: 12),
                  _notice(_resendNotice!),
                ],
                const SizedBox(height: 16),
                OutlinedButton.icon(
                  key: const Key('register-resend'),
                  onPressed: _cooldown > 0 || _resending ? null : _resend,
                  icon: const Icon(Icons.refresh_rounded),
                  label: Text(
                    _cooldown > 0 ? 'Resend in ${_cooldown}s' : 'Resend email',
                  ),
                ),
              ],
            ),
          ),
        ),
        const SizedBox(height: 12),
        FilledButton(
          key: const Key('register-back-to-sign-in'),
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('Back to sign in'),
        ),
      ],
    );
  }

  Widget _createdBody(ThemeData theme) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(24, 4, 24, 40),
      keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
      children: [
        _medallion(Icons.check_rounded),
        const SizedBox(height: 20),
        Text(
          'Account created',
          style: theme.textTheme.headlineMedium?.copyWith(
            fontWeight: FontWeight.w800,
            letterSpacing: -0.6,
            height: 1.1,
          ),
        ),
        const SizedBox(height: 10),
        Text(
          'Your account is ready. Sign in with your email and password.',
          style: theme.textTheme.bodyMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: 24),
        FilledButton(
          key: const Key('register-continue'),
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('Continue to sign in'),
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

  Widget _notice(String message) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.primary.withValues(alpha: 0.18),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.check_circle_outline_rounded,
            size: 18,
            color: theme.colorScheme.onSurface,
          ),
          const SizedBox(width: 8),
          Expanded(child: Text(message, style: theme.textTheme.bodySmall)),
        ],
      ),
    );
  }

  Widget _errorBanner(String message) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.error.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.error_outline_rounded,
            size: 18,
            color: theme.colorScheme.error,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.error,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
