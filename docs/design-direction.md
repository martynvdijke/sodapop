# Sodapop — Design Direction

**Status:** v1.0, greenfield. Mobile-first PWA, Go server-rendered templates, small TypeScript layer.
**Scope:** Brand, tokens, type, components, motion, accessibility. This document is the source of truth until a `tokens.css` exists; the tokens block below is intended to be copied into it verbatim.
**Validation owner:** orchestrator.

---

## 1. Brand

Sodapop is a check-in log for soft drinks: craft bottlings, regional brands, fountain pours, and the occasional hard soda. It treats soda with the seriousness beer apps reserve for beer, but keeps the tone light — scan the barcode, rate it in quarter stars, note what it tasted like, move on. The look is a cold glass on a warm counter: bright lime, cream paper, deep green ink, and a single tangerine accent reserved for scores.

**Personality:** crisp · curious · sociable · unpretentious

**Voice in UI:** sentence case, no exclamation points, verb-first buttons ("Check in", "Add this soda"), "soda" not "beverage", absolute dates ("12 Mar") not "2 days ago" in detail views, and no sugar shaming. Nutrition data is stated, never judged.

**Avoid**

- Corporate wellness green or emerald "sustainability" branding.
- Gamer neon: lime glow on pure black, scanlines, RGB edges.
- Beer-app cosplay: amber and brown palettes, dark wood, pint-glass iconography.
- Retro diner kitsch: checkerboard, chrome, cursive scripts.
- Generic SaaS: purple-to-blue gradients, glassmorphism, floating blobs, gradient hero text.
- Childish candy branding: bubble letters, rainbow palettes, mascot faces.
- Health-app moralizing: red warnings on sugar or caffeine, "guilt" language.

---

## 2. Color

**The one rule that makes the light theme work: lime is a fill, not an ink.** `#B4E32C` has a 1.35:1 contrast ratio against the cream background and 1.45:1 against cards. It is therefore never used for body text, small icons, thin borders, tab underlines, or a focus ring by itself. Lime appears as a filled area large enough to carry deep-green text (buttons, the scan button, selected chips), or as a tinted surface. Deep green does the reading; lime does the shouting.

**Composition:** roughly 60% cream or near-black base, 25% card surfaces, 10% ink and borders, 5% lime. One lime page-level action per screen. The center scan button is chrome and does not count against that budget.

**Edge rule:** in light mode, a lime fill that sits directly on cream or `--surface` carries a 1px `--border-strong` edge (3.79:1 against the page). It keeps a lime button from reading as a highlighter mark and gives the shape a boundary that passes the 3:1 non-text requirement. In dark mode lime already separates from the background at 12.95:1 and needs no edge. Tangerine never sits on lime or lime tint (2.92:1 against `--primary-tint`); stars and badge glyphs live on `--surface`, `--bg`, or their own tint.

**Why the supporting hues**

- **Cream, not white:** `#F2F4E6` carries a faint green cast, like uncoated label stock. Pure white flattens lime into neon; cream makes it read as fresh.
- **Pine ink, not black:** `#16241A` is 14.49:1 on cream, stays in the same hue family as lime, and translates to a warm bone in dark mode instead of a harsh inverted black.
- **Tangerine, not red or gold:** `#E85D26` sits at hue 40 against lime's 124. It is used for star fills and rating marks. It is not the danger red and not the success green, so ratings never collide with status. The lightness gap (OKLCH 65% vs 85%) keeps the two separated in deuteranopia and protanopia simulations; the numeric score remains the real signal.
- **Status colors are deeper or mintier than primary:** success is deep pine in light mode and mint in dark; it can't be mistaken for the lime action color. Danger is brick red, always paired with an icon and a word.

### 2.1 Role table

| Token | Role | Light | Dark |
|---|---|---|---|
| `--bg` | App background, page canvas | `#F2F4E6` | `#0C130E` |
| `--surface` | Cards, sheets, nav, inputs | `#FBFBF2` | `#151E17` |
| `--surface-raised` | Menus, popovers, dialogs, raised controls | `#FFFFFF` | `#1E2A20` |
| `--surface-sunken` | Image wells, inset fields, pressed cards, locked badges | `#E9EDDA` | `#080E09` |
| `--border` | Decorative hairlines and card edges only | `#D8DCC2` | `#31402F` |
| `--border-strong` | Form-control boundaries, outlined chips, empty stars, dividers that carry meaning | `#74805A` | `#6B7A63` |
| `--text` | Body and headings | `#16241A` | `#EEF2E0` |
| `--text-muted` | Secondary labels, metadata, timestamps | `#55613D` | `#A8B49A` |
| `--text-disabled` | Disabled labels; never the only cue for state | `#8B957B` | `#6F7D66` |
| `--primary` | Lime fills: primary buttons, scan button, selected chip | `#B4E32C` | `#B9E63A` |
| `--primary-hover` | Hover on lime | `#9FD11A` | `#C9F45A` |
| `--primary-press` | Active/pressed lime | `#86B50D` | `#A6D028` |
| `--primary-contrast` | Text and icons on lime; inset focus ring where needed | `#16241A` | `#0C130E` |
| `--primary-tint` | Quiet lime surfaces: active nav pill, earned badge tile, soft emphasis | `#E7EFCB` | `#26331C` |
| `--accent` | Star fill and rating marks (graphic use only) | `#E85D26` | `#FF8A57` |
| `--accent-ink` | Text links and labels in the accent family | `#BC440F` | `#FFA57F` |
| `--accent-tint` | Rating summaries and score callouts | `#FCEBE2` | `#3A2118` |
| `--success` | Confirmation text and icons | `#1D6B45` | `#5FD39A` |
| `--success-tint` | Confirmation bars and "would drink again" selection | `#DDEEE3` | `#122E23` |
| `--warning` | Non-destructive warnings, availability notices | `#8A5A00` | `#F2B33D` |
| `--warning-tint` | Warning panels | `#F6EBCF` | `#33280F` |
| `--danger` | Errors and destructive actions | `#B3261E` | `#FF7A6E` |
| `--danger-tint` | Error panels | `#F7E0DD` | `#3A1714` |
| `--focus-ring` | Focus outline on non-lime surfaces | `#16241A` | `#B9E63A` |
| `--focus-halo` | Soft outer halo behind the focus outline | `#E7EFCB` | `#B9E63A38` |
| `--overlay` | Solid overlay base for composited scrims | `#0C130E` | `#050805` |

