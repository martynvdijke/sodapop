import 'package:flutter/material.dart';

import '../api/models.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';

/// The compact "favourite soda" card pinned to a profile. It follows the same
/// 54px thumbnail language as the check-in rows so a profile reads as one
/// surface rather than two different list styles.
class PinnedDrinkCard extends StatelessWidget {
  const PinnedDrinkCard({
    super.key,
    required this.drink,
    required this.onTap,
    this.imageHeaders = const {},
    this.baseUrl,
    this.label = 'Pinned favourite',
  });

  final FavouriteDrink drink;
  final VoidCallback onTap;
  final Map<String, String> imageHeaders;

  /// Server origin used to resolve a relative image path from the API.
  final String? baseUrl;
  final String label;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final meta = [
      drink.brand,
      drink.style,
    ].where((value) => value.trim().isNotEmpty).join(' · ');
    final name = drink.name.trim().isEmpty ? 'Soda' : drink.name;

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            children: [
              _thumb(theme),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      label.toUpperCase(),
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: Soda.tangerine,
                        fontWeight: FontWeight.w800,
                        letterSpacing: 0.6,
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.titleSmall?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (meta.isNotEmpty) ...[
                      const SizedBox(height: 2),
                      Text(
                        meta,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
              if (drink.averageRating != null)
                Padding(
                  padding: const EdgeInsets.only(right: 4),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(
                        Icons.star_rounded,
                        size: 16,
                        color: Soda.tangerine,
                      ),
                      Text(
                        drink.averageRating!.toStringAsFixed(1),
                        style: theme.textTheme.labelLarge?.copyWith(
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                    ],
                  ),
                ),
              Icon(
                Icons.chevron_right_rounded,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _thumb(ThemeData theme) {
    const size = 54.0;
    final placeholder = Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: theme.colorScheme.primary.withValues(alpha: 0.22),
        borderRadius: BorderRadius.circular(12),
      ),
      child: const Icon(
        Icons.local_drink_outlined,
        size: 22,
        color: Soda.forest,
      ),
    );
    final url = absoluteMediaUrl(drink.imageUrl ?? '', base: baseUrl);
    if (url.isEmpty) return placeholder;

    return ClipRRect(
      borderRadius: BorderRadius.circular(12),
      child: Image.network(
        url,
        width: size,
        height: size,
        fit: BoxFit.cover,
        headers: imageHeaders,
        semanticLabel: 'Photo of ${drink.name}',
        errorBuilder: (_, _, _) => placeholder,
        loadingBuilder: (context, child, progress) =>
            progress == null ? child : placeholder,
      ),
    );
  }
}

/// A quiet inline helper for screens that have no pinned soda yet.
class PinnedDrinkEmpty extends StatelessWidget {
  const PinnedDrinkEmpty({super.key, this.message = 'No pinned soda yet.'});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
        ),
      ),
      child: Row(
        children: [
          Icon(
            Icons.push_pin_outlined,
            size: 18,
            color: theme.colorScheme.onSurfaceVariant,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Text(
              message,
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
