package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"sodapop/ent"
	"sodapop/ent/aidraftsession"
	"sodapop/internal/catalog"
	"sodapop/internal/db"
)

const (
	// DraftExpiry is how long a conversational draft session remains usable.
	DraftExpiry = 7 * 24 * time.Hour
	// MaxDraftMessageRunes bounds one user-supplied turn.
	MaxDraftMessageRunes = 2000
	// MaxDraftNotesRunes bounds the initial notes accepted by CreateDraftSession.
	MaxDraftNotesRunes = 4000
	// MaxDraftTurns is the number of user/assistant turns retained on a session.
	MaxDraftTurns = 20

	maxMessagesJSONBytes = 512 << 10
	maxDraftJSONBytes    = 64 << 10
	draftMetaKey         = "_meta"
)

var (
	// ErrDraftNotFound hides both missing sessions and sessions owned by
	// somebody else, so the API can return 404 without enabling enumeration.
	ErrDraftNotFound = errors.New("ai draft session not found")
	// ErrDraftCommitted means the session can no longer accept turns.
	ErrDraftCommitted = errors.New("ai draft session already committed")
	// ErrDraftIncomplete means the saved draft lacks the fields needed to
	// create a drink.
	ErrDraftIncomplete = errors.New("ai draft is not ready to commit")
	// ErrDraftTooLarge means a stored or supplied turn exceeded its limit.
	ErrDraftTooLarge = errors.New("ai draft message is too large")
	// ErrDraftTruncated preserves the existing ai_output_truncated wording
	// while giving draft callers a descriptive name.
	ErrDraftTruncated = ErrTruncated
	// ErrDraftStorage means the AI service was built without an Ent client.
	ErrDraftStorage = errors.New("ai draft storage is not configured")
)

// DraftMessage is one persisted conversational turn.
type DraftMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// DraftTurn is the API-facing result of creating or advancing a draft.
type DraftTurn struct {
	SessionID         string         `json:"session_id"`
	Status            string         `json:"status"`
	Message           string         `json:"message"`
	Draft             map[string]any `json:"draft"`
	UpstreamSessionID string         `json:"upstream_session_id,omitempty"`
	CreatedAt         string         `json:"created_at"`
}

// draftMeta is stored alongside the drink draft in draft_json. The schema is
// deliberately unchanged: endpoint/session cursors live here, not in columns.
type draftMeta struct {
	UpstreamSessionID string `json:"upstream_session_id,omitempty"`
	EndpointID        int    `json:"endpoint_id,omitempty"`
	Barcode           string `json:"barcode,omitempty"`
	CommittedDrinkID  int    `json:"committed_drink_id,omitempty"`
}

type draftState struct {
	Draft map[string]any `json:"draft"`
	Meta  draftMeta      `json:"meta"`
}

// commitLocks serialises concurrent commits for one session inside this
// process. Entries are reference-counted and removed when the last waiter
// unlocks, so the map is bounded by in-flight commits rather than by every
// session ever committed. SQLite plus the committed drink id keep the
// operation idempotent.
var commitLocks = &keyedLocks{locks: map[string]*keyedLockEntry{}}

