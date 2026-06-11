-- +goose Up
-- The `deadline` column backed a deadline-based priority scheduler
-- (SetDeadline/GetDeadline/EffectivePriority) that was never wired into the
-- engine — it was a dead feature removed alongside bandwidth.go. Drop the
-- column so the schema stops advertising a knob nothing reads or writes.
ALTER TABLE torrents DROP COLUMN deadline;

-- +goose Down
ALTER TABLE torrents ADD COLUMN deadline TEXT;
