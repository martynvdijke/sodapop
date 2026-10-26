package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"sodapop/ent"
	"sodapop/ent/deliveryoutbox"
	"sodapop/ent/integration"
	"sodapop/ent/pushsubscription"
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
