# internal/path-validation

Structural safety checks: absolute-path requirements, traversal rejection, ancestor symlink
resolution, and capability-based file opens. This package answers whether it is structurally
safe to touch a path. It does not decide policy; that is `internal/protection-rules`.

Not yet implemented. Built alongside `internal/deletion-engine` in Phase 1.
