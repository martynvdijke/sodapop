import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../auth/auth_controller.dart';
import '../widgets/soda_ui.dart';

class _MixPick {
  _MixPick(this.drink) : volume = TextEditingController(text: '150');

  final Drink drink;
  final TextEditingController volume;

  void dispose() => volume.dispose();
}

/// Builds a mix from two to six catalog sodas.
class MixCreateScreen extends ConsumerStatefulWidget {
  const MixCreateScreen({super.key});

  static Future<MixRecipe?> open(BuildContext context) {
    return Navigator.of(context).push<MixRecipe>(
      MaterialPageRoute<MixRecipe>(builder: (_) => const MixCreateScreen()),
    );
  }

  @override
  ConsumerState<MixCreateScreen> createState() => _MixCreateScreenState();
}

class _MixCreateScreenState extends ConsumerState<MixCreateScreen> {
  final _name = TextEditingController();
  final _notes = TextEditingController();
  final _search = TextEditingController();
  final List<_MixPick> _picks = [];
  List<Drink> _results = const [];
  String _visibility = 'public';
  bool _searching = false;
  bool _searchStarted = false;
  bool _creating = false;
  String? _searchError;
  String? _formError;

  @override
  void dispose() {
    _name.dispose();
    _notes.dispose();
    _search.dispose();
    for (final pick in _picks) {
      pick.dispose();
    }
    super.dispose();
  }

  Future<void> _runSearch() async {
    if (_searching) return;
    final query = _search.text.trim();
    if (query.length < 2) {
      setState(() => _searchError = 'Type at least two characters.');
      return;
    }
    setState(() {
      _searching = true;
      _searchStarted = true;
      _searchError = null;
      _formError = null;
    });
    FocusScope.of(context).unfocus();
    try {
      final results = await ref.read(apiProvider).search(query);
      if (!mounted) return;
      setState(() {
        _results = results;
        _searching = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _searching = false;
        _searchError = ApiClient.describe(error).message;
      });
    }
  }

