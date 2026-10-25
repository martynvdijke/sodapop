package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// TastingNote rewrites rough impressions as a short, unpretentious note. It
// enforces the voice rules even if the provider ignores them.
func (s *Service) TastingNote(ctx context.Context, rough string) (string, error) {
	rough = strings.TrimSpace(rough)
	if rough == "" {
		return "", ErrNothingToSend
	}
	ctx, cancel := withDefaultTimeout(ctx, TextTimeout)
	defer cancel()

	resp, err := s.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: tastingNoteSystemPrompt},
			{Role: "user", Content: rough},
		},
		MaxTokens:   300,
		Temperature: 0.7,
	})
	if err != nil {
		return "", err
	}
	note := sanitizeProse(resp.Content)
	note = limitSentences(note, 3)
	note = truncateRunes(note, 280)
	if note == "" {
		return "", ErrTruncated
	}
	return note, nil
}

// DraftDrink turns rough notes and/or a barcode into a structured draft. The
// returned session ID is reused for follow-up turns.
func (s *Service) DraftDrink(ctx context.Context, notes, barcode, sessionID string) (map[string]any, string, string, error) {
	notes = strings.TrimSpace(notes)
	barcode = strings.TrimSpace(barcode)
	if notes == "" && barcode == "" {
		return nil, "", "", ErrNothingToSend
	}

	var prompt strings.Builder
	if barcode != "" {
		fmt.Fprintf(&prompt, "Barcode: %s\n", barcode)
	}
	if notes != "" {
		fmt.Fprintf(&prompt, "Rough notes or label text:\n%s\n", notes)
	}
	fmt.Fprint(&prompt, "\nReturn the draft JSON object.")

	ctx, cancel := withDefaultTimeout(ctx, DraftTimeout)
	defer cancel()

	resp, err := s.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: draftSystemPrompt},
			{Role: "user", Content: prompt.String()},
		},
		MaxTokens:   0,
		Temperature: 0.4,
		SessionID:   sessionID,
		JSON:        true,
	})
	if err != nil {
		return nil, "", "", err
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(stripJSONFences(resp.Content)), &parsed); err != nil {
		return nil, "", resp.SessionID, fmt.Errorf("%w: draft was not valid JSON", ErrInvalidOutput)
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
	return draft, reply, resp.SessionID, nil
}

// Recommend returns a short Soda Sommelier suggestion from a history summary.
func (s *Service) Recommend(ctx context.Context, historySummary string) (string, error) {
	historySummary = strings.TrimSpace(historySummary)
	if historySummary == "" {
		historySummary = "No check-ins yet; suggest a good starting point for someone new to soda."
	}
	ctx, cancel := withDefaultTimeout(ctx, TextTimeout)
	defer cancel()

	resp, err := s.Chat(ctx, ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: recommendSystemPrompt},
			{Role: "user", Content: historySummary},
		},
		MaxTokens:   500,
		Temperature: 0.8,
	})
	if err != nil {
		return "", err
	}
	recommendation := truncateRunes(sanitizeProse(resp.Content), 800)
	if recommendation == "" {
		return "", ErrTruncated
	}
	return recommendation, nil
}

// sanitizeProse strips exclamation marks and collapses whitespace, enforcing
// Sodapop's voice rules regardless of what the provider returned.
func sanitizeProse(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	value = strings.ReplaceAll(value, "!", ".")
	value = strings.Join(strings.Fields(value), " ")
	for strings.Contains(value, "..") {
		value = strings.ReplaceAll(value, "..", ".")
	}
	return strings.TrimSpace(value)
}

// limitSentences keeps at most max sentence terminators.
func limitSentences(text string, max int) string {
	if max <= 0 {
		return text
	}
	count := 0
	for i, r := range text {
		if r != '.' && r != '?' {
			continue
		}
		count++
		if count >= max {
			return strings.TrimSpace(text[:i+1])
		}
	}
	return text
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(string(runes[:limit]))
}

// stripJSONFences removes markdown code fences and any chatter around the JSON
// object that some providers add despite JSON mode.
func stripJSONFences(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "\ufeff"))
	if strings.HasPrefix(value, "```") {
		if newline := strings.IndexByte(value, '\n'); newline >= 0 {
			value = value[newline+1:]
		} else {
			value = strings.TrimPrefix(value, "```")
		}
		value = strings.TrimSuffix(strings.TrimSpace(value), "```")
	}
	value = strings.TrimSpace(value)
	if start := strings.IndexByte(value, '{'); start > 0 {
		value = value[start:]
	}
	if end := strings.LastIndexByte(value, '}'); end >= 0 && end < len(value)-1 {
		value = value[:end+1]
	}
	return strings.TrimSpace(value)
}

func normalizeDraft(draft map[string]any) {
	for _, key := range []string{"name", "brand", "style", "container", "sweetener", "carbonation", "acid", "description"} {
		value, ok := draft[key].(string)
		if !ok {
			draft[key] = ""
			continue
		}
		draft[key] = sanitizeProse(value)
	}
	for _, key := range []string{"volume_ml", "caffeine_mg_per_100ml", "sugar_g_per_100ml"} {
		draft[key] = draftNumber(draft[key])
	}
	draft["flavours"] = normalizeFlavours(draft["flavours"])
}

func draftNumber(value any) float64 {
	switch n := value.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case json.Number:
		if parsed, err := n.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return parsed
		}
	}
	return 0
}

func normalizeFlavours(value any) []any {
	out := []any{}
	switch list := value.(type) {
	case []any:
		for _, item := range list {
			if flavour, ok := flavourEntry(item); ok {
				out = append(out, flavour)
			}
		}
	case []string:
		for _, name := range list {
			if flavour, ok := flavourEntry(name); ok {
				out = append(out, flavour)
			}
		}
	}
	return out
}

func flavourEntry(value any) (map[string]any, bool) {
	switch item := value.(type) {
	case string:
		name := sanitizeProse(item)
		if name == "" {
			return nil, false
		}
		return map[string]any{"name": name, "intensity": 3}, true
	case map[string]any:
		name, _ := item["name"].(string)
		name = sanitizeProse(name)
		if name == "" {
			return nil, false
		}
		intensity := int(draftNumber(item["intensity"]))
		if intensity <= 0 {
			intensity = 3
		}
		intensity = clamp(intensity, 1, 5)
		return map[string]any{"name": name, "intensity": intensity}, true
	default:
		return nil, false
	}
}

func draftSummary(draft map[string]any) string {
	brand, _ := draft["brand"].(string)
	name, _ := draft["name"].(string)
	switch {
	case brand != "" && name != "":
		return fmt.Sprintf("Drafted %s %s.", brand, name)
	case name != "":
		return fmt.Sprintf("Drafted %s.", name)
	case brand != "":
		return fmt.Sprintf("Drafted a drink from %s.", brand)
	default:
		return "Draft ready to review."
	}
}