// CreateDraftSession performs one drafting turn, then persists the resulting
// conversation, draft and upstream session cursor on AIDraftSession.
func (s *Service) CreateDraftSession(ctx context.Context, userID, kind, initialNotes, barcode string) (*ent.AIDraftSession, error) {
	if s == nil || s.Client == nil {
		return nil, ErrDraftStorage
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrInvalidInput
	}
	initialNotes = strings.TrimSpace(initialNotes)
	barcode = strings.TrimSpace(barcode)
	if initialNotes == "" && barcode == "" {
		return nil, ErrNothingToSend
	}
	if len([]rune(initialNotes)) > MaxDraftNotesRunes {
		return nil, fmt.Errorf("%w: notes are limited to %d characters", ErrInvalidInput, MaxDraftNotesRunes)
	}

	kindValue, err := draftKind(kind)
	if err != nil {
		return nil, err
	}

	draft, reply, upstream, err := s.DraftDrink(ctx, initialNotes, barcode, "")
	if err != nil {
		return nil, err
	}
	endpointID := 0
	if ep, lookupErr := s.resolveEndpoint(ctx); lookupErr == nil && ep != nil {
		endpointID = ep.ID
	}

	now := db.Now()
	prompt := draftPrompt(initialNotes, barcode)
	messages := []DraftMessage{
		{Role: "user", Content: prompt, CreatedAt: now},
		{Role: "assistant", Content: reply, CreatedAt: now},
	}
	messagesJSON, err := encodeDraftMessages(messages)
	if err != nil {
		return nil, err
	}
	draftJSON, err := encodeDraftState(draft, draftMeta{UpstreamSessionID: upstream, EndpointID: endpointID, Barcode: barcode})
	if err != nil {
		return nil, err
	}

	row, err := s.Client.AIDraftSession.Create().
		SetUserID(userID).
		SetKind(kindValue).
		SetMessagesJSON(messagesJSON).
		SetDraftJSON(draftJSON).
		SetStatus(aidraftsession.StatusChatting).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// AppendDraftMessage adds one user turn and the assistant's reply. The write
// happens only after the provider returns a usable draft, so truncation can
// never leave a partial assistant turn or drink behind.
func (s *Service) AppendDraftMessage(ctx context.Context, userID, sessionID, content string) (*DraftTurn, error) {
	return s.advanceDraft(ctx, userID, sessionID, strings.TrimSpace(content))
}

// ReviseDraft rewrites the current draft in place, reusing the same session.
func (s *Service) ReviseDraft(ctx context.Context, userID, sessionID, instruction string) (*DraftTurn, error) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return nil, fmt.Errorf("%w: an instruction is required", ErrInvalidInput)
	}
	if len([]rune(instruction)) > MaxDraftMessageRunes {
		return nil, fmt.Errorf("%w: instructions are limited to %d characters", ErrDraftTooLarge, MaxDraftMessageRunes)
	}
	return s.advanceDraft(ctx, userID, sessionID, instruction)
}

