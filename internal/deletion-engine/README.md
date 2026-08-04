# internal/deletion-engine

The single funnel every destructive operation passes through. No package in this project
deletes, moves, or truncates a file directly; every such action calls into this package, which
in turn consults `internal/path-validation` and `internal/protection-rules` before acting.

Not yet implemented. This is the first package built in Phase 1, and the one given the most
review before anything is layered on top of it.