Two extras live outside the role table: `--scrim` and the scanner constants (section 2.4). They are fixed per theme in the CSS block.

### 2.2 OKLCH reference

Hex is the contract for implementation; OKLCH is here for interpolation, chart ramps, and future tints.

| Token | Light | Dark |
|---|---|---|
| `--bg` | `oklch(96.2% 0.019 113)` | `oklch(17.7% 0.015 154)` |
| `--surface` | `oklch(98.6% 0.012 107)` | `oklch(22.4% 0.019 151)` |
| `--surface-raised` | `oklch(100% 0 0)` | `oklch(27.0% 0.025 149)` |
| `--surface-sunken` | `oklch(93.8% 0.026 117)` | `oklch(15.5% 0.014 150)` |
| `--border` | `oklch(88.4% 0.035 114)` | `oklch(35.3% 0.035 142)` |
| `--border-strong` | `oklch(58.0% 0.057 122)` | `oklch(56.0% 0.039 135)` |
| `--text` | `oklch(24.4% 0.027 153)` | `oklch(95.3% 0.024 117)` |
| `--text-muted` | `oklch(47.2% 0.057 124)` | `oklch(75.3% 0.039 128)` |
| `--text-disabled` | `oklch(65.4% 0.039 125)` | `oklch(57.1% 0.038 133)` |
| `--primary` | `oklch(85.3% 0.200 124)` | `oklch(86.3% 0.195 124)` |
| `--primary-hover` | `oklch(79.6% 0.196 125)` | `oklch(90.8% 0.184 123)` |
| `--primary-press` | `oklch(71.3% 0.179 126)` | `oklch(79.9% 0.186 124)` |
| `--primary-contrast` | `oklch(24.4% 0.027 153)` | `oklch(17.7% 0.015 154)` |
| `--primary-tint` | `oklch(93.7% 0.048 118)` | `oklch(30.2% 0.043 132)` |
| `--accent` | `oklch(65.0% 0.184 40)` | `oklch(75.3% 0.157 43)` |
| `--accent-ink` | `oklch(54.7% 0.165 40)` | `oklch(80.3% 0.119 44)` |
| `--accent-tint` | `oklch(95.1% 0.022 50)` | `oklch(27.8% 0.042 41)` |
| `--success` | `oklch(47.1% 0.097 158)` | `oklch(78.5% 0.135 160)` |
| `--success-tint` | `oklch(93.3% 0.023 158)` | `oklch(27.5% 0.040 166)` |
| `--warning` | `oklch(50.8% 0.108 73)` | `oklch(80.6% 0.147 79)` |
| `--warning-tint` | `oklch(94.1% 0.039 89)` | `oklch(28.3% 0.042 86)` |
| `--danger` | `oklch(50.1% 0.178 29)` | `oklch(73.2% 0.164 27)` |
| `--danger-tint` | `oklch(92.5% 0.026 27)` | `oklch(25.7% 0.056 27)` |
| `--focus-ring` | `oklch(24.4% 0.027 153)` | `oklch(86.3% 0.195 124)` |
| `--overlay` | `oklch(17.7% 0.015 154)` | `oklch(12.8% 0.011 145)` |

### 2.3 Contrast verification

Measured with the WCAG 2.x relative-luminance formula. Body copy targets 4.5:1, large text and meaningful graphics 3:1.

| Pair | Ratio | Required | Result |
|---|---:|---:|---|
| Text on `--bg` (light) | **14.49:1** | 4.5 | Pass |
| Text on `--surface` (light) | **15.51:1** | 4.5 | Pass |
| Muted text on `--bg` (light) | **5.96:1** | 4.5 | Pass |
| Muted text on `--surface` (light) | **6.38:1** | 4.5 | Pass |
| `--primary-contrast` on `--primary` (light) | **10.73:1** | 4.5 | Pass |
| `--primary-contrast` on `--primary-hover` (light) | **8.92:1** | 4.5 | Pass |
| `--primary-contrast` on `--primary-press` (light) | **6.63:1** | 4.5 | Pass |
| `--accent-ink` on `--surface` (light) | **5.08:1** | 4.5 | Pass |
| `--accent-ink` on `--accent-tint` (light) | **4.56:1** | 4.5 | Pass |
| `--accent` star fill on `--surface` (light) | **3.35:1** | 3.0 | Pass (graphic) |
| `--success` on `--surface` (light) | **6.23:1** | 4.5 | Pass |
| `--warning` on `--surface` (light) | **5.70:1** | 4.5 | Pass |
| `--danger` on `--surface` (light) | **6.28:1** | 4.5 | Pass |
| `--border-strong` on `--surface` (light) | **4.05:1** | 3.0 | Pass (control boundaries) |
| Text on `--bg` (dark) | **16.51:1** | 4.5 | Pass |
| Text on `--surface` (dark) | **14.97:1** | 4.5 | Pass |
| Muted text on `--bg` (dark) | **8.66:1** | 4.5 | Pass |
| `--primary-contrast` on `--primary` (dark) | **12.95:1** | 4.5 | Pass |
| `--accent-ink` on `--surface` (dark) | **8.89:1** | 4.5 | Pass |
| `--accent` star fill on `--surface` (dark) | **7.34:1** | 3.0 | Pass |
| `--success` on `--surface` (dark) | **9.16:1** | 4.5 | Pass |
| `--warning` on `--surface` (dark) | **9.18:1** | 4.5 | Pass |
| `--danger` on `--surface` (dark) | **6.72:1** | 4.5 | Pass |
| `--border-strong` on `--surface` (dark) | **3.73:1** | 3.0 | Pass |
| **Lime on cream** (`--primary` on `--bg`) | **1.35:1** | 3.0 | **Fail — never use as ink** |