func (s *Service) advanceDraft(ctx context.Context, userID, sessionID, content string) (*DraftTurn, error) {
	if s == nil || s.Client == nil {
		return nil, ErrDraftStorage
	}
	if content == "" {
		return nil, fmt.Errorf("%w: a message is required", ErrInvalidInput)
	}
	if len([]rune(content)) > MaxDraftMessageRunes {
		return nil, fmt.Errorf("%w: messages are limited to %d characters", ErrDraftTooLarge, MaxDraftMessageRunes)
	}

	row, err := s.LoadDraft(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if row.Status == aidraftsession.StatusCommitted {
		return nil, ErrDraftCommitted
	}

	messages, err := decodeDraftMessages(row.MessagesJSON)
	if err != nil {
		return nil, err
	}
	currentDraft, meta, err := decodeDraftState(row.DraftJSON)
	if err != nil {
		return nil, err
	}

	now := db.Now()
	history := append(messages, DraftMessage{Role: "user", Content: content, CreatedAt: now})
	history = capDraftMessages(history, MaxDraftTurns-1)

	draft, reply, upstream, err := s.draftWithHistory(ctx, history, currentDraft, meta.UpstreamSessionID)
	if err != nil {
		if errors.Is(err, ErrTruncated) {
			return nil, ErrDraftTruncated
		}
		return nil, err
	}

	history = capDraftMessages(append(history, DraftMessage{Role: "assistant", Content: reply, CreatedAt: now}), MaxDraftTurns)
	messagesJSON, err := encodeDraftMessages(history)
	if err != nil {
		return nil, err
	}
	if upstream != "" {
		meta.UpstreamSessionID = upstream
	}
	draftJSON, err := encodeDraftState(draft, meta)
	if err != nil {
		return nil, err
	}

	status := aidraftsession.StatusReady
	if strings.TrimSpace(stringValue(draft["name"])) == "" {
		status = aidraftsession.StatusChatting
	}
	updated, err := s.Client.AIDraftSession.UpdateOneID(row.ID).
		SetMessagesJSON(messagesJSON).
		SetDraftJSON(draftJSON).
		SetStatus(status).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return draftTurn(updated, reply), nil
}

// CommitDraft creates exactly one provisional drink from the saved draft. A
// repeated call returns the drink recorded in draft_json without touching the
// catalog again.
func (s *Service) CommitDraft(ctx context.Context, userID, sessionID string) (*ent.Drink, error) {
	if s == nil || s.Client == nil {
		return nil, ErrDraftStorage
	}
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrNotEnabled
	}

	unlock := commitLocks.lock(sessionID)
	defer unlock()

	row, err := s.LoadDraft(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	draft, meta, err := decodeDraftState(row.DraftJSON)
	if err != nil {
		return nil, err
	}
	if meta.CommittedDrinkID > 0 {
		drink, err := s.Client.Drink.Get(ctx, meta.CommittedDrinkID)
		if err == nil {
			return drink, nil
		}
		if !ent.IsNotFound(err) {
			return nil, err
		}
		// A previously committed drink was removed; fall through and recreate
		// from the still-valid draft.
	}
	if row.Status == aidraftsession.StatusCommitted && meta.CommittedDrinkID == 0 {
		return nil, ErrDraftIncomplete
	}

	name := strings.TrimSpace(stringValue(draft["name"]))
	if name == "" {
		return nil, fmt.Errorf("%w: the draft needs a name", ErrDraftIncomplete)
	}
	if s.Catalog == nil {
		return nil, ErrDraftStorage
	}

	in := catalog.CreateInput{
		Name:        name,
		Brand:       strings.TrimSpace(stringValue(draft["brand"])),
		StyleSlug:   firstNonEmpty(stringValue(draft["style_slug"]), stringValue(draft["style"])),
		Container:   strings.TrimSpace(stringValue(draft["container"])),
		VolumeML:    int(draftNumber(draft["volume_ml"])),
		Sweetener:   strings.TrimSpace(stringValue(draft["sweetener"])),
		Carbonation: strings.TrimSpace(stringValue(draft["carbonation"])),
		Acid:        strings.TrimSpace(stringValue(draft["acid"])),
		Barcode:     meta.Barcode,
		Description: strings.TrimSpace(stringValue(draft["description"])),
	}
	drink, _, err := s.Catalog.CreateProvisional(ctx, userID, in)
	if err != nil {
		return nil, err
	}

	meta.CommittedDrinkID = drink.ID
	draftJSON, err := encodeDraftState(draft, meta)
	if err != nil {
		return nil, err
	}
	if _, err := s.Client.AIDraftSession.UpdateOneID(row.ID).
		SetDraftJSON(draftJSON).
		SetStatus(aidraftsession.StatusCommitted).
		SetUpdatedAt(db.Now()).
		Save(ctx); err != nil {
		return nil, err
	}
	return drink, nil
}

// LoadDraft fetches an unexpired session owned by userID. Unknown sessions,
// sessions owned by another user and expired sessions all return
// ErrDraftNotFound so callers cannot distinguish them.
func (s *Service) LoadDraft(ctx context.Context, userID, sessionID string) (*ent.AIDraftSession, error) {
	if s == nil || s.Client == nil {
		return nil, ErrDraftStorage
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return nil, ErrDraftNotFound
	}
	row, err := s.Client.AIDraftSession.Query().
		Where(aidraftsession.IDEQ(sessionID), aidraftsession.UserIDEQ(userID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrDraftNotFound
		}
		return nil, err
	}
	if draftExpired(row) {
		return nil, ErrDraftNotFound
	}
	return row, nil
}

// ExpireDrafts removes sessions older than DraftExpiry. The returned count is
// useful for logs and tests; no user content is exposed.
func (s *Service) ExpireDrafts(ctx context.Context) (int, error) {
	if s == nil || s.Client == nil {
		return 0, ErrDraftStorage
	}
	cutoff := time.Now().UTC().Add(-DraftExpiry).Format(time.RFC3339)
	return s.Client.AIDraftSession.Delete().
		Where(aidraftsession.CreatedAtLT(cutoff)).
		Exec(ctx)
}

// DraftTurnFromSession builds the API response for a freshly written session.
func DraftTurnFromSession(row *ent.AIDraftSession) (*DraftTurn, error) {
	if row == nil {
		return nil, ErrDraftNotFound
	}
	messages, err := decodeDraftMessages(row.MessagesJSON)
	if err != nil {
		return nil, err
	}
	draft, meta, err := decodeDraftState(row.DraftJSON)
	if err != nil {
		return nil, err
	}
	reply := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			reply = messages[i].Content
			break
		}
	}
	turn := draftTurn(row, reply)
	turn.Draft = draft
	turn.UpstreamSessionID = meta.UpstreamSessionID
	return turn, nil
}

