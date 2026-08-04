# Naming conventions

Status: done.

## Rule

Every file and directory created for this project uses kebab-case: lowercase words separated
by single hyphens, chosen to be descriptive on their own without needing the surrounding path
for context. Examples: `deletion-engine`, `path-validation-rules.yaml`,
`launch-agent-scanner.go`, `0001-project-foundation-and-repository-layout.md`.

## Exceptions

Filenames a toolchain requires by exact spelling keep their conventional form, because
renaming them silently breaks the tooling that reads them:

- `go.mod`, `go.sum`
- `README.md` (including inside subdirectories, since GitHub and most editors render it
  specially)
- `LICENSE`
- `.gitignore`, `.gitattributes`
- Anything under `.github/workflows/` that GitHub Actions expects by convention
- `Makefile`

Go source files inside `internal/` and `cmd/` packages follow Go's own convention
(`snake_case.go` or a single descriptive lowercase word, matching the package name) rather than
kebab-case, since Go tooling and community convention both expect that and fighting it buys
nothing. Kebab-case governs directory names and freeform documentation, data, and asset files.

## Why this split

A blanket rule that ignored tooling conventions would produce a repository that looks
disciplined on the surface but breaks `go build`, breaks GitHub Actions autodetection, or stops
rendering correctly on GitHub. The rule is applied where we have full freedom to choose a name,
and set aside only where a name is effectively load-bearing for a tool we depend on.

## How this could improve with time

If the project grows a documentation site or a package registry entry, revisit whether asset
filenames referenced by URL (for example in a generated docs site) need a stricter, URL-safe
subset of kebab-case than what plain filesystem use requires.