Notes:

- `--border` (1.35:1 light, 1.55:1 dark) is decorative only. Any border that tells you where an input starts or that divides interactive rows uses `--border-strong`.
- `--text-disabled` is 3.02:1 on the light surface and 3.90:1 on the dark surface. Disabled text is exempt from WCAG, but keep it above 3:1 anyway; pair disabled states with `aria-disabled`.
- Text on lime is always `--primary-contrast`, never `--text`. In dark mode, cream `--text` on lime is only 1.27:1.
- Focus indicators are checked separately in section 7.

### 2.4 CSS custom properties

Theme selection: Go sets `data-theme="light|dark"` on `<html>` from the user's cookie. If no cookie exists, a small inline script in `<head>`, before the stylesheet, sets the attribute from `localStorage` or `matchMedia("(prefers-color-scheme: dark)")`. `:root` is light, so the no-JS default is a readable light app with no flash. Do not animate the theme switch.

```css
/* tokens.css — single source of truth */
:root {
  color-scheme: light;

  /* Surfaces */
  --bg: #F2F4E6;
  --surface: #FBFBF2;
  --surface-raised: #FFFFFF;
  --surface-sunken: #E9EDDA;

  /* Lines */
  --border: #D8DCC2;
  --border-strong: #74805A;

  /* Ink */
  --text: #16241A;
  --text-muted: #55613D;
  --text-disabled: #8B957B;

  /* Lime */
  --primary: #B4E32C;
  --primary-hover: #9FD11A;
  --primary-press: #86B50D;
  --primary-contrast: #16241A;
  --primary-tint: #E7EFCB;

  /* Tangerine */
  --accent: #E85D26;
  --accent-ink: #BC440F;
  --accent-tint: #FCEBE2;

  /* Status */
  --success: #1D6B45;
  --success-tint: #DDEEE3;
  --warning: #8A5A00;
  --warning-tint: #F6EBCF;
  --danger: #B3261E;
  --danger-tint: #F7E0DD;

  /* Focus and scrims */
  --focus-ring: #16241A;
  --focus-halo: #E7EFCB;
  --overlay: #0C130E;
  --scrim: rgb(12 19 14 / 0.60);

  /* Fixed scanner surface — dark in both themes */
  --scanner-scrim: rgb(12 19 14 / 0.72);
  --scanner-text: #EEF2E0;
  --scanner-text-muted: #A8B49A;
  --scanner-chrome: #151E17;
  --scanner-border: #31402F;
  --scanner-lime: #B9E63A;
  --scanner-edge: rgb(238 242 224 / 0.25);
  --scanner-wash: rgb(185 230 58 / 0.12);

  /* Type */
  --font-sans: "Bricolage Grotesque", ui-sans-serif, system-ui, -apple-system,
    "Segoe UI", Roboto, Arial, sans-serif;
  --fs-display: clamp(2.125rem, 8vw, 2.75rem);
  --lh-display: 1.05;
  --fs-title: 1.75rem;
  --lh-title: 1.15;
  --fs-heading: 1.375rem;
  --lh-heading: 1.27;
  --fs-subhead: 1.0625rem;
  --lh-subhead: 1.41;
  --fs-body: 0.9375rem;
  --lh-body: 1.47;
  --fs-body-lg: 1.0625rem;
  --lh-body-lg: 1.53;
  --fs-label: 0.8125rem;
  --lh-label: 1.38;
  --fs-meta: 0.75rem;
  --lh-meta: 1.33;
  --fs-overline: 0.6875rem;
  --lh-overline: 1.27;
  --fs-rating-hero: 3.25rem;
  --lh-rating-hero: 1;
  --fs-rating-card: 1.25rem;
  --lh-rating-card: 1.2;

  --fw-regular: 400;
  --fw-medium: 500;
  --fw-semibold: 600;
  --fw-bold: 700;
  --fw-black: 800;

  --tracking-tight: -0.03em;
  --tracking-snug: -0.015em;
  --tracking-normal: 0;
  --tracking-wide: 0.08em;

  /* Space — 4px grid */
  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-5: 20px;
  --space-6: 24px;
  --space-8: 32px;
  --space-10: 40px;
  --space-14: 56px;
  --space-20: 80px;

  /* Shape */
  --radius-xs: 6px;
  --radius-sm: 10px;
  --radius-md: 14px;
  --radius-lg: 20px;
  --radius-full: 999px;

  /* Layout */
  --gutter: 16px;
  --header-h: 56px;
  --nav-h: 64px;
  --content-max: 640px;
  --rail-w: 232px;
  --tap-min: 44px;
  --tap-lg: 48px;
  --safe-b: env(safe-area-inset-bottom, 0px);
  --safe-t: env(safe-area-inset-top, 0px);

  /* Motion */
  --dur-1: 90ms;
  --dur-2: 120ms;
  --dur-3: 180ms;
  --dur-4: 220ms;
  --dur-5: 260ms;
  --ease-out: cubic-bezier(0.2, 0.7, 0.2, 1);
  --ease-sheet: cubic-bezier(0.32, 0.72, 0, 1);
  --ease-snap: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-in-out: cubic-bezier(0.45, 0, 0.55, 1);

  /* Elevation — cards use borders, not shadows */
  --shadow-1: 0 1px 2px rgb(22 36 26 / 0.06), 0 2px 6px rgb(22 36 26 / 0.05);
  --shadow-sheet: 0 -8px 32px rgb(22 36 26 / 0.14);
  --shadow-scanner: 0 2px 12px rgb(0 0 0 / 0.35);
}

[data-theme="dark"] {
  color-scheme: dark;

  --bg: #0C130E;
  --surface: #151E17;
  --surface-raised: #1E2A20;
  --surface-sunken: #080E09;

  --border: #31402F;
  --border-strong: #6B7A63;

  --text: #EEF2E0;
  --text-muted: #A8B49A;
  --text-disabled: #6F7D66;

  --primary: #B9E63A;
  --primary-hover: #C9F45A;
  --primary-press: #A6D028;
  --primary-contrast: #0C130E;
  --primary-tint: #26331C;

  --accent: #FF8A57;
  --accent-ink: #FFA57F;
  --accent-tint: #3A2118;

  --success: #5FD39A;
  --success-tint: #122E23;
  --warning: #F2B33D;
  --warning-tint: #33280F;
  --danger: #FF7A6E;
  --danger-tint: #3A1714;

  --focus-ring: #B9E63A;
  --focus-halo: #B9E63A38;
  --overlay: #050805;
  --scrim: rgb(5 8 5 / 0.72);

  --shadow-1: 0 1px 0 rgb(238 242 224 / 0.04);
  --shadow-sheet: 0 -8px 32px rgb(0 0 0 / 0.50);
}

@media (min-width: 600px) {
  :root {
    --gutter: 20px;
  }
}

@media (min-width: 1024px) {
  :root {
    --gutter: 24px;
    --content-max: 720px;
    --fs-body: 1rem;
    --lh-body: 1.5;
  }
}
```

