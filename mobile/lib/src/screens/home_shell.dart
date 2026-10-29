import 'dart:async';

import 'package:flutter/material.dart';

import '../push/push_service.dart';
import 'discover_screen.dart';
import 'feed_screen.dart';
import 'notifications_screen.dart';
import 'profile_screen.dart';
import 'scanner_screen.dart';

class HomeShell extends StatefulWidget {
  const HomeShell({super.key});

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  int _index = 0;
  StreamSubscription<void>? _pushSub;

  static const _pages = [FeedScreen(), ScannerScreen(), DiscoverScreen(), ProfileScreen()];

  @override
  void initState() {
    super.initState();
    // Foreground notification messages are not drawn by Android; surface one
    // while the shell is alive and let the notifications screen refresh itself.
    _pushSub = PushService.instance.onMessage.listen((_) => _announce());
  }

  @override
  void dispose() {
    _pushSub?.cancel();
    super.dispose();
  }

  void _announce() {
    if (!mounted) return;
    final messenger = ScaffoldMessenger.maybeOf(context);
    messenger
      ?..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: const Text('New notification'),
          action: SnackBarAction(
            label: 'View',
            onPressed: () => NotificationsScreen.open(context),
          ),
        ),
      );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(index: _index, children: _pages),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (value) => setState(() => _index = value),
        destinations: const [
          NavigationDestination(icon: Icon(Icons.people_alt_outlined), selectedIcon: Icon(Icons.people_alt), label: 'Friends'),
          NavigationDestination(icon: Icon(Icons.qr_code_scanner_rounded), label: 'Scan'),
          NavigationDestination(icon: Icon(Icons.search_rounded), label: 'Discover'),
          NavigationDestination(icon: Icon(Icons.person_outline_rounded), selectedIcon: Icon(Icons.person), label: 'You'),
        ],
      ),
    );
  }
}
