import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/auth_controller.dart';
import 'public_feed_screen.dart';
import 'public_people_screen.dart';

/// The logged-out shell. It exposes only the read-only public preview; every
/// write affordance remains behind the sign-in screen.
class PublicShell extends ConsumerStatefulWidget {
  const PublicShell({super.key});

  @override
  ConsumerState<PublicShell> createState() => _PublicShellState();
}

class _PublicShellState extends ConsumerState<PublicShell> {
  int _index = 0;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(
        index: _index,
        children: const [PublicFeedScreen(), PublicPeopleScreen()],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (value) => setState(() => _index = value),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.local_drink_outlined),
            selectedIcon: Icon(Icons.local_drink),
            label: 'Public',
          ),
          NavigationDestination(
            icon: Icon(Icons.people_alt_outlined),
            selectedIcon: Icon(Icons.people_alt),
            label: 'People',
          ),
        ],
      ),
    );
  }
}

/// App bar action shared by the preview screens. It leaves the preview and
/// returns to onboarding, where the same server is preselected.
class PreviewSignInButton extends ConsumerWidget {
  const PreviewSignInButton({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return TextButton(
      onPressed: () => ref.read(authProvider.notifier).stopPreview(),
      child: const Text('Sign in'),
    );
  }
}