Base layer (the only component-adjacent CSS in this document):

```css
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font-family: var(--font-sans);
  font-size: var(--fs-body);
  line-height: var(--lh-body);
  font-weight: var(--fw-regular);
  -webkit-font-smoothing: antialiased;
  text-rendering: optimizeLegibility;
}

:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 2px;
  box-shadow: 0 0 0 5px var(--focus-halo);
}

/* Focus rings sit 2px outside the control, against the page or surface.
   Never set outline-offset: 0 on a lime fill: the ring must land on the
   surrounding color, not on the lime itself. */

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
```

---

## 3. Typography

**One family, no pairing.** **Bricolage Grotesque** variable, self-hosted as a single latin woff2 subset (weights 400–800, optical size axis enabled, `font-display: swap`). Fallback: `ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, Arial, sans-serif`.

Why: its slightly off-square bowls and tight, high-set letterforms feel like a modern soda label rather than another geometric SaaS grotesque. It holds up at 12px metadata because of the optical-size axis, and it has enough weight range to make a 52px rating numeral feel physical without a second display face. Do not load a separate display font, and do not use italics — the extra file is not worth it.

`font-optical-sizing: auto` is default; leave it on. Preload the single woff2 and set `font-display: swap`. Test the swap at 320px; if the fallback reflows titles, add a metric-matched fallback override rather than a second family.

### 3.1 Scale

| Token | Size / line height | Weight | Tracking | Use |
|---|---|---|---|---|
| `--fs-display` | clamp(34–44px) / 1.05 | 800 | -0.03em | Splash, empty-state hero only |
| `--fs-title` | 28px / 32px | 800 | -0.02em | Screen and venue titles |
| `--fs-heading` | 22px / 28px | 700 | -0.015em | Sheet titles, section headers |
| `--fs-subhead` | 17px / 24px | 700 | -0.005em | Drink names, list-row titles |
| `--fs-body-lg` | 17px / 26px | 400 | 0 | Notes, venue description |
| `--fs-body` | 15px / 22px | 400 | 0 | Default body on mobile |
| `--fs-label` | 13px / 18px | 600 | 0 | Buttons, chips, field labels |
| `--fs-meta` | 12px / 16px | 500 | 0 | Sugar, caffeine, counts, timestamps |
| `--fs-overline` | 11px / 14px | 700 | +0.08em | Section eyebrows, "Locked" |
| `--fs-rating-hero` | 52px / 52px | 800 | -0.04em | Check-in and detail rating |
| `--fs-rating-card` | 20px / 24px | 700 | 0 | Feed and card ratings |

Desktop changes one thing: prose body becomes 16px/24px at ≥1024px; component labels keep their mobile sizes. The layout gets wider, not louder. Never use all-caps for anything longer than two words. Inline links use `--accent-ink` (5.08:1 on `--surface`, 4.75:1 on `--bg`) with a 1px underline and 2px offset; never lime links. Do not underline non-link text.

### 3.2 Numeric rating rules

- Scale is 0–5 in quarter steps. Store as an integer 0–20 (quarter points), not a float.
- **Lists:** one decimal (`4.3`). **Detail and check-in:** one decimal minimum, two only when the value is a quarter (`4.25`), trailing zeros kept to one place (`4.0`, `4.5`). Never show `0.0` for unrated — show “Not rated” (or an em dash in dense rows).
- Always show the denominator once per component: `4.25` at full size, `/5` at 0.6em, muted, baseline-aligned. Never repeat “/5” in a table row.
- Turn on `font-variant-numeric: tabular-nums` and `font-feature-settings: "tnum" 1`. If the shipped cut lacks tabular figures, reserve a fixed 3.5ch slot and center the numeral; a changing score must never reflow the row.
- Stars are never the only representation. The numeral is always adjacent. Rating counts are separate muted text (“128 ratings”), never inside the star row.
- No star characters (`★`) in running text or copy. Stars are SVG, `aria-hidden`, with a text label on the container.
- In practice: card `4.3`; detail `4.25 / 5`; unrated `Not rated`; rating count `128 ratings`.

---

## 4. Space, shape, elevation

