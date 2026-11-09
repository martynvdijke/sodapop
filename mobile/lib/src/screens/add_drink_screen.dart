import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_client.dart';
import '../auth/auth_controller.dart';
import '../theme/soda_theme.dart';
import '../utils/gtin.dart';
import 'drink_detail_screen.dart';

/// Opens the soda that was added. Injected by tests so they can assert
/// navigation without loading the catalog.
typedef AddDrinkOpener =
    Future<void> Function(BuildContext context, String slug, {String name});

/// Curated style slugs. The catalog has more, but these cover most sodas.
const _styles = <(String, String)>[
  ('', 'Not sure'),
  ('cola', 'Cola'),
  ('root-beer', 'Root beer'),
  ('lemon-lime', 'Lemon-lime'),
  ('orange', 'Orange'),
  ('ginger-ale', 'Ginger ale'),
  ('cream-soda', 'Cream soda'),
  ('grape', 'Grape'),
  ('cherry-berry', 'Cherry & berry'),
  ('mixers', 'Tonic & mixers'),
  ('sparkling-water', 'Sparkling water'),
];

/// The container values the server understands. `bottle` is not one of them:
/// the enum distinguishes glass and PET instead.
const _containers = <(String, String)>[
  ('can', 'Can'),
  ('glass', 'Glass bottle'),
  ('pet', 'PET bottle'),
  ('fountain', 'Fountain'),
  ('carton', 'Carton'),
  ('other', 'Other'),
];

/// The beverage types the server understands. Non-NA drinks carry no ABV.
const _beverageTypes = <(String, String)>[
  ('soda', 'Soda'),
  ('water', 'Water'),
  ('tea', 'Tea'),
  ('coffee', 'Coffee'),
  ('na_beer', 'NA beer'),
  ('na_wine', 'NA wine'),
  ('na_spirit', 'NA spirits'),
  ('other', 'Other'),
];

const _sweeteners = <(String, String)>[
  ('', 'Unknown'),
  ('sugar', 'Sugar'),
  ('hfcs', 'HFCS'),
  ('aspartame', 'Aspartame'),
  ('sucralose', 'Sucralose'),
  ('stevia', 'Stevia'),
  ('monk_fruit', 'Monk fruit'),
  ('allulose', 'Allulose'),
  ('mixed', 'Mixed'),
  ('none', 'None'),
];

const _carbonations = <(String, String)>[
  ('', 'Unknown'),
  ('still', 'Still'),
  ('light', 'Light'),
  ('standard', 'Standard'),
  ('extra', 'Extra'),
];

const _acids = <(String, String)>[
  ('', 'Unknown'),
  ('phosphoric', 'Phosphoric'),
  ('citric', 'Citric'),
  ('malic', 'Malic'),
  ('tartaric', 'Tartaric'),
  ('none', 'None'),
];

/// A plain form for adding one soda by hand. The server deduplicates, so a
/// submission for a soda that already exists opens the existing row.
class AddDrinkScreen extends ConsumerStatefulWidget {
  const AddDrinkScreen({
    super.key,
    this.barcode = '',
    this.initialName = '',
    this.initialBrand = '',
    this.initialVolumeMl,
    this.openDrink,
  });

  /// Prefilled from a scan or a search that found nothing.
  final String barcode;
  final String initialName;
  final String initialBrand;
  final int? initialVolumeMl;
  final AddDrinkOpener? openDrink;

  static Future<void> open(
    BuildContext context, {
    String barcode = '',
    String name = '',
    String brand = '',
    int? volumeMl,
  }) {
    return Navigator.of(context).push<void>(
      MaterialPageRoute<void>(
        builder: (_) => AddDrinkScreen(
          barcode: barcode,
          initialName: name,
          initialBrand: brand,
          initialVolumeMl: volumeMl,
        ),
      ),
    );
  }

  @override
  ConsumerState<AddDrinkScreen> createState() => _AddDrinkScreenState();
}

class _AddDrinkScreenState extends ConsumerState<AddDrinkScreen> {
  final _formKey = GlobalKey<FormState>();
  late final _barcode = TextEditingController(text: widget.barcode.trim());
  late final _name = TextEditingController(text: widget.initialName.trim());
  late final _brand = TextEditingController(text: widget.initialBrand.trim());
  late final _volume = TextEditingController(
    text: '${(widget.initialVolumeMl ?? 0) > 0 ? widget.initialVolumeMl : 355}',
  );
  final _notes = TextEditingController();
  final _abv = TextEditingController(text: '0');

  String _style = '';
  String _container = 'can';
  String _beverageType = 'soda';
  String _sweetener = '';
  String _carbonation = '';
  String _acid = '';
  bool _submitting = false;
  String? _error;

