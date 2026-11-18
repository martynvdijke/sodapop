/// Client-side plumbing for editing a check-in: the author's 24-hour window
/// and the price text → integer cents conversion.
///
/// The server enforces both rules (see `internal/checkins/edit.go`); these
/// helpers only keep the UI honest before it asks. They are pure so they can be
/// unit tested without a widget or a network.
library;

/// How long after creation an author may edit a check-in. Mirrors the server's
/// `checkins.EditWindow`, and is measured from `created_at` — never from the
/// last edit, so a repeated edit cannot extend it.
const Duration checkinEditWindow = Duration(hours: 24);

/// The largest price the API accepts, in integer cents (0–100000 inclusive).
const int maxPriceCents = 100000;

/// True while an author may still edit a check-in created at [createdAt].
///
/// A missing or unparsable timestamp is treated as a **closed** window: the
/// app would rather hide Edit than offer an action the server will refuse.
bool checkinEditWindowOpen(String? createdAt, {DateTime? now}) {
  final text = (createdAt ?? '').trim();
  if (text.isEmpty) return false;
  final created = DateTime.tryParse(text);
  if (created == null) return false;
  final elapsed = (now ?? DateTime.now()).toUtc().difference(created.toUtc());
  // A row stamped slightly in the future (clock skew) is still inside its
  // window; the server remains the authority.
  return elapsed < checkinEditWindow;
}

/// What [parsePriceCents] made of the field text.
enum PriceInputStatus {
  /// A usable amount; [ParsedPrice.cents] holds it.
  ok,

  /// Nothing was typed, which means "no price" (and, in edit mode, "clear").
  blank,

  /// The text is not a valid amount, or the amount exceeds [maxPriceCents].
  /// Callers show a field error; the value is never clamped.
  invalid,
}

/// The result of parsing a price field. [cents] is non-null only when the
/// status is [PriceInputStatus.ok].
class ParsedPrice {
  const ParsedPrice.ok(int this.cents) : status = PriceInputStatus.ok;
  const ParsedPrice.blank()
    : status = PriceInputStatus.blank,
      cents = null;
  const ParsedPrice.invalid()
    : status = PriceInputStatus.invalid,
      cents = null;

  final PriceInputStatus status;
  final int? cents;

  bool get isOk => status == PriceInputStatus.ok;
  bool get isBlank => status == PriceInputStatus.blank;
  bool get isInvalid => status == PriceInputStatus.invalid;
}

/// Converts user text in **major units** to integer cents.
///
/// The app always treats what is typed as major units, so `"1.50"` is 150 and
/// `"2"` is 200. The web form's integer-means-raw-cents rule does not apply
/// here. At most two decimal places are accepted; blank input is
/// [PriceInputStatus.blank]; anything non-numeric, negative, or above
/// [maxPriceCents] is [PriceInputStatus.invalid] rather than silently clamped.
ParsedPrice parsePriceCents(String input) {
  final text = input.trim();
  if (text.isEmpty) return const ParsedPrice.blank();

  final match = RegExp(r'^(\d+)(?:\.(\d{1,2}))?$').firstMatch(text);
  if (match == null) return const ParsedPrice.invalid();

  final whole = int.tryParse(match.group(1)!);
  if (whole == null) return const ParsedPrice.invalid();

  final fraction = match.group(2) ?? '';
  final minor = int.tryParse(fraction.padRight(2, '0'));
  if (minor == null) return const ParsedPrice.invalid();

  final cents = whole * 100 + minor;
  if (cents > maxPriceCents) return const ParsedPrice.invalid();
  return ParsedPrice.ok(cents);
}

/// Renders integer cents as major-unit text for display: 150 → `"1.50"`,
/// 200 → `"2"`, 199 → `"1.99"`.
String formatMajorUnits(int cents) {
  final sign = cents < 0 ? '-' : '';
  final absolute = cents.abs();
  final whole = absolute ~/ 100;
  final minor = absolute % 100;
  if (minor == 0) return '$sign$whole';
  return '$sign$whole.${minor.toString().padLeft(2, '0')}';
}