- **Grid:** 4px base. Use 4/8/12/16/20/24/32/40/56/80. Gutters 16px, 20px at ≥600px, 24px at ≥1024px. Section gaps 24px, card gaps 12px.
- **Layout:** mobile content max 640px. Header 56px, bottom nav 64px plus safe area, scroll content bottom padding `calc(var(--nav-h) + var(--safe-b) + 12px)` so the lifted scan button never covers the last row. At ≥1024px: fixed left rail 232px, content column max 720px, no third column. Bottom nav disappears; scan becomes a full-width 44px lime button at the top of the rail.
- **Radii:** 6px for checkboxes, small counters, and inline tags; 10px for buttons and inputs; 14px for cards; 20px for sheets and desktop dialogs; full for chips and avatars. Product images 10px. Keep cards at 14px and buttons at 10px; don't drift to 8 or 18 on a whim.
- **Elevation:** light theme cards are `--surface` with a 1px `--border` edge and no shadow. Shadows are only for bottom sheets, popovers, scanner controls, and the sticky sheet footer. Dark theme separates with `--surface-raised` plus a 1px border, not heavier shadows.
- **Imagery:** cans 1:1, bottles 3:4, `object-fit: contain`, 8% inner padding on `--surface-sunken`. Product photos are clean cutouts with no baked-in shadow. Missing image: a 10px-radius tinted tile with the brand's initial at 24px/800 in `--primary-contrast` on `--primary-tint`; never a gray question mark or stock photo.
- **Icons:** 1.5px stroke at 20/24px, round caps and joins. One filled state only: active bottom-nav icons. Icons on cream use `--text`, never lime. A custom barcode icon is allowed to be chunkier (2px bars) for recognition.

---

## 5. Components

All tap targets are at least 44×44px; scanner and sheet primary controls are 48px or larger. Disabled controls use `--surface-sunken` with `--text-muted` and `aria-disabled`; never dim an entire control with opacity. All measurements are mobile-first.

### 5.1 App shell and navigation

- **Header:** 56px plus `--safe-t` in standalone PWA, sticky, `--surface`, 1px bottom `--border`. 16px gutter.
  - Feed: lowercase wordmark “sodapop”, 22px/1, 800, tracking -0.045em, `--text`. No logo mark, no gradient. Right side: one search icon button, 44×44, 20px icon. Scan lives in the center of the nav, not in the header.
  - Inner screens: 44×44 back button, 16px icon, then title at 17px/24px 700, truncated after one line.
- **Bottom nav:** 64px plus `--safe-b`, 5 slots — Feed, Discover, Scan, Venues, You. Labels 11px/14px 600, 4px below the 24px icon.
  - Active item: a 44×28 `--primary-tint` pill behind a filled 20px icon, label 700 and `--text`. Inactive: outline icon and `--text-muted`. Active state is not color-only; the fill and weight change too.
  - Scan is the center 56px circle: `--primary` fill, `--primary-contrast` 26px barcode icon, 2px `--surface` ring, 1px `--border-strong` edge in light mode, `--shadow-1`, lifted 12px. Label “Scan” sits in the nav below it. Never hide the label.
  - Nav background is `--surface` with a 1px top border; no blur, no translucency.
- **Desktop rail:** wordmark at top, Feed, Discover, Venues, and You as 44px rows, then a full-width 44px lime scan button. Rail background `--bg` with a right border; content background `--bg`. No decorative graphics.

### 5.2 Drink card

- Single column in the feed, max 640px; two columns of 348px at ≥1024px with a 24px gap.
- Card: `--surface`, 1px `--border`, 14px radius, 12px padding. 12px between cards.
- **Image:** 72×88px, 10px radius, `--surface-sunken`, 8% padding, cutout can or bottle. The image is not the link target by itself; the whole card is one link.
- **Text column:** 12px gap after image.
  - Name: 17px/22px, 700, `--text`, clamp to 2 lines.
  - Brand: 13px/18px, 500, `--text-muted`, 1 line.
  - One style chip max: 24px pill, `--primary-tint` background, `--text` label 12px/16px 600, 10px horizontal padding. No outline.
- **Rating block:** right-aligned, 56px wide: score 20px/24px 700 tabular, with a 12px tangerine star to its right. No “/5” on cards. The number is `--text`, never accent-colored.
- **Meta row:** 12px/16px, `--text-muted`, full card width below the image/text row with 8px top margin, one line, middle dots: container size, sugar per container, caffeine per container; ABV only when above 0. Max three items, then “+1”. Example: `355 ml · 39 g sugar · 34 mg caffeine`. Units are lowercase, numbers are not localized.
- **States:** pressed background `--surface-sunken` (no scale). Hover at pointer devices: `--border-strong` and `translateY(-1px)` over 120ms. No shadow, no chevron.
- Score absent: “Not rated” at 12px/16px muted in the rating slot; keep the slot width so the grid does not jump.

### 5.3 Check-in sheet

- **Mobile:** bottom sheet, 20px top radius, max-height 92vh, drag handle 36×4px `--border-strong` at 8px from the top, 60% `--scrim` overlay. Dragging down past 96px dismisses; less than that springs back in 220ms. **Desktop:** centered 480px dialog, 20px radius all around, max-height 80vh, same scrim, drag handle hidden.
- **Header:** 22px/28px 700 “Check in”, close button 44×44 at the right, 16px padding.
- **Product row:** 56px tall: 40×48px thumb, name 15px/22px 600, barcode 12px/16px muted in tabular figures, “Change” text button with a 44px target on the right, 8px gap. Bottom hairline `--border`.
- **Rating block:** 24px vertical padding, centered. Readout 52px/52px 800 tabular with `/5` at 20px muted; 4px gap; stars 32px visual in 44px hit areas; helper 12px/16px muted “Drag or tap to rate” below.
- **Taste:** label 13px/18px 700, optional helper 12px muted “Pick up to 4”, 8px gap, wrapping chips. 8px between chips.
- **Notes:** 3-row textarea, 15px/22px, 12px padding, 10px radius, `--border-strong` 1px border; label above, helper “Optional — what stood out?” below at 12px muted. No placeholder-only labeling.
- **Rows:** venue and date as 44px rows with a 20px icon, label 13px muted above value 15px/22px 600, chevron 16px on the right, hairline dividers. Date defaults to today.
- **Photo:** 88×88 tile, 10px radius, dashed 1px `--border-strong`, camera icon 24px and “Add photo” 12px label. One photo maximum; no carousel in v1.
- **Footer:** sticky, 16px padding plus `--safe-b`, top border. Primary “Post check-in” 52px tall, full width, `--primary`, 15px/20px 700, 10px radius, 1px `--border-strong` edge in light mode. Disabled state is `--surface-sunken` with `--text-muted` (5.56:1 in light), not 40% opacity. No secondary button; the close button is the cancel.
- **Validation:** 13px/18px `--danger` below the field, with a 16px alert icon and the field border switched to `--danger`. Focus moves to the first invalid field. Never rely on red alone: the message carries the error.
- **Success:** sheet closes in 220ms; a 56px confirmation bar appears at the top of the current screen: `--success-tint`, 20px success check, “Check-in posted” 15px/22px 600, “View” action with a 44px target, and a 44px dismiss button. Auto-dismiss after 6s, paused on hover or focus.

