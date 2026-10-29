package integrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"sodapop/ent"
	"sodapop/ent/deliveryoutbox"
	"sodapop/ent/integration"
	"sodapop/ent/pushdevice"
	"sodapop/ent/pushsubscription"
	"sodapop/ent/user"
	"sodapop/ent/usersetting"
	"sodapop/internal/telemetry"
)

const (
	outboxBatchSize   = 20
	outboxMaxAttempts = 5
	outboxTick        = 30 * time.Second
	outboxMaxBackoff  = time.Hour
)

// Dispatch records an event in the durable outbox for every enabled
// integration and active push subscription owned by the event user. It only
// persists work; RunOutbox performs delivery. A missing target is not an
// error. The returned error is reserved for real marshal or database failures.
func (s *Service) Dispatch(ctx context.Context, e Event) error {
	if s == nil || s.Client == nil || e.UserID == "" {
		return nil
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	created := now()
	payloadJSON := string(payload)

	var builders []*ent.DeliveryOutboxCreate

	rows, err := s.Client.Integration.Query().
		Where(integration.UserID(e.UserID), integration.Enabled(true)).
		All(ctx)
	if err != nil {
		return fmt.Errorf("load integration targets: %w", err)
	}
	for _, row := range rows {
		kind, ok := outboxKind(row.Kind)
		if !ok {
			continue
		}
		builders = append(builders, s.Client.DeliveryOutbox.Create().
			SetUserID(e.UserID).
			SetKind(kind).
			SetTarget(strconv.Itoa(row.ID)).
			SetPayloadJSON(payloadJSON).
			SetAttempts(0).
			SetNextAttemptAt(created).
			SetLastError("").
			SetCreatedAt(created))
	}

	settings, err := s.Client.UserSetting.Query().
		Where(usersetting.UserID(e.UserID)).
		Only(ctx)
	switch {
	case err == nil && settings.PushEnabled:
		subs, subErr := s.Client.PushSubscription.Query().
			Where(pushsubscription.UserID(e.UserID)).
			All(ctx)
		if subErr != nil {
			return fmt.Errorf("load push subscriptions: %w", subErr)
		}
		for _, sub := range subs {
			builders = append(builders, s.Client.DeliveryOutbox.Create().
				SetUserID(e.UserID).
				SetKind(deliveryoutbox.KindPush).
				SetTarget(strconv.Itoa(sub.ID)).
				SetPayloadJSON(payloadJSON).
				SetAttempts(0).
				SetNextAttemptAt(created).
				SetLastError("").
				SetCreatedAt(created))
		}

		if s.fcmEnabled() {
			devices, devErr := s.Client.PushDevice.Query().
				Where(pushdevice.UserID(e.UserID)).
				All(ctx)
			if devErr != nil {
				return fmt.Errorf("load push devices: %w", devErr)
			}
			for _, device := range devices {
				builders = append(builders, s.Client.DeliveryOutbox.Create().
					SetUserID(e.UserID).
					SetKind(deliveryoutbox.KindFcm).
					SetTarget(device.ID).
					SetPayloadJSON(payloadJSON).
					SetAttempts(0).
					SetNextAttemptAt(created).
					SetLastError("").
					SetCreatedAt(created))
			}
		}
	case err != nil && !ent.IsNotFound(err):
		return fmt.Errorf("load user settings: %w", err)
	}

	if len(builders) == 0 {
		return nil
	}
	if _, err := s.Client.DeliveryOutbox.CreateBulk(builders...).Save(ctx); err != nil {
		return fmt.Errorf("enqueue outbox rows: %w", err)
	}
	return nil
}

// RunOutbox polls the durable delivery queue until the context is cancelled.
// It processes an initial batch immediately, then once per outboxTick.
func (s *Service) RunOutbox(ctx context.Context) {
	if s == nil || s.Client == nil {
		return
	}
	ticker := time.NewTicker(outboxTick)
	defer ticker.Stop()
	for {
		s.processOutbox(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// OutboxDepth reports the number of undelivered rows, including rows that
// exhausted their retries.
func (s *Service) OutboxDepth(ctx context.Context) (int, error) {
	if s == nil || s.Client == nil {
		return 0, nil
	}
	return s.Client.DeliveryOutbox.Query().
		Where(deliveryoutbox.DeliveredAtIsNil()).
		Count(ctx)
}

func (s *Service) processOutbox(ctx context.Context) {
	if s == nil || s.Client == nil || ctx.Err() != nil {
		return
	}
	rows, err := s.Client.DeliveryOutbox.Query().
		Where(
			deliveryoutbox.DeliveredAtIsNil(),
			deliveryoutbox.NextAttemptAtLTE(now()),
			deliveryoutbox.AttemptsLT(outboxMaxAttempts),
		).
		Order(
			ent.Asc(deliveryoutbox.FieldNextAttemptAt),
			ent.Asc(deliveryoutbox.FieldID),
		).
		Limit(outboxBatchSize).
		All(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger().Warn("outbox load failed", "err", err)
		}
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		s.deliverOutboxRow(ctx, row)
	}
}

func (s *Service) deliverOutboxRow(ctx context.Context, row *ent.DeliveryOutbox) {
	timeout := 5 * time.Second
	if row.Kind == deliveryoutbox.KindTrmnl {
		timeout = 12 * time.Second
	}
	sendCtx, cancel := context.WithTimeout(ctx, timeout)
	permanent, sendErr := s.sendOutbox(sendCtx, row)
	cancel()
	telemetry.RecordIntegration(string(row.Kind), sendErr)

	// A cancelled context means shutdown, not a delivery verdict. Leave the
	// row untouched so it is retried by the next process.
	if ctx.Err() != nil {
		return
	}
	if sendErr == nil {
		if _, err := s.Client.DeliveryOutbox.UpdateOneID(row.ID).
			SetDeliveredAt(now()).
			SetLastError("").
			Save(ctx); err != nil && ctx.Err() == nil {
			s.logger().Warn("outbox mark delivered failed", "kind", row.Kind, "target", row.Target, "err", err)
		}
		return
	}

	attempts := row.Attempts + 1
	nextAttempt := now()
	if permanent || attempts >= outboxMaxAttempts {
		attempts = outboxMaxAttempts
	} else {
		nextAttempt = time.Now().UTC().Add(outboxBackoff(attempts)).Format(time.RFC3339)
	}
	msg := truncateError(sendErr.Error())
	if _, err := s.Client.DeliveryOutbox.UpdateOneID(row.ID).
		SetAttempts(attempts).
		SetNextAttemptAt(nextAttempt).
		SetLastError(msg).
		Save(ctx); err != nil && ctx.Err() == nil {
		s.logger().Warn("outbox record failure failed", "kind", row.Kind, "target", row.Target, "err", err)
	}
	if !permanent && attempts < outboxMaxAttempts {
		s.logger().Warn("outbox delivery failed",
			"kind", row.Kind, "target", row.Target, "attempt", attempts, "err", msg)
		return
	}
	s.logger().Warn("outbox delivery abandoned",
		"kind", row.Kind, "target", row.Target, "attempt", attempts, "err", msg)
}

// sendOutbox loads the fresh target at delivery time, then delegates to the
// existing senders. Secrets live only on the integration row and are never
// read from the queued payload. The bool reports a permanent failure.
func (s *Service) sendOutbox(ctx context.Context, row *ent.DeliveryOutbox) (bool, error) {
	event, err := decodeOutboxEvent(row.PayloadJSON)
	if err != nil {
		return true, err
	}
	switch row.Kind {
	case deliveryoutbox.KindWebhook, deliveryoutbox.KindMatrix, deliveryoutbox.KindTrmnl:
		id, err := strconv.Atoi(row.Target)
		if err != nil {
			return true, errMissingTarget()
		}
		target, err := s.Client.Integration.Get(ctx, id)
		if ent.IsNotFound(err) {
			return true, errMissingTarget()
		}
		if err != nil {
			return false, err
		}
		if target.UserID != row.UserID || !target.Enabled {
			return true, errMissingTarget()
		}
		switch row.Kind {
		case deliveryoutbox.KindWebhook:
			return false, s.sendHomeAssistant(ctx, target, event)
		case deliveryoutbox.KindMatrix:
			return false, s.sendMatrix(ctx, target, event)
		default:
			return false, s.sendTRMNL(ctx, target)
		}
	case deliveryoutbox.KindPush:
		id, err := strconv.Atoi(row.Target)
		if err != nil {
			return true, errMissingTarget()
		}
		sub, err := s.Client.PushSubscription.Get(ctx, id)
		if ent.IsNotFound(err) {
			return true, errMissingTarget()
		}
		if err != nil {
			return false, err
		}
		if sub.UserID != row.UserID {
			return true, errMissingTarget()
		}
		return false, s.sendPushToSubscriptions(
			ctx, []*ent.PushSubscription{sub}, eventTitle(event.Kind), event.Text, event.URL)
	case deliveryoutbox.KindFcm:
		device, err := s.Client.PushDevice.Get(ctx, row.Target)
		if ent.IsNotFound(err) {
			// The device was unregistered after the event was queued.
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if device.UserID != row.UserID {
			return true, errMissingTarget()
		}
		pruned, err := s.SendFCM(ctx, device.Token, eventTitle(event.Kind), event.Text, event.URL)
		if err != nil {
			return false, err
		}
		if pruned {
			if _, err := s.Client.PushDevice.Delete().
				Where(pushdevice.ID(device.ID), pushdevice.UserID(row.UserID)).
				Exec(ctx); err != nil {
				return false, err
			}
		}
		return false, nil
	default:
		return true, errMissingTarget()
	}
}

func outboxKind(kind integration.Kind) (deliveryoutbox.Kind, bool) {
	switch kind {
	case integration.KindHomeAssistant:
		return deliveryoutbox.KindWebhook, true
	case integration.KindMatrix:
		return deliveryoutbox.KindMatrix, true
	case integration.KindTrmnl:
		return deliveryoutbox.KindTrmnl, true
	default:
		return "", false
	}
}

func decodeOutboxEvent(payload string) (Event, error) {
	var event Event
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return Event{}, fmt.Errorf("decode outbox payload: %w", err)
	}
	return event, nil
}

func outboxBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Duration(1<<uint(attempts)) * time.Minute
	if delay > outboxMaxBackoff {
		delay = outboxMaxBackoff
	}
	return delay
}

func errMissingTarget() error {
	return fmt.Errorf("delivery target is not configured")
}

func truncateError(msg string) string {
	const max = 500
	if len(msg) <= max {
		return msg
	}
	return msg[:max]
}

// Delivery statuses shown in the admin delivery history.
const (
	DeliveryStatusDelivered = "delivered"
	DeliveryStatusPending   = "pending"
	DeliveryStatusRetrying  = "retrying"
	DeliveryStatusAbandoned = "abandoned"
)

// DeliveryRecord is one outbox row prepared for the admin history page. Text
// and URL come from the queued event payload; they stay empty when the payload
// cannot be decoded. UserHandle stays empty when the user no longer exists.
type DeliveryRecord struct {
	ID            int
	UserID        string
	UserHandle    string
	Kind          string
	Target        string
	Text          string
	URL           string
	LastError     string
	NextAttemptAt string
	CreatedAt     string
	DeliveredAt   *string
	Attempts      int
	Status        string
}

// deliveryStatusWhere maps a status filter to a parameter-free SQL predicate.
// An empty or unknown status matches every row.
func deliveryStatusWhere(status string) string {
	switch status {
	case DeliveryStatusDelivered:
		return "delivered_at IS NOT NULL"
	case DeliveryStatusPending:
		return "delivered_at IS NULL AND attempts = 0"
	case DeliveryStatusRetrying:
		return "delivered_at IS NULL AND attempts > 0 AND attempts < " + strconv.Itoa(outboxMaxAttempts)
	case DeliveryStatusAbandoned:
		return "delivered_at IS NULL AND attempts >= " + strconv.Itoa(outboxMaxAttempts)
	default:
		return "1 = 1"
	}
}

// classifyDelivery returns the status label for a row's delivery state.
func classifyDelivery(deliveredAt *string, attempts int) string {
	if deliveredAt != nil {
		return DeliveryStatusDelivered
	}
	switch {
	case attempts <= 0:
		return DeliveryStatusPending
	case attempts >= outboxMaxAttempts:
		return DeliveryStatusAbandoned
	default:
		return DeliveryStatusRetrying
	}
}

// ListDeliveries returns a page of delivery history, newest first, plus the
// total number of rows matching the same status filter. The limit defaults to
// 50 and is capped at 200. Only parameterized SQL is used; the status filter is
// mapped to a fixed predicate.
func (s *Service) ListDeliveries(ctx context.Context, status string, limit, offset int) ([]DeliveryRecord, int, error) {
	if s == nil || s.SQL == nil {
		return nil, 0, nil
	}
	if !isDeliveryStatus(status) {
		status = ""
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	where := deliveryStatusWhere(status)
	var total int
	if err := s.SQL.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM delivery_outbox WHERE `+where).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deliveries: %w", err)
	}

	rows, err := s.SQL.QueryContext(ctx,
		`SELECT id, user_id, kind, target, payload_json, attempts, next_attempt_at, last_error, delivered_at, created_at
		 FROM delivery_outbox WHERE `+where+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()

	records := []DeliveryRecord{}
	userIDs := map[string]struct{}{}
	index := map[string][]int{}
	for rows.Next() {
		var (
			rec         DeliveryRecord
			payload     string
			deliveredAt sql.NullString
		)
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.Kind, &rec.Target, &payload,
			&rec.Attempts, &rec.NextAttemptAt, &rec.LastError, &deliveredAt, &rec.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan delivery: %w", err)
		}
		if deliveredAt.Valid {
			value := deliveredAt.String
			rec.DeliveredAt = &value
		}
		if event, err := decodeOutboxEvent(payload); err == nil {
			rec.Text = event.Text
			rec.URL = event.URL
		}
		rec.Status = classifyDelivery(rec.DeliveredAt, rec.Attempts)
		if rec.UserID != "" {
			userIDs[rec.UserID] = struct{}{}
			index[rec.UserID] = append(index[rec.UserID], len(records))
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate deliveries: %w", err)
	}

	if len(userIDs) > 0 {
		ids := make([]string, 0, len(userIDs))
		for id := range userIDs {
			ids = append(ids, id)
		}
		handles := map[string]string{}
		users, err := s.Client.User.Query().Where(user.IDIn(ids...)).All(ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("load delivery users: %w", err)
		}
		for _, u := range users {
			handles[u.ID] = u.Handle
		}
		for id, positions := range index {
			for _, pos := range positions {
				records[pos].UserHandle = handles[id]
			}
		}
	}
	return records, total, nil
}

// DeliveryStatusCounts returns how many rows fall into each delivery status.
func (s *Service) DeliveryStatusCounts(ctx context.Context) (map[string]int, error) {
	counts := map[string]int{
		DeliveryStatusDelivered: 0,
		DeliveryStatusPending:   0,
		DeliveryStatusRetrying:  0,
		DeliveryStatusAbandoned: 0,
	}
	if s == nil || s.SQL == nil {
		return counts, nil
	}
	var delivered, pending, retrying, abandoned int
	err := s.SQL.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN delivered_at IS NOT NULL THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN delivered_at IS NULL AND attempts = 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN delivered_at IS NULL AND attempts > 0 AND attempts < `+strconv.Itoa(outboxMaxAttempts)+` THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN delivered_at IS NULL AND attempts >= `+strconv.Itoa(outboxMaxAttempts)+` THEN 1 ELSE 0 END), 0)
		FROM delivery_outbox`).
		Scan(&delivered, &pending, &retrying, &abandoned)
	if err != nil {
		return nil, fmt.Errorf("count delivery statuses: %w", err)
	}
	counts[DeliveryStatusDelivered] = delivered
	counts[DeliveryStatusPending] = pending
	counts[DeliveryStatusRetrying] = retrying
	counts[DeliveryStatusAbandoned] = abandoned
	return counts, nil
}

// RetryDelivery resets an abandoned row so the worker picks it up again. The
// update only matches rows that are not deliverable and have exhausted their
// attempts, so it never races a row that is still in flight and never revives a
// delivered row. It returns false when no row matched.
func (s *Service) RetryDelivery(ctx context.Context, id int) (bool, error) {
	if s == nil || s.SQL == nil || id <= 0 {
		return false, nil
	}
	res, err := s.SQL.ExecContext(ctx,
		`UPDATE delivery_outbox
		 SET attempts = 0, next_attempt_at = ?, delivered_at = NULL, last_error = ''
		 WHERE id = ? AND delivered_at IS NULL AND attempts >= ?`,
		now(), id, outboxMaxAttempts)
	if err != nil {
		return false, fmt.Errorf("retry delivery: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("retry delivery rows: %w", err)
	}
	return n > 0, nil
}

// isDeliveryStatus reports whether status is one of the history filters.
func isDeliveryStatus(status string) bool {
	switch status {
	case "", DeliveryStatusDelivered, DeliveryStatusPending, DeliveryStatusRetrying, DeliveryStatusAbandoned:
		return true
	default:
		return false
	}
}