  void _add(Drink drink) {
    if (_picks.length >= 6) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('A mix holds six sodas.')));
      return;
    }
    if (_picks.any((pick) => pick.drink.id == drink.id)) return;
    setState(() {
      _picks.add(_MixPick(drink));
      _formError = null;
    });
  }

  void _remove(_MixPick pick) {
    setState(() {
      _picks.remove(pick);
    });
    WidgetsBinding.instance.addPostFrameCallback((_) => pick.dispose());
  }

  Future<void> _submit() async {
    if (_creating) return;
    final name = _name.text.trim();
    final runes = name.runes.length;
    if (runes < 2 || runes > 80) {
      setState(() => _formError = 'Give the mix a name of 2–80 characters.');
      return;
    }
    if (_picks.length < 2 || _picks.length > 6) {
      setState(() => _formError = 'Choose between two and six sodas.');
      return;
    }
    final components = <MixComponent>[];
    for (final pick in _picks) {
      final volume = int.tryParse(pick.volume.text.trim());
      if (volume == null || volume < 1 || volume > 4000) {
        setState(
          () => _formError =
              'Give ${pick.drink.name} a volume between 1 and 4000 ml.',
        );
        return;
      }
      components.add(MixComponent(drinkId: pick.drink.id, volumeMl: volume));
    }
    setState(() {
      _creating = true;
      _formError = null;
    });
    try {
      final created = await ref
          .read(apiProvider)
          .createMix(
            name: name,
            notes: _notes.text.trim(),
            visibility: _visibility,
            components: components,
          );
      if (!mounted) return;
      Navigator.of(context).pop(created);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _creating = false;
        _formError = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pickedIds = _picks.map((pick) => pick.drink.id).toSet();
    final available = _results
        .where((drink) => !pickedIds.contains(drink.id))
        .toList();
    return Scaffold(
      appBar: AppBar(title: const Text('New mix')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
        children: [
          _label('Name'),
          TextField(
            controller: _name,
            textCapitalization: TextCapitalization.sentences,
            inputFormatters: [LengthLimitingTextInputFormatter(80)],
            decoration: const InputDecoration(hintText: 'Cherry ginger sunset'),
          ),
          const SizedBox(height: 16),
          _label('Notes'),
          TextField(
            controller: _notes,
            minLines: 2,
            maxLines: 4,
            textCapitalization: TextCapitalization.sentences,
            inputFormatters: [LengthLimitingTextInputFormatter(500)],
            decoration: const InputDecoration(
              hintText: 'Half cherry cola, half ginger beer, lots of ice.',
            ),
          ),
          const SizedBox(height: 20),
          _label('Visibility'),
          const SizedBox(height: 8),
          SodaSegmented<String>(
            options: const [
              ('private', 'Only me'),
              ('friends', 'Friends'),
              ('public', 'Anyone'),
            ],
            selected: _visibility,
            onChanged: (value) => setState(() => _visibility = value),
          ),
          const SizedBox(height: 26),
          SodaSectionTitle(
            title: 'Components',
            trailing: '${_picks.length} of 6',
          ),
          const SizedBox(height: 4),
          Text(
            'Search the catalog, then add two to six sodas.',
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 12),
          if (_picks.isNotEmpty) ...[
            _selectedComponents(),
            const SizedBox(height: 16),
          ],
          TextField(
            controller: _search,
            textInputAction: TextInputAction.search,
            enabled: !_searching && _picks.length < 6,
            onSubmitted: (_) => _runSearch(),
            decoration: InputDecoration(
              hintText: _picks.length >= 6
                  ? 'Six is the limit'
                  : 'Search by name or brand',
              suffixIcon: IconButton(
                onPressed: _picks.length >= 6 ? null : _runSearch,
                tooltip: 'Search',
                icon: const Icon(Icons.search_rounded),
              ),
            ),
          ),
          if (_searchError != null) ...[
            const SizedBox(height: 8),
            Text(
              _searchError!,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.error,
              ),
            ),
          ],
          if (_searching) const SodaLoadingView(dense: true),
          if (available.isNotEmpty && _picks.length < 6) ...[
            const SizedBox(height: 10),
            Card(
              child: Column(
                children: [
                  for (var index = 0; index < available.length; index++) ...[
                    if (index > 0)
                      Divider(
                        height: 1,
                        color: theme.colorScheme.onSurface.withValues(
                          alpha: 0.08,
                        ),
                      ),
                    ListTile(
                      title: Text(
                        available[index].name,
                        style: const TextStyle(fontWeight: FontWeight.w700),
                      ),
                      subtitle: Text(
                        [
                          available[index].brand,
                          available[index].style,
                        ].where((value) => value.isNotEmpty).join(' · '),
                      ),
                      trailing: IconButton(
                        onPressed: () => _add(available[index]),
                        tooltip: 'Add ${available[index].name}',
                        icon: const Icon(Icons.add_circle_outline_rounded),
                      ),
                      onTap: () => _add(available[index]),
                    ),
                  ],
                ],
              ),
            ),
          ] else if (_searchStarted &&
              !_searching &&
              _searchError == null &&
              _results.isEmpty) ...[
            const SizedBox(height: 8),
            Text(
              'No sodas matched.',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          if (_formError != null) ...[
            const SizedBox(height: 18),
            _errorBanner(_formError!),
          ],
          const SizedBox(height: 22),
          FilledButton(
            onPressed: _creating ? null : _submit,
            child: Text(_creating ? 'Creating…' : 'Create mix'),
          ),
        ],
      ),
    );
  }

  Widget _selectedComponents() {
    final theme = Theme.of(context);
    return Card(
      child: Column(
        children: [
          for (var index = 0; index < _picks.length; index++) ...[
            if (index > 0)
              Divider(
                height: 1,
                color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
              ),
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 12, 6, 12),
              child: Row(
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          _picks[index].drink.name,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.titleSmall?.copyWith(
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        if (_picks[index].drink.brand.isNotEmpty)
                          Text(
                            _picks[index].drink.brand,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                      ],
                    ),
                  ),
                  const SizedBox(width: 8),
                  SizedBox(
                    width: 88,
                    child: TextField(
                      controller: _picks[index].volume,
                      keyboardType: TextInputType.number,
                      textAlign: TextAlign.right,
                      inputFormatters: [
                        FilteringTextInputFormatter.digitsOnly,
                        LengthLimitingTextInputFormatter(4),
                      ],
                      decoration: const InputDecoration(
                        hintText: '150',
                        suffixText: 'ml',
                        isDense: true,
                      ),
                    ),
                  ),
                  IconButton(
                    onPressed: () => _remove(_picks[index]),
                    tooltip: 'Remove ${_picks[index].drink.name}',
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
          ],
        ],
      ),
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
