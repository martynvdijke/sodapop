import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/public_preview.dart';
import 'public_checkin_screen.dart';
import 'public_profile_screen.dart';
import 'public_shell.dart';

/// Newest public pours. Reads the unauthenticated preview endpoint only.
class PublicFeedScreen extends ConsumerStatefulWidget {
  const PublicFeedScreen({super.key});

  @override
  ConsumerState<PublicFeedScreen> createState() => _PublicFeedScreenState();
}

class _PublicFeedScreenState extends ConsumerState<PublicFeedScreen> {
  List<FeedItem> _items = const [];
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
      final items = await ref.read(apiProvider).publicFeed(limit: 30);
      if (!mounted) return;
      setState(() {
        _items = items;
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
        title: const Text('Public pours'),
        actions: [
          IconButton(
            onPressed: _load,
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh_rounded),
          ),
          const PreviewSignInButton(),
        ],
      ),
      body: RefreshIndicator(
        color: Soda.lime,
        onRefresh: _load,
        child: _body(context),
      ),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator(color: Soda.lime));
    }
    if (_error != null) {
      return ListView(
        children: [
          const SizedBox(height: 80),
          Center(child: Text(_error!, textAlign: TextAlign.center)),
          Center(
            child: TextButton(onPressed: _load, child: const Text('Try again')),
          ),
        ],
      );
    }
    if (_items.isEmpty) {
      return ListView(
        children: const [
          SizedBox(height: 100),
          Icon(Icons.local_drink_outlined, size: 56, color: Soda.muted),
          SizedBox(height: 12),
          Center(
            child: Text(
              'No public pours yet.\nSign in and be the first.',
              textAlign: TextAlign.center,
            ),
          ),
        ],
      );
    }
    final api = ref.read(apiProvider);
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: _items.length,
      separatorBuilder: (_, _) => const SizedBox(height: 12),
      itemBuilder: (context, index) {
        final item = _items[index];
        return PublicCheckinCard(
          item: item,
          api: api,
          onTap: () => PublicCheckinScreen.open(context, item.id),
          onAuthorTap: item.handle.isEmpty
              ? null
              : () => PublicProfileScreen.open(context, item.handle),
        );
      },
    );
  }
}