func draftTurn(row *ent.AIDraftSession, reply string) *DraftTurn {
	draft, meta, _ := decodeDraftState(row.DraftJSON)
	return &DraftTurn{
		SessionID:         row.ID,
		Status:            string(row.Status),
		Message:           reply,
		Draft:             draft,
		UpstreamSessionID: meta.UpstreamSessionID,
		CreatedAt:         row.CreatedAt,
	}
}

func (s *Service) draftWithHistory(ctx context.Context, history []DraftMessage, current map[string]any, upstream string) (map[string]any, string, string, error) {
	ctx, cancel := withDefaultTimeout(ctx, DraftTimeout)
	defer cancel()

	messages := []ChatMessage{{Role: "system", Content: draftSystemPrompt}}
	if len(current) > 0 {
		if encoded, err := json.Marshal(current); err == nil {
			messages = append(messages, ChatMessage{
				Role:    "system",
				Content: "Current draft JSON:\n" + string(encoded),
			})
		}
	}
	for _, turn := range history {
		role := strings.ToLower(strings.TrimSpace(turn.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		messages = append(messages, ChatMessage{Role: role, Content: turn.Content})
	}

	resp, err := s.Chat(ctx, ChatRequest{
		Messages:    messages,
		Temperature: 0.4,
		SessionID:   upstream,
		JSON:        true,
	})
	if err != nil {
		return nil, "", "", err
	}
	draft, reply, err := parseDraftContent(resp.Content)
	if err != nil {
		return nil, "", resp.SessionID, err
	}
	return draft, reply, resp.SessionID, nil
}

// parseDraftContent mirrors DraftDrink's parsing and normalisation so
// multi-turn replies have exactly the same shape as first-turn drafts.
func parseDraftContent(content string) (map[string]any, string, error) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stripJSONFences(content)), &parsed); err != nil {
		return nil, "", fmt.Errorf("%w: draft was not valid JSON", ErrInvalidOutput)
	}
	draft := parsed
	if inner, ok := parsed["draft"].(map[string]any); ok {
		draft = inner
	}
	reply := ""
	for _, key := range []string{"reply", "message", "assistant_message"} {
		if value, ok := parsed[key].(string); ok && strings.TrimSpace(value) != "" {
			reply = sanitizeProse(value)
			break
		}
	}
	for _, key := range []string{"reply", "message", "assistant_message", "session_id", "status", "draft"} {
		delete(draft, key)
	}
	normalizeDraft(draft)
	if reply == "" {
		reply = draftSummary(draft)
	}
	return draft, reply, nil
}

func encodeDraftMessages(messages []DraftMessage) (string, error) {
	encoded, err := json.Marshal(messages)
	if err != nil {
		return "", fmt.Errorf("encode draft messages: %w", err)
	}
	if len(encoded) > maxMessagesJSONBytes {
		return "", ErrDraftTooLarge
	}
	return string(encoded), nil
}

func decodeDraftMessages(raw string) ([]DraftMessage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []DraftMessage{}, nil
	}
	if len(raw) > maxMessagesJSONBytes {
		return nil, ErrDraftTooLarge
	}
	var messages []DraftMessage
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, fmt.Errorf("%w: stored messages are invalid", ErrInvalidOutput)
	}
	out := make([]DraftMessage, 0, len(messages))
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		out = append(out, DraftMessage{Role: role, Content: content, CreatedAt: message.CreatedAt})
	}
	return out, nil
}

