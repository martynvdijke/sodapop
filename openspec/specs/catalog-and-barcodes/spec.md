# Catalog, Soda Taxonomy, and Barcode Scanning Specification

## Purpose

Defines the shared soda catalog (`Brand`, `Style`, `Flavour`, `Drink`, `DrinkBarcode`, `DrinkAlias`, `OffProduct`), browser and worker barcode decoding, GTIN-14 normalization, Open Food Facts v3.6 lookup with ODbL isolation, and unknown-soda provisional creation.

## Requirements

### Requirement: Two-Level Soda Style Taxonomy and Flavour Descriptors
The system SHALL model drinks with a parent/substyle hierarchy (`styles.parent_id`), intensity-scored flavour tags (`drink_flavours.intensity` 1–5), and soda-specific descriptors (`sweetener`, `carbonation`, `acid`, `is_diet`, `is_caffeine_free`, nutrition per 100 ml).

#### Scenario: Seeded taxonomy and FTS5 synchronization
- **WHEN** `cmd/seed` runs or a catalog drink/alias is created, updated, or merged
- **THEN** the 17 parent style families and core flavours exist in SQLite
- **AND** the standalone FTS5 table `drinks_fts(name, brand, style, aliases)` is updated in the same database transaction

### Requirement: Web Barcode Scanner with Mandatory WebAssembly Fallback
The web client SHALL decode EAN-13, EAN-8, UPC-A, UPC-E, and Code 128 barcodes using native `BarcodeDetector` when supported and dynamically load `barcode-detector` (ZXing-C++ WebAssembly in a Web Worker) on iOS Safari, Firefox, and desktop browsers without native support.

#### Scenario: GTIN-14 normalization and GS1 check-digit validation
- **WHEN** a camera frame or manual entry yields an 8, 12, 13, or 14-digit code
- **THEN** the client and server validate the GS1 check digit and left-pad with zeros to canonical 14-digit GTIN-14
- **AND** a 12-digit UPC-A and its zero-prefixed 13-digit EAN-13 representation resolve to the exact same `drink_barcodes.barcode` row
- **AND** manual digit entry is always visible and automatically prompted after 8 seconds without a decode

### Requirement: Barcode Resolution Ladder and Open Food Facts ODbL Isolation
`GET /api/v1/barcodes/{gtin}` SHALL resolve against local `drink_barcodes` first, then Open Food Facts v3.6 (`GET /api/v3.6/product/{barcode}.json`) when enabled, and finally offer the prefilled unknown-soda creation flow.

#### Scenario: Rate-limited Open Food Facts lookup and ODbL isolation
- **WHEN** a GTIN is not in `drink_barcodes` and `OFF_ENABLED=true`
- **THEN** the OFF client sends a custom `User-Agent` (`Sodapop/<version> (<contact>)`), enforces `>= 1 req/s` spacing and a hard `15 req/min` ceiling, and stores the raw response in `off_products` (`license='ODbL-1.0'`)
- **AND** OFF fields are never silently blended into user-edited columns without attribution

#### Scenario: Unknown barcode provisional creation and soft-key dedupe
- **WHEN** a verified user submits the unknown-soda form for a new GTIN
- **THEN** if a drink with matching normalized `brand + name + volume_ml + packaging` exists, the UI prompts "Is this the same soda?" before creating a new row
- **AND** attaching the barcode or creating a provisional drink (`verified=0`, `source='user'`) immediately opens the check-in sheet
