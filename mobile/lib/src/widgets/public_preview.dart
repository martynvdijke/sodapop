import 'package:flutter/material.dart';

import '../api/api_client.dart';
import '../api/models.dart';
import '../theme/soda_theme.dart';
import '../utils/url.dart';
import 'soda_format.dart';
import 'soda_ui.dart';

/// Read-only check-in card used by the logged-out preview. It deliberately has
/// no save, toast, comment, or report affordances.
class PublicCheckinCard extends StatelessWidget {
  const PublicCheckinCard({
    super.key,
    required this.item,
    required this.api,
    this.onTap,
    this.onAuthorTap,
  });

  final FeedItem item;
  final ApiClient api;
  final VoidCallback? onTap;
  final VoidCallback? onAuthorTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: InkWell(
                      onTap: onAuthorTap,
                      borderRadius: BorderRadius.circular(10),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(vertical: 2),
                        child: Row(
                          children: [
                            SodaAvatar(
                              initial: item.displayName.isEmpty
                                  ? item.handle
                                  : item.displayName,
                              size: 36,
                            ),
                            const SizedBox(width: 10),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(
                                    item.displayName,
                                    style: theme.textTheme.titleSmall?.copyWith(
                                      fontWeight: FontWeight.w700,
                                    ),
                                  ),
                                  Text(
                                    '@${item.handle}',
                                    style: theme.textTheme.bodySmall?.copyWith(
                                      color: theme.colorScheme.onSurfaceVariant,
                                    ),
                                  ),
                                ],
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Text(
                    ratingLabel((item.rating ?? 0).round()),
                    style: theme.textTheme.titleMedium?.copyWith(
                      color: Soda.tangerine,
                      fontWeight: FontWeight.w800,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              Text(
                item.drinkName,
                style: theme.textTheme.titleLarge?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              if (item.brand.isNotEmpty ||
                  item.venueName.isNotEmpty ||
                  item.atHome)
                Text(
                  [
                    if (item.brand.isNotEmpty) item.brand,
                    if (item.venueName.isNotEmpty)
                      'at ${item.venueName}'
                    else if (item.atHome)
                      sodaAtHomeLabel(true),
                  ].join(' · '),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              if (item.photoId.isNotEmpty) ...[
                const SizedBox(height: 10),
                ClipRRect(
                  borderRadius: BorderRadius.circular(12),
                  child: Image.network(
                    api.publicMediaUrl(item.photoId),
                    height: 210,
                    width: double.infinity,
                    fit: BoxFit.cover,
                    errorBuilder: (_, _, _) => const SizedBox.shrink(),
                  ),
                ),
              ],
              if (item.note.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(item.note, style: theme.textTheme.bodyMedium),
              ],
              if (sodaPrepMethod(item.prepMethod).isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(
                  item.prepNote.isEmpty
                      ? sodaPrepMethod(item.prepMethod)
                      : '${sodaPrepMethod(item.prepMethod)} · ${item.prepNote}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              const SizedBox(height: 10),
              Row(
                children: [
                  Icon(
                    Icons.celebration_outlined,
                    size: 18,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(width: 6),
                  Text(
                    '${item.toasts} toast${item.toasts == 1 ? '' : 's'}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const Spacer(),
                  Text(
                    'Read only',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// One row in the public people directory.
class PublicPersonTile extends StatelessWidget {
  const PublicPersonTile({super.key, required this.person, this.onTap});

  final PublicPerson person;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SodaAvatar(
                initial: person.displayName.isEmpty
                    ? person.handle
                    : person.displayName,
                size: 44,
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      person.displayName.isEmpty
                          ? '@${person.handle}'
                          : person.displayName,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    Text(
                      '@${person.handle}',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                    if (person.bio.isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        person.bio,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodyMedium,
                      ),
                    ],
                    const SizedBox(height: 8),
                    Text(
                      '${person.pours} public pour${person.pours == 1 ? '' : 's'} · ${person.styles} style${person.styles == 1 ? '' : 's'}',
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
