# OpenCode ("opencode2") AI Subsystem Specification

## Purpose

Defines Sodapop's OpenCode ("opencode2") AI subsystem (`internal/ai`), ported from `dnd/ai`, providing admin-managed OpenAI-compatible endpoints (first-class for OpenCode Zen `https://opencode.ai/zen/go/v1`), AES-GCM encrypted API keys, URL normalization, honest `sodapop/<version>` and `x-opencode-session` attribution, conversational/vision drink drafting, check-in tasting-note polish, and Soda Sommelier recommendations.

## Requirements

### Requirement: Admin AI Endpoint Management and AES-GCM Key Encryption
The system SHALL store AI configuration in Ent entities `AppSetting` (`ai_enabled`) and `AIEndpoint` (`name`, `type` in `text|image|vision`, `base_url`, `encrypted_api_key`, `model`, `tags`, `enabled`, `temperature`, `max_tokens`, `image_size`).

#### Scenario: API key is encrypted at rest and masked in responses
- **WHEN** an admin creates or updates an `AIEndpoint` via `/api/v1/admin/ai-endpoints`
- **THEN** the plaintext API key is encrypted with AES-GCM using a 32-byte key derived from `SESSION_SECRET` (`SHA-256("sodapop-ai-key:" + SESSION_SECRET)`) before persisting to `encrypted_api_key`
- **AND** all GET/POST/PUT responses return only a masked representation (`sk-...xxxx`)

#### Scenario: Base URL normalization prevents duplicate operation paths
- **WHEN** an endpoint is saved or an outbound AI request URL is constructed via `NormalizeAIBaseURL` and `AIRequestURL`
- **THEN** trailing slashes and accidental suffixes (`/chat/completions`, `/images/generations`, `/completions`) are stripped so OpenCode Zen URLs like `https://opencode.ai/zen/go/v1` never produce `/chat/completions/chat/completions`
- **AND** non-`http(s)` URLs are rejected with HTTP `400`

### Requirement: Honest Provider Attribution and Session Continuity
Every outbound HTTP request to an AI provider SHALL identify Sodapop honestly and preserve session continuity across multi-turn workflows.

#### Scenario: Outbound headers on every AI call
- **WHEN** `internal/ai` sends an HTTP request for endpoint testing, drink drafting, label vision extraction, tasting-note polish, or recommendations
- **THEN** `SetAIProviderHeaders` sets `User-Agent: sodapop/<version>` (fallback `0.0.0-dev`)
- **AND** sets `x-opencode-session: <session_id>`, reusing the caller/draft-session ID or generating a 16-byte random hex ID on turn 1 and returning `session_id` in the response payload

### Requirement: Timeouts, Token Budgets, and Truncation Handling
The system SHALL enforce operation-specific timeouts (`TestTimeout=30s`, `TextTimeout=60s`, `ImageTimeout=120s`, `DraftTimeout=180s`) and guard against truncated upstream completions.

#### Scenario: Upstream error or truncated completion
- **WHEN** `POST /api/v1/admin/ai-endpoints/{id}/test` fails upstream
- **THEN** the response includes the upstream HTTP status code and up to 200 characters of the upstream response body
- **AND WHEN** a drafting or text completion returns empty content or `finish_reason == "length"` before valid structured output is complete
- **THEN** the service returns an actionable `ai_output_truncated` error and does not persist a broken assistant turn in `ai_draft_sessions`

### Requirement: Sodapop AI Capabilities (Drink Drafting, Tasting-Note Polish, Sommelier)
When `ai_enabled=1` and a matching endpoint is enabled, the system SHALL expose conversational/vision drink drafting, check-in tasting-note polish, and catalog recommendations.

#### Scenario: Multi-turn drink drafting from rough notes or barcode stub
- **WHEN** a verified user posts to `POST /api/v1/ai/drafts` or `/api/v1/ai/drafts/{id}/messages` with rough soda notes, label text, or an OFF stub
- **THEN** the assistant responds with `{"status": "chatting"|"ready", "message": "...", "draft": {"brand": "...", "name": "...", "style_slug": "...", "flavour_tags": [...], "sweetener": "...", "acid": "...", "carbonation": "...", "sugar_g_per_100ml": ...}, "session_id": "..."}`
- **AND** `POST /api/v1/ai/drafts/{id}/commit` creates or updates the catalog drink inside an Ent transaction

#### Scenario: Tasting-note polish respects Sodapop voice rules
- **WHEN** a user invokes `POST /api/v1/ai/tasting-note` with selected flavour chips, rating, and rough notes
- **THEN** the assistant returns a 1–2 sentence draft note in sentence case with no exclamation marks and no nutritional shaming
