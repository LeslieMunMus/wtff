// Package cleancatalog proposes candidates for the clean command, from
// declarative data rather than code.
//
// It is the mirror image of internal/protection-rules. That package answers
// "must this never be removed"; this package answers "where does reclaimable
// space usually live." Neither package trusts the other to be complete. A
// path this package proposes still passes through the deletion engine's full
// path validation and protection rule check before anything happens to it,
// exactly as if a person had typed the path directly into wtff remove. This
// package's only job is deciding what to suggest, never deciding what is
// safe.
//
// # Why third party caches are treated differently from Apple's own
//
// The default assumption for a directory under ~/Library/Caches is that it
// is safe to propose: that is the documented contract of the directory, and
// third party applications generally honor it, since they are consumers of a
// published API rather than owners of its internal state.
//
// Apple's own first party services do not reliably follow that contract for
// their own directories under the same tree. Inspecting a real machine while
// this package was designed found FamilyCircle, CloudKit, GameKit and
// dozens of entries under the com.apple. namespace holding account, family
// sharing, cloud sync and home automation state, not disposable content,
// despite living in a directory named Caches. So candidate discovery
// excludes the com.apple. namespace under Caches by default, as a
// conservative default and a performance optimization, and
// internal/protection-rules independently protects the same namespace and
// the specific named exceptions found during that inspection, so the
// exclusion here is a courtesy, not the actual safety boundary. A gap in
// this package's exclusion list would still be caught there.
package cleancatalog
