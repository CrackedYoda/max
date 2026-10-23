-- name: InsertAuditLog :exec
INSERT INTO audit_logs (actor_account, action, resource_type, resource_id, metadata)
VALUES ($1, $2, $3, $4, $5);