  @override
  void dispose() {
    _barcode.dispose();
    _name.dispose();
    _brand.dispose();
    _volume.dispose();
    _notes.dispose();
    _abv.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_submitting) return;
    if (!(_formKey.currentState?.validate() ?? false)) return;
    FocusScope.of(context).unfocus();

    final api = ref.read(apiProvider);
    final opener = widget.openDrink ?? DrinkDetailScreen.open;
    final navigator = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    final route = ModalRoute.of(context);
    final volume = int.tryParse(_volume.text.trim()) ?? 355;
    final abv = (double.tryParse(_abv.text.trim()) ?? 0).clamp(0, 100).toDouble();

    setState(() {
      _submitting = true;
      _error = null;
    });

    try {
      final result = await api.createDrink(
        name: _name.text.trim(),
        brand: _brand.text.trim(),
        styleSlug: _style,
        container: _container,
        volumeMl: volume,
        beverageType: _beverageType,
        abvPercent: abv,
        sweetener: _sweetener,
        carbonation: _carbonation,
        acid: _acid,
        barcode: normalizeGtin(_barcode.text.trim()) ?? '',
        description: _notes.text.trim(),
      );
      if (!mounted) return;
      setState(() => _submitting = false);
      messenger.showSnackBar(
        SnackBar(
          content: Text(
            result.created
                ? 'Added as provisional'
                : 'This soda was already in the catalog',
          ),
        ),
      );
      // Open the soda, then drop this form from under it so Back returns to
      // wherever the form was opened from.
      final opening = opener(
        context,
        result.slug,
        name: result.name.isEmpty ? _name.text.trim() : result.name,
      );
      if (route != null && !route.isCurrent) navigator.removeRoute(route);
      await opening;
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _error = ApiClient.describe(error).message;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Add a soda')),
      body: SafeArea(
        top: false,
        child: Form(
          key: _formKey,
          child: ListView(
            padding: const EdgeInsets.fromLTRB(20, 8, 20, 36),
            children: [
              _IntroCard(theme: theme),
              const SizedBox(height: 22),
              _Label('The basics'),
              const SizedBox(height: 10),
              TextFormField(
                key: const Key('add-barcode'),
                controller: _barcode,
                keyboardType: TextInputType.number,
                inputFormatters: [LengthLimitingTextInputFormatter(14)],
                decoration: const InputDecoration(
                  labelText: 'Barcode',
                  hintText: '5449000000996',
                  helperText: 'Optional. From a scan or the label.',
                  helperMaxLines: 2,
                ),
                validator: (value) {
                  final raw = (value ?? '').trim();
                  if (raw.isEmpty) return null;
                  if (normalizeGtin(raw) == null) {
                    return 'That barcode is not valid. Check the digits.';
                  }
                  return null;
                },
              ),
              const SizedBox(height: 14),
              TextFormField(
                key: const Key('add-name'),
                controller: _name,
                textCapitalization: TextCapitalization.words,
                inputFormatters: [LengthLimitingTextInputFormatter(120)],
                decoration: const InputDecoration(
                  labelText: 'Name',
                  hintText: 'Cherry cola',
                  helperText: 'Required.',
                ),
                validator: (value) {
                  if ((value ?? '').trim().isEmpty) {
                    return 'Add a name for the soda.';
                  }
                  return null;
                },
                onChanged: (_) {
                  if (_error != null) setState(() => _error = null);
                },
              ),
              const SizedBox(height: 14),
              TextFormField(
                key: const Key('add-brand'),
                controller: _brand,
                textCapitalization: TextCapitalization.words,
                inputFormatters: [LengthLimitingTextInputFormatter(120)],
                decoration: const InputDecoration(
                  labelText: 'Brand',
                  hintText: 'Fentimans',
                ),
              ),
              const SizedBox(height: 14),
              _Dropdown(
                fieldKey: const Key('add-style'),
                label: 'Style',
                value: _style,
                options: _styles,
                onChanged: (value) => setState(() => _style = value),
              ),
              const SizedBox(height: 14),
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    flex: 3,
                    child: _Dropdown(
                      fieldKey: const Key('add-container'),
                      label: 'Container',
                      value: _container,
                      options: _containers,
                      onChanged: (value) => setState(() => _container = value),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    flex: 2,
                    child: TextFormField(
                      key: const Key('add-volume'),
                      controller: _volume,
                      keyboardType: TextInputType.number,
                      inputFormatters: [LengthLimitingTextInputFormatter(5)],
                      decoration: const InputDecoration(labelText: 'Volume ml'),
                      validator: (value) {
                        final ml = int.tryParse((value ?? '').trim());
                        if (ml == null || ml < 1 || ml > 5000) {
                          return '1–5000';
                        }
                        return null;
                      },
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    flex: 3,
                    child: _Dropdown(
                      fieldKey: const Key('add-beverage-type'),
                      label: 'Beverage type',
                      value: _beverageType,
                      options: _beverageTypes,
                      onChanged: (value) =>
                          setState(() => _beverageType = value),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    flex: 2,
                    child: TextFormField(
                      key: const Key('add-abv'),
                      controller: _abv,
                      keyboardType: const TextInputType.numberWithOptions(
                        decimal: true,
                      ),
                      inputFormatters: [
                        FilteringTextInputFormatter.allow(
                          RegExp(r'^\d{0,3}(\.\d?)?'),
                        ),
                      ],
                      decoration: const InputDecoration(
                        labelText: 'ABV %',
                        helperText: '0 if none',
                      ),
                      validator: (value) {
                        final raw = (value ?? '').trim();
                        if (raw.isEmpty) return null;
                        final abv = double.tryParse(raw);
                        if (abv == null || abv < 0 || abv > 100) {
                          return '0–100';
                        }
                        return null;
                      },
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 26),
              _Label('Taste and fizz'),
              const SizedBox(height: 10),
              _Dropdown(
                fieldKey: const Key('add-sweetener'),
                label: 'Sweetener',
                value: _sweetener,
                options: _sweeteners,
                onChanged: (value) => setState(() => _sweetener = value),
              ),
              const SizedBox(height: 14),
              _Dropdown(
                fieldKey: const Key('add-carbonation'),
                label: 'Carbonation',
                value: _carbonation,
                options: _carbonations,
                onChanged: (value) => setState(() => _carbonation = value),
              ),
              const SizedBox(height: 14),
              _Dropdown(
                fieldKey: const Key('add-acid'),
                label: 'Acid',
                value: _acid,
                options: _acids,
                onChanged: (value) => setState(() => _acid = value),
              ),
              const SizedBox(height: 14),
              TextFormField(
                key: const Key('add-notes'),
                controller: _notes,
                minLines: 3,
                maxLines: 6,
                textCapitalization: TextCapitalization.sentences,
                inputFormatters: [LengthLimitingTextInputFormatter(2000)],
                decoration: const InputDecoration(
                  labelText: 'Notes',
                  hintText: 'Botanically brewed, cherry and vanilla.',
                  alignLabelWithHint: true,
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 16),
                _Notice(message: _error!),
              ],
              const SizedBox(height: 20),
              FilledButton.icon(
                key: const Key('add-submit'),
                onPressed: _submitting ? null : _submit,
                icon: _submitting
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(
                          strokeWidth: 2,
                          color: Soda.forest,
                        ),
                      )
                    : const Icon(Icons.add_rounded, size: 20),
                label: Text(_submitting ? 'Adding…' : 'Add soda'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _IntroCard extends StatelessWidget {
  const _IntroCard({required this.theme});

  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    final dark = theme.brightness == Brightness.dark;
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(22),
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            theme.colorScheme.primary.withValues(alpha: dark ? 0.22 : 0.42),
            theme.colorScheme.surface,
          ],
        ),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.08),
        ),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 42,
            height: 42,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: theme.colorScheme.primary,
              shape: BoxShape.circle,
            ),
            child: Icon(
              Icons.local_drink_outlined,
              size: 21,
              color: theme.colorScheme.onPrimary,
            ),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'New soda',
                  style: theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w800,
                    letterSpacing: -0.3,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  'It joins as provisional until a moderator checks it. If this '
                  'soda is already in the catalog, we will take you to it '
                  'instead of creating a duplicate.',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    height: 1.45,
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _Label extends StatelessWidget {
  const _Label(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Text(
      text,
      style: theme.textTheme.labelLarge?.copyWith(
        fontWeight: FontWeight.w800,
        letterSpacing: 0.2,
      ),
    );
  }
}

class _Dropdown extends StatelessWidget {
  const _Dropdown({
    required this.fieldKey,
    required this.label,
    required this.value,
    required this.options,
    required this.onChanged,
  });

  final Key fieldKey;
  final String label;
  final String value;
  final List<(String, String)> options;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return DropdownButtonFormField<String>(
      key: fieldKey,
      initialValue: value,
      isExpanded: true,
      decoration: InputDecoration(labelText: label),
      items: [
        for (final (slug, name) in options)
          DropdownMenuItem(value: slug, child: Text(name)),
      ],
      onChanged: (next) {
        if (next != null) onChanged(next);
      },
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
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
