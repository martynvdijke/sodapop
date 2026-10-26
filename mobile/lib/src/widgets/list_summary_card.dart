import 'package:flutter/material.dart';

import '../api/models.dart';
import 'soda_format.dart';
import 'soda_ui.dart';

/// One list summary, shared by the Lists screen and the profile lists section.
class ListSummaryCard extends StatelessWidget {
  const ListSummaryCard({
    super.key,
    required this.summary,
    required this.onTap,
  });

  final ListSummary summary;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final visibility = sodaListVisibility(summary.visibility);
    final meta = [
      if (visibility.isNotEmpty) visibility,
      sodaCount(summary.itemCount, 'soda'),
    ].join(' · ');

    return Card(
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      summary.name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                    if (summary.description.isNotEmpty) ...[
                      const SizedBox(height: 4),
                      Text(
                        summary.description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ],
                    const SizedBox(height: 6),
                    Text(
                      meta,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              if (summary.isSystem) ...[
                const SizedBox(width: 10),
                const SodaPill(label: 'Built in'),
              ],
              const SizedBox(width: 4),
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
}
