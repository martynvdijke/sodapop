import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/public_preview.dart';
import 'public_profile_screen.dart';
import 'public_shell.dart';

/// One public check-in, rendered without any viewer capabilities.
class PublicCheckinScreen extends ConsumerStatefulWidget {
  const PublicCheckinScreen({super.key, required this.id});

  final String id;

  static Future<void> open(BuildContext context, String id) => Navigator.of(
    context,
  ).push(MaterialPageRoute(builder: (_) => PublicCheckinScreen(id: id)));

  @override
  ConsumerState<PublicCheckinScreen> createState() =>
      _PublicCheckinScreenState();
}

class _PublicCheckinScreenState extends ConsumerState<PublicCheckinScreen> {
  FeedItem? _item;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final item = await ref.read(apiProvider).publicCheckin(widget.id);
      if (!mounted) return;
      setState(() {
        _item = item;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Public pour'),
        actions: const [PreviewSignInButton()],
      ),
      body: _body(context),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator(color: Soda.lime));
    }
    final item = _item;
    if (_error != null || item == null) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(_error ?? 'Check-in not found'),
            const SizedBox(height: 8),
            TextButton(onPressed: _load, child: const Text('Try again')),
          ],
        ),
      );
    }
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        PublicCheckinCard(
          item: item,
          api: ref.read(apiProvider),
          onAuthorTap: item.handle.isEmpty
              ? null
              : () => PublicProfileScreen.open(context, item.handle),
        ),
        const SizedBox(height: 12),
        Text(
          'Sign in to toast, save, or comment.',
          textAlign: TextAlign.center,
          style: Theme.of(context).textTheme.bodySmall?.copyWith(
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }
}
