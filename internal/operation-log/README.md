# internal/operation-log

Structured, append-only record of every operation wtff performs, for audit and for a future
history command.

Not yet implemented. Built alongside the deletion engine in Phase 1, since the engine should
never have a code path that deletes without also logging.
