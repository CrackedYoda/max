package audit

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/max/max-core/db/generated"
)

type Entry struct {
	Actor        *uuid.UUID
	Action       string
	ResourceType string
	ResourceID   string
	Metadata     map[string]any
}

// Write records an audit entry using the caller's queries, so it commits or
// rolls back with the change it describes.
func Write(ctx context.Context, q *generated.Queries, e Entry) error {
	meta := []byte("{}")
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return err
		}
		meta = b
	}
	return q.InsertAuditLog(ctx, generated.InsertAuditLogParams{
		ActorAccount: e.Actor,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Metadata:     meta,
	})
}
