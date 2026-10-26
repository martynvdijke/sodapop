import 'package:flutter/material.dart';

import '../theme/soda_theme.dart';

/// A small, round initial avatar. When [onTap] is set the whole 44px target is
/// tappable and labelled for screen readers.
class SodaAvatar extends StatelessWidget {
  const SodaAvatar({
    super.key,
    required this.initial,
    this.size = 40,
    this.onTap,
    this.semanticLabel,
  });

  final String initial;
  final double size;
  final VoidCallback? onTap;
  final String? semanticLabel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final label = initial.trim().isEmpty
        ? '?'
        : initial.trim().characters.first.toUpperCase();
    final circle = Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: theme.colorScheme.primary.withValues(alpha: 0.3),
        shape: BoxShape.circle,
      ),
      child: Text(
        label,
        style: TextStyle(
          color: theme.colorScheme.onSurface,
          fontSize: size * 0.42,
          fontWeight: FontWeight.w800,
        ),
      ),
    );
    if (onTap == null) return circle;

    final target = size < 44 ? 44.0 : size;
    return Semantics(
      button: true,
      label: semanticLabel ?? 'Open profile',
      child: InkResponse(
        onTap: onTap,
        radius: target / 2,
        child: SizedBox(
          width: target,
          height: target,
          child: Center(child: circle),
        ),
      ),
    );
  }
}

/// Quarter-star display in tangerine. Five stars are filled fractionally so a
/// 4.25 rating looks like 4.25, not a rounded half-star.
class SodaRating extends StatelessWidget {
  const SodaRating({super.key, required this.quarters, this.size = 20});

  /// Rating in quarter stars, 0–20.
  final int quarters;
  final double size;

  @override
  Widget build(BuildContext context) {
    final value = (quarters.clamp(0, 20)) / 4;
    return Semantics(
      container: true,
      label: 'Rated ${_valueLabel(quarters)} out of 5',
      child: ExcludeSemantics(
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            for (var star = 0; star < 5; star++)
              SizedBox(
                width: size,
                height: size,
                child: Stack(
                  children: [
                    Icon(
                      Icons.star_rounded,
                      size: size,
                      color: Theme.of(
                        context,
                      ).colorScheme.onSurface.withValues(alpha: 0.16),
                    ),
                    if (value > star)
                      ClipRect(
                        child: Align(
                          alignment: Alignment.centerLeft,
                          widthFactor: (value - star).clamp(0.0, 1.0),
                          child: Icon(
                            Icons.star_rounded,
                            size: size,
                            color: Soda.tangerine,
                          ),
                        ),
                      ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }

  static String _valueLabel(int quarters) {
    if (quarters <= 0) return '0';
    final whole = quarters ~/ 4;
    final part = quarters % 4;
    return part == 0
        ? '$whole'
        : '$whole.${part * 25}'.replaceFirst('.50', '.5');
  }
}

/// A compact rounded label for visibility, venue names and other metadata.
class SodaPill extends StatelessWidget {
  const SodaPill({super.key, required this.label, this.icon});

  final String label;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(999),
        color: theme.colorScheme.onSurface.withValues(alpha: 0.05),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.12),
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 14, color: theme.colorScheme.onSurfaceVariant),
            const SizedBox(width: 5),
          ],
          Text(
            label,
            style: theme.textTheme.labelSmall?.copyWith(
              fontWeight: FontWeight.w600,
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

/// A two-to-three option segmented control. The selected option is a lime
/// fill; the rest stay transparent so the control reads as one quiet bar.
class SodaSegmented<T> extends StatelessWidget {
  const SodaSegmented({
    super.key,
    required this.options,
    required this.selected,
    required this.onChanged,
  });

  final List<(T, String)> options;
  final T selected;
  final ValueChanged<T> onChanged;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(
        color: theme.colorScheme.onSurface.withValues(alpha: 0.04),
        borderRadius: BorderRadius.circular(14),
        border: Border.all(
          color: theme.colorScheme.onSurface.withValues(alpha: 0.1),
        ),
      ),
      child: Row(
        children: [
          for (final (value, label) in options)
            Expanded(
              child: Semantics(
                button: true,
                selected: value == selected,
                child: Material(
                  color: value == selected
                      ? theme.colorScheme.primary
                      : Colors.transparent,
                  borderRadius: BorderRadius.circular(10),
                  child: InkWell(
                    onTap: () => onChanged(value),
                    borderRadius: BorderRadius.circular(10),
                    child: SizedBox(
                      height: 44,
                      child: Center(
                        child: Text(
                          label,
                          textAlign: TextAlign.center,
                          style: theme.textTheme.labelLarge?.copyWith(
                            fontWeight: FontWeight.w700,
                            color: value == selected
                                ? theme.colorScheme.onPrimary
                                : theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// Section heading used inside scroll views.
class SodaSectionTitle extends StatelessWidget {
  const SodaSectionTitle({super.key, required this.title, this.trailing});

  final String title;
  final String? trailing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      crossAxisAlignment: CrossAxisAlignment.baseline,
      textBaseline: TextBaseline.alphabetic,
      children: [
        Expanded(
          child: Text(
            title,
            style: theme.textTheme.titleMedium?.copyWith(
              fontWeight: FontWeight.w800,
            ),
          ),
        ),
        if (trailing != null)
          Text(
            trailing!,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
      ],
    );
  }
}

/// Full-page or inline loading state, shared by every v1.2 screen.
class SodaLoadingView extends StatelessWidget {
  const SodaLoadingView({super.key, this.label, this.dense = false});

  final String? label;
  final bool dense;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (dense) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 24),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const SizedBox(
              width: 16,
              height: 16,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: Soda.lime,
              ),
            ),
            if (label != null) ...[
              const SizedBox(width: 10),
              Text(
                label!,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ],
        ),
      );
    }
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.all(24),
      children: [
        const SizedBox(height: 96),
        const Center(child: CircularProgressIndicator(color: Soda.lime)),
        if (label != null) ...[
          const SizedBox(height: 16),
          Text(
            label!,
            textAlign: TextAlign.center,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ],
    );
  }
}

/// Shared empty/error state. Set [dense] to place it inside a larger list.
class SodaStatusView extends StatelessWidget {
  const SodaStatusView({
    super.key,
    required this.icon,
    required this.title,
    this.message,
    this.actionLabel,
    this.onAction,
    this.dense = false,
  });

  final IconData icon;
  final String title;
  final String? message;
  final String? actionLabel;
  final VoidCallback? onAction;
  final bool dense;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final content = Padding(
      padding: EdgeInsets.all(dense ? 8 : 24),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            icon,
            size: dense ? 30 : 54,
            color: theme.colorScheme.onSurfaceVariant,
          ),
          SizedBox(height: dense ? 8 : 14),
          Text(
            title,
            textAlign: TextAlign.center,
            style: dense
                ? theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                  )
                : theme.textTheme.titleMedium?.copyWith(
                    fontWeight: FontWeight.w700,
                  ),
          ),
          if (message != null) ...[
            const SizedBox(height: 6),
            Text(
              message!,
              textAlign: TextAlign.center,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          if (actionLabel != null) ...[
            const SizedBox(height: 10),
            TextButton(onPressed: onAction, child: Text(actionLabel!)),
          ],
        ],
      ),
    );
    if (dense) return content;
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      children: [
        const SizedBox(height: 64),
        Center(child: content),
      ],
    );
  }
}
