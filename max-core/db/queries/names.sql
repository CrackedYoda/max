-- name: LockName :exec
-- Every operation that makes a name unavailable (claim, rename reservation,
-- rename finalize) takes this lock first, so they can never race.
SELECT pg_advisory_xact_lock(hashtextextended('maxid-name:' || sqlc.arg(name)::text, 0));

-- name: NameUsage :one
SELECT
    EXISTS (SELECT 1 FROM max_ids m WHERE m.canonical_name = sqlc.arg(name)) AS taken,
    EXISTS (SELECT 1 FROM rename_requests r WHERE r.new_name = sqlc.arg(name) AND r.status = 'awaiting_payment') AS pending_rename,
    (SELECT q.max_id FROM name_quarantine q WHERE q.canonical_name = sqlc.arg(name) AND q.quarantined_until > now())::uuid AS quarantined_for;