### 5.4 Barcode scanner

The scanner is always dark, in both themes, because it sits over a camera feed. It uses the fixed `--scanner-*` tokens.

- **Chrome:** full viewport, camera feed behind a 72% `--scanner-scrim`. Top bar respects `--safe-t`. Close button 48px on the left, torch 48px on the right.
  - Close: solid `--scanner-chrome`, 1px `--scanner-border`, cream icon.
  - Torch: same resting style; active is `--scanner-lime` fill with a dark icon. Persist torch state for the session.
- **Viewfinder:** 280×160px on screens 360px and wider; 240×140px below that. 16px radius, centered at 42% of viewport height. Outside the window, the scrim dims the feed. A 1px `--scanner-edge` inner line defines the window without a heavy frame. Four corner brackets: 24px long, 3px `--scanner-lime`, 4px outer radius.
- **Copy:** 16px below the window, 15px/22px `--scanner-text`, “Point at a barcode”; below it 12px/16px `--scanner-text-muted`, “Usually on the back or side of the can.” No arrows, no laser line, no beep by default.
- **Idle motion:** brackets breathe between 55% and 100% opacity over 1600ms `--ease-in-out`. Stop after 12 seconds to save battery; stop immediately on detection. This is the only looping animation in the app.
- **Detect:** freeze the frame. Brackets snap inward 8px (to 264×144) over 160ms `--ease-snap`; a `--scanner-wash` crosses the window over 120ms; a 40px check and product line appear (“Cola · 355 ml can”, 15px/22px 600). Haptic 10ms. Hold the success state for 600ms, then open the check-in sheet; a tap opens it immediately. Announce via a polite live region and move focus to the sheet heading.
- **No match:** a bottom panel with 20px top radius, `--scanner-chrome`, 20px padding. “No match for 0123456789012” at 15px/22px, then a 48px lime “Add this soda” and a 48px outlined “Scan again” (`--scanner-border`, cream text). No full-screen red, no shake.
- **Permission denied:** centered panel in the same chrome: “Camera unavailable” 17px/24px 700, one sentence of explanation 14px/20px muted, 48px lime “Enter barcode”, text link “Open settings”. Manual entry is never hidden behind this state.
- **Manual entry:** always visible at the bottom, 8px above `--safe-b`: full-width 48px button, solid `--scanner-chrome`, 1px `--scanner-border`, cream label “Enter barcode”. Opens a dark manual sheet in scanner chrome: 18px numeric input (prevents iOS zoom), 48px tall, `inputmode="numeric"`, 10px radius, label above, 48px lime submit “Find soda”. Errors appear inline in `--danger` with text, not color alone.

### 5.5 Rating control

- Five stars, 32px glyphs in 44×44px hit areas, total 220px, centered. Filled with `--accent`; empty is a 1.5px `--border-strong` outline with transparent fill. Never a gray solid star.
- Interaction: drag horizontally to set quarter increments; tap sets the nearest quarter; pointer down does not jump until 8px of movement, so a tap is not a drag. Set `touch-action: pan-y` on the row so a vertical swipe still scrolls the sheet. A floating value bubble (40×28px, fixed ink `#16241A` background, cream `#FBFBF2` text, `--radius-sm`) sits 24px above the finger during drag and disappears on release. The star fill is clipped at the exact fraction — 4.25 reads as four full stars plus a quarter wedge — and the numeral carries the exact value.
- Readout above: 52px/52px 800 tabular plus `/5` at 20px muted. Unrated shows “Not rated” at 17px/24px 600 and all stars outlined.
- Haptics: 10ms tick at whole-star steps only (1.0, 2.0 … 5.0). Do not buzz every quarter.
- Keyboard and screen reader: `role="slider"`, `aria-valuemin="0"`, `aria-valuemax="5"`, `aria-valuenow` (0 only when unrated), `aria-valuetext="4.25 out of 5"` or “Not rated”. Arrow keys ±0.25, Page Up/Down ±1, Home 0, End 5.
- Optional row below: “Would drink again” 32px pill with a 16px icon and text; selected uses `--success-tint` background, `--success` text, and a check icon. Selected state is never color-only.
- Rating colors are fixed across themes: tangerine fill, pine/cream numerals. No red for low scores and no green for high scores.

### 5.6 Style chips

- 32px tall visually, wrapped in a 44px-tall hit area; full radius; 12px horizontal padding; 13px/18px 600; 8px gap.
- Unselected: transparent background, 1px `--border-strong`, `--text` label. Hover: `--surface-sunken`.
- Selected: `--primary` fill, `--primary-contrast` label, 1px `--border-strong` border in light mode (none in dark), 14px check icon before the label, pressed `--primary-press`. Selection is communicated by fill, check, and weight — not color alone.
- Detail screens wrap chips, max 12, left-aligned. Card feeds show one chip and no horizontal scroller. A chip label never truncates; shorten the taxonomy instead (“Cream soda”, not “Cream soda, vanilla”).
- Diet and zero-sugar chips are neutral, not green. They are facts, not achievements.

### 5.7 Badge / achievement tile