func encodeDraftState(draft map[string]any, meta draftMeta) (string, error) {
	payload := make(map[string]any, len(draft)+1)
	for key, value := range draft {
		if key == draftMetaKey {
			continue
		}
		payload[key] = value
	}
	// The draft fields stay top-level for consumers that read draft_json
	// directly; only the internal cursor lives under a reserved key.
	payload[draftMetaKey] = meta
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode draft: %w", err)
	}
	if len(encoded) > maxDraftJSONBytes {
		return "", ErrDraftTooLarge
	}
	return string(encoded), nil
}

// decodeDraftState accepts the top-level draft written by this file, the
// wrapper used briefly during development, and a bare legacy draft object.
func decodeDraftState(raw string) (map[string]any, draftMeta, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return map[string]any{}, draftMeta{}, nil
	}
	if len(raw) > maxDraftJSONBytes {
		return nil, draftMeta{}, ErrDraftTooLarge
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, draftMeta{}, fmt.Errorf("%w: stored draft is invalid", ErrInvalidOutput)
	}
	if _, ok := payload["draft"].(map[string]any); ok {
		var wrapper draftState
		if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
			return nil, draftMeta{}, fmt.Errorf("%w: stored draft is invalid", ErrInvalidOutput)
		}
		if wrapper.Draft == nil {
			wrapper.Draft = map[string]any{}
		}
		return wrapper.Draft, wrapper.Meta, nil
	}
	var meta draftMeta
	if rawMeta, ok := payload[draftMetaKey]; ok {
		encoded, err := json.Marshal(rawMeta)
		if err != nil {
			return nil, draftMeta{}, fmt.Errorf("%w: draft metadata is invalid", ErrInvalidOutput)
		}
		if err := json.Unmarshal(encoded, &meta); err != nil {
			return nil, draftMeta{}, fmt.Errorf("%w: draft metadata is invalid", ErrInvalidOutput)
		}
		delete(payload, draftMetaKey)
	}
	return payload, meta, nil
}

func capDraftMessages(messages []DraftMessage, max int) []DraftMessage {
	if max <= 0 || len(messages) <= max {
		return messages
	}
	start := len(messages) - max
	for start < len(messages) && messages[start].Role != "user" {
		start++
	}
	if start >= len(messages) {
		return messages[len(messages)-max:]
	}
	return messages[start:]
}

func draftExpired(row *ent.AIDraftSession) bool {
	if row == nil {
		return true
	}
	stamp := strings.TrimSpace(row.CreatedAt)
	if stamp == "" {
		stamp = strings.TrimSpace(row.UpdatedAt)
	}
	created, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		// Fail closed only when the row has no usable timestamp at all.
		return stamp == ""
	}
	return time.Since(created) > DraftExpiry
}

func draftKind(kind string) (aidraftsession.Kind, error) {
	value := aidraftsession.Kind(strings.ToLower(strings.TrimSpace(kind)))
	if value == "" {
		return aidraftsession.KindDrink, nil
	}
	if err := aidraftsession.KindValidator(value); err != nil {
		return "", fmt.Errorf("%w: kind must be drink, tasting_note or venue", ErrInvalidInput)
	}
	return value, nil
}

func draftPrompt(notes, barcode string) string {
	var prompt strings.Builder
	if barcode != "" {
		fmt.Fprintf(&prompt, "Barcode: %s\n", barcode)
	}
	if notes != "" {
		fmt.Fprintf(&prompt, "Rough notes or label text:\n%s\n", notes)
	}
	fmt.Fprint(&prompt, "\nReturn the draft JSON object.")
	return prompt.String()
}

// keyedLocks is a bounded mutex map: each entry disappears once no goroutine
// holds or awaits it. Lock/unlock must be paired.
type keyedLocks struct {
	mu    sync.Mutex
	locks map[string]*keyedLockEntry
}

type keyedLockEntry struct {
	mu   sync.Mutex
	refs int
}

func (k *keyedLocks) lock(key string) func() {
	k.mu.Lock()
	entry := k.locks[key]
	if entry == nil {
		entry = &keyedLockEntry{}
		k.locks[key] = entry
	}
	entry.refs++
	k.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		k.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}

// len reports the number of tracked sessions. Tests use it to prove eviction.
func (k *keyedLocks) len() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.locks)
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
