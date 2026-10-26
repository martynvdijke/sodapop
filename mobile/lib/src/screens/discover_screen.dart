import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import 'checkin_sheet.dart';
import 'venues_screen.dart';

class DiscoverScreen extends ConsumerStatefulWidget {
  const DiscoverScreen({super.key});

  @override
  ConsumerState<DiscoverScreen> createState() => _DiscoverScreenState();
}

class _DiscoverScreenState extends ConsumerState<DiscoverScreen> {
  final _query = TextEditingController();
  List<Drink> _results = const [];
  bool _searching = false;
  bool _searched = false;
  String? _error;

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _search() async {
    final value = _query.text.trim();
    if (value.length < 2) {
      setState(() => _error = 'Type at least two characters.');
      return;
    }
    setState(() {
      _searching = true;
      _error = null;
      _searched = true;
    });
    try {
      final results = await ref.read(apiProvider).search(value);
      if (!mounted) return;
      setState(() {
        _results = results;
        _searching = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _searching = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('Discover'),
        actions: [
          IconButton(
            onPressed: () => VenuesScreen.open(context),
            tooltip: 'Venues',
            icon: const Icon(Icons.place_outlined),
          ),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _query,
                    textInputAction: TextInputAction.search,
                    decoration: const InputDecoration(
                      hintText: 'Cola, ginger beer, cherry…',
                    ),
                    onSubmitted: (_) => _search(),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton.filled(
                  onPressed: _search,
                  icon: const Icon(Icons.search_rounded),
                ),
              ],
            ),
          ),
          if (_error != null)
            Padding(padding: const EdgeInsets.all(16), child: Text(_error!)),
          if (_searching) const LinearProgressIndicator(color: Soda.lime),
          Expanded(
            child: !_searched
                ? const Center(
                    child: Text(
                      'Search the shared catalog.\nResults are capped, because Sodapop is not a database to scrape.',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Soda.muted),
                    ),
                  )
                : _results.isEmpty
                ? const Center(child: Text('No sodas matched.'))
                : ListView.separated(
                    padding: const EdgeInsets.all(16),
                    itemCount: _results.length,
                    separatorBuilder: (_, _) => const SizedBox(height: 10),
                    itemBuilder: (context, index) {
                      final drink = _results[index];
                      return Card(
                        child: ListTile(
                          contentPadding: const EdgeInsets.symmetric(
                            horizontal: 16,
                            vertical: 8,
                          ),
                          title: Text(
                            drink.name,
                            style: const TextStyle(fontWeight: FontWeight.w700),
                          ),
                          subtitle: Text(drink.subtitle),
                          trailing: FilledButton(
                            onPressed: () => showCheckinSheet(context, drink),
                            child: const Text('Log'),
                          ),
                        ),
                      );
                    },
                  ),
          ),
          Padding(
            padding: const EdgeInsets.all(16),
            child: Text(
              'Tip: scanning a barcode is faster than typing.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