- Grid: `repeat(auto-fill, minmax(96px, 1fr))`, 8px gap; 3-up at 360px and wider.
- Tile: 14px radius, matching cards. Min-height 120px, 12px padding.
- **Earned:** `--primary-tint` background, 1px `--border-strong` inset edge, 40px line-art glyph in `--primary-contrast` (1.75px stroke), name 12px/16px 700, date 11px/14px muted. A 16px `--primary` dot with a dark check sits top-right.
- **Locked:** `--surface-sunken` background, 1px dashed `--border-strong`, glyph in `--text-muted`, name in `--text-muted`, overline “Locked”. No blur filter, no opacity on the text, no padlock emoji.
- No metallic finishes, shine sweeps, or confetti. The earned state is a quiet flip from dashed to tinted.
- Badge detail is a normal page, not a modal. Grid order is earned first, then locked.

### 5.8 Venue page header

- Cover: full-bleed 160px (200px at ≥600px). Photo if available; otherwise `--surface-sunken` with a centered 64px venue glyph in `--border-strong`. No generated gradients.
- Back button: 44px solid `--surface` circle with `--shadow-1`, `--text` icon, 8px from `--safe-t` and the gutter. Share and save use the same solid circles on the right, 8px gap.
- Content sheet overlaps the cover by 24px, top radius 20px, `--surface`, 20px top padding, 16px sides.
- Name 28px/32px 800, tracking -0.02em, up to 2 lines. Type and neighborhood 13px/18px muted on one line. “Open now” is 12px/16px `--success` with a 6px dot **and** the word; never a bare dot.
- Primary “Check in here” 48px, full width, 12px below the meta, lime with a 1px `--border-strong` edge in light mode. Save and share remain 44px icon buttons with accessible labels.
- Stats row: three equal columns (check-ins, average rating, saved), 12px gap, 1px left dividers, numbers 20px/24px 700 tabular, labels 11px/14px overline muted.
- Tabs: 48px tall, sticky below the header. Recent / Menu / Info at 15px/20px 600. Active is `--text` with a 3px `--text` bottom bar; inactive is `--text-muted`. Lime is not a tab indicator on cream — it fails the 3:1 non-text rule and reads as decoration.
- Menu tab: simple two-column list of soda name and price/volume, 44px rows; no card grid, no photos in v1.

### 5.9 Friend activity row

- 64px tall, 16px horizontal padding, 1px `--border` divider inset 52px from the left (36px avatar + 16px). Whole row is a link.
- Avatar 36px circle, image or the person's initial at 14px/16px 700 on `--primary-tint`; 8px gap. A 36px avatar is acceptable because the row is the target, not the avatar.
- Line 1: name 15px/22px 600, then “checked in” 13px/18px muted, then drink name 15px/22px 600. Wrap to a maximum of two lines; drink name is the truncation point.
- Line 2: 12px/16px muted — venue name and rating with a 12px star and the number. Time sits top-right, aligned with line 1; 12px/16px muted.
- Thumbnail 40×40px, 8px radius, `--surface-sunken`, right-aligned. If no image, omit the thumb; do not show a placeholder here.
- Pressed: `--surface-sunken`; hover: divider stays, background shifts to `--surface-raised` at ≥900px. No like button in the row; likes live on the check-in detail.

### 5.10 Empty states

- Centered, 64px top padding, text max 32ch, 96px line-art illustration at 1.5px stroke in `--border-strong` (or a shape filled with `--primary-tint`). No characters, no mascots, no stock art.
- Stack: illustration, 16px, title 17px/24px 700, 4px, body 14px/20px muted, 20px, one primary 44px button (auto width, min 160px), optional 44px text link.
- Plain copy, no jokes:
  - Feed: “No check-ins yet” / “Scan a soda or follow someone to fill this.” / “Scan a soda”.
  - Search: “No sodas match ‘root’” / “Check the spelling or add it as a new soda.” / “Add a soda”.
  - Badges: “No badges yet” / “Check in five different sodas to earn the first one.” / “Browse badges”.
  - Venue: “No check-ins here yet” / “Be the first to log a soda from this venue.” / “Check in here”.
- Scanner failures use the panel patterns in 5.4, not empty states.

---

## 6. Motion

Motion is used to confirm cause and effect, not to decorate. Three interactions, plus state feedback.

| Interaction | Specification | Reduced motion |
|---|---|---|
| **First feed paint** | First 8 cards: opacity 0→1 and `translateY(8px)→0`, 220ms `--ease-out`, 30ms stagger. Runs once per screen per session, not on scroll-back. | No transform or stagger; cards appear at full opacity. |
| **Check-in submit** | Button press scales to 0.98 in 90ms `--ease-out`; sheet closes over 220ms `--ease-sheet` while the scrim fades out in 180ms; once the sheet has closed, the confirmation bar enters in 180ms with an 8px rise. | Instant state change; the confirmation bar appears with no fade or rise. |
| **Scanner detect** | Brackets snap inward over 160ms `--ease-snap`; a `--scanner-wash` crosses the window over 120ms; then the check fades and scales from 0.9 over 180ms; idle breathing stops on detect. | Brackets jump to the detected position; wash replaced by a static `--scanner-wash` state; check appears instantly. |

State feedback: hover and focus transitions are 120ms `--ease-out`; pressed scale 0.98 at 90ms; no transition on color tokens during theme switches.

Rules: animate only `opacity` and `transform`; never width, height, top, or left. No element animates more than two properties. Nothing loops except the scanner idle breathing, which stops after 12 seconds; no auto-playing sound; haptics only where specified (10ms). No parallax, no spring overshoot above 1.02, no scroll-jacking.

---

## 7. Accessibility

