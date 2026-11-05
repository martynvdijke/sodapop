import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../widgets/public_preview.dart';
import 'public_profile_screen.dart';
import 'public_shell.dart';

/// Directory of people who opted into a public profile.
class PublicPeopleScreen extends ConsumerStatefulWidget {
  const PublicPeopleScreen({super.key});

  @override
  ConsumerState<PublicPeopleScreen> createState() => _PublicPeopleScreenState();
}

class _PublicPeopleScreenState extends ConsumerState<PublicPeopleScreen> {
  List<PublicPerson> _people = const [];
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
      final people = await ref.read(apiProvider).publicPeople(limit: 60);
      if (!mounted) return;
      setState(() {
        _people = people;
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
        title: const Text('Public people'),
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
    if (_people.isEmpty) {
      return ListView(
        children: const [
          SizedBox(height: 100),
          Icon(Icons.people_outline, size: 56, color: Soda.muted),
          SizedBox(height: 12),
          Center(
            child: Text(
              'Nobody has opened their profile yet.',
              textAlign: TextAlign.center,
            ),
          ),
        ],
      );
    }
    return ListView.separated(
      padding: const EdgeInsets.all(16),
      itemCount: _people.length,
      separatorBuilder: (_, _) => const SizedBox(height: 12),
      itemBuilder: (context, index) {
        final person = _people[index];
        return PublicPersonTile(
          person: person,
          onTap: () => PublicProfileScreen.open(context, person.handle),
        );
      },
    );
  }
}
