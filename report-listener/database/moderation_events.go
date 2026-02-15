package database

import (
	"context"
	"encoding/json"
	"fmt"
)

type ModerationEvent struct {
	Actor      string
	ActorIP    string
	Action     string
	TargetType string
	TargetID   string
	Details    any
	RequestID  string
}

// InsertModerationEvent appends a moderation/audit row (best-effort).
// Callers should treat failures as non-fatal; the primary operation should not depend on this log.
func (d *Database) InsertModerationEvent(ctx context.Context, ev ModerationEvent) error {
	var detailsJSON []byte
	if ev.Details != nil {
		if b, err := json.Marshal(ev.Details); err == nil {
			detailsJSON = b
		}
	}

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO moderation_events (
			actor, actor_ip, action, target_type, target_id, details, request_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		nullableStr(ev.Actor),
		nullableStr(ev.ActorIP),
		ev.Action,
		ev.TargetType,
		ev.TargetID,
		nullableBytes(detailsJSON),
		nullableStr(ev.RequestID),
	)
	if err != nil {
		return fmt.Errorf("insert moderation_events: %w", err)
	}
	return nil
}