- **Contrast:** all body text meets 4.5:1 (light body on cream is 14.49:1, on cards 15.51:1; dark is 16.51:1 and 14.97:1). Text on lime is 10.73:1 light and 12.95:1 dark. Muted text is ≥5.96:1 light and ≥8.66:1 dark. Meaningful graphics and control borders meet 3:1 (`--border-strong` is 4.05:1 light and 3.73:1 dark). The one forbidden combination — lime as ink on cream at 1.35:1 — is banned rather than worked around.
- **Lime usage:** in light mode, lime is only a filled area with `--primary-contrast` text/icons, never text, thin strokes, dividers, or a lone focus ring. In dark mode lime may be used for text, but reserve it for short emphasis; body copy stays `--text`.
- **Tap targets:** 44×44px minimum, 8px separation between adjacent targets; 48px minimum for scanner controls; 52px for the check-in submit; bottom-nav items are the full 64px height; star hit areas 44px; chips get a 44px hit wrapper around the 32px visual.
- **Focus:** visible on every interactive element via `:focus-visible`, never removed. Default is a 2px `--focus-ring` outline at 2px offset plus a 5px `--focus-halo` glow. The ring is drawn on the surrounding surface, not on the fill: ink on cream (14.49:1) in light, lime on green-black (12.95:1) in dark. Keep the 2px offset on lime fills; if an inset ring is unavoidable, use `--primary-contrast` (10.73:1 on light lime, 12.95:1 on dark lime). On cards, use a 2px inset ring so it is not clipped by overflow. Include a 44px “Skip to content” link that appears on focus. Check forced-colors mode: outlines survive, and no state depends on a background image.
- **Color-blind safety:** ratings are numeral + fractional star fill + accessible text; style, diet, sugar, and caffeine facts are text; success and danger always pair icon + word; the scanner success uses a check + product name; venue open status uses the word “Open”. Never encode a state in hue alone. Tangerine and lime separate primarily by lightness (OKLCH 65% vs 85%), which holds up in protanopia and deuteranopia simulations; the numeral is always the source of truth.
- **Screen readers:** rating slider exposes value and text (section 5.5); scanner announces detections through `aria-live="polite"` then moves focus to the product heading; nav uses `aria-current="page"`; tabs use a tablist with `aria-selected`; images use the product name and container (“Cola, 355 ml can”), not “image”; decorative illustration is `aria-hidden`; icon-only buttons have accessible names (“Search”, “Close scanner”, “Torch”).
- **Forms:** visible labels, 44px minimum field height, errors linked by `aria-describedby`, 18px input text for the barcode field to prevent iOS zoom, `autocomplete` on venue search, no placeholder-as-label.
- **Zoom and reflow:** works at 320px width and 200% zoom with no horizontal page scroll; chip rows may scroll horizontally with a visible edge fade; drink names clamp to 2 lines, never truncating the brand or score; sheet content scrolls with the footer pinned.
- **Motion and sound:** `prefers-reduced-motion` removes transforms, stagger, and the scanner breathing loop; the app is silent by default; haptics are supplementary and never the only feedback.
- **Dark mode:** not an inversion. Cream becomes near-black green, ink becomes warm bone (`#EEF2E0`, never pure white), lime lightens slightly for punch, and images sit on `--surface-sunken` to avoid glowing cutouts.

---

## 8. Design anti-patterns for this app

1. **Lime text or lime icons on cream.** It reads as unfinished or broken. Lime is a fill.
2. **Neon lime on pure black.** Gamer, not grocery. The dark theme is green-black, and lime is a material, not a light source.
3. **Beer-app cosplay.** Brown/amber palettes, dark wood textures, pint glasses, "brewery" language. Soda is its own category.
4. **Retro diner pastiche.** Checkerboard, chrome, cursive scripts, "Since 1952" badges. The type and color already carry the personality.
5. **Gradient SaaS styling.** Purple-blue hero gradients, glass cards, floating blobs, gradient text. Flat cream surfaces and hairlines only.
6. **Stars without numbers.** A 4.25 rating must read as 4.25 on detail, not "about four stars". Also no red-for-bad/green-for-good scores.
7. **Color-only states.** Selected chips, scanner success, venue open, form errors — always a second cue.
8. **Sugar shaming.** Red warning triangles on sugar or caffeine, "guilt-free" copy, calorie leaderboards. State the number; skip the verdict.
9. **Tiny targets.** 32px icon buttons, closely packed stars, a torch button in the status bar. 44px minimum, 48px in the scanner.
10. **Modal-first onboarding.** No carousel, no permission wall, no account gate before the first scan. The camera prompt appears when the scanner opens, with manual entry beside it.
11. **Decoration competing with the can.** Heavy shadows, busy patterns, sticker overlays, or a second accent hue. Product imagery is the loudest thing on the card.
12. **Fake delight.** Confetti on check-in, burp sounds, mascot reactions, badge shine sweeps. Confirmation is a quiet bar and a check.
13. **Infinite scanner gimmicks.** Laser beams, beeps, auto-zoom pulses, looping animations beyond the one breathing state.
14. **Bubbly everything.** Full-round cards, pill inputs, pill buttons, and pill chips at once. Radii are assigned by role (6/10/14/20/full), not by mood.

---

## 9. Handoff notes

- Copy the tokens block into `web/static/tokens.css` (or equivalent) without renaming tokens. Colors are unprefixed semantic names; everything else uses `--fs-`, `--space-`, `--radius-`, `--dur-`, `--ease-`, `--shadow-`.
- If a utility CSS layer is introduced later, map utilities 1:1 to these tokens. Do not create a second spacing or type scale.
- Set `data-theme` on `<html>` server-side from the cookie; use the inline pre-paint script only when no cookie exists. Add `theme-color` metas for `#F2F4E6` and `#0C130E`; update the active one if the user overrides the OS theme.
- The scanner overlay ignores the theme on purpose. Do not "fix" it to match light mode.
- Icons: 1.5px stroke set at 20/24px; the barcode glyph is the one exception at 2px bars.
- Never set `outline-offset: 0` on a lime-filled control; the focus ring must land on the surrounding surface. If a control genuinely needs an inset ring, use `--primary-contrast`. `data-theme` on `<html>` is the only theme attribute; do not invent other tone attributes.
- Validation for this document: orchestrator reviews all UI copy against section 1's voice rules and confirms the palette tokens match the role table exactly.
