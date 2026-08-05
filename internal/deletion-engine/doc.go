// Package deletionengine is the single funnel every destructive operation in
// wtff passes through.
//
// No other package removes, moves, or truncates anything. Everything asks this
// package, and this package is the only place that consults path validation and
// protection policy. The value of that arrangement is not tidiness: it is that
// there is exactly one place to audit, and a new feature cannot accidentally
// acquire its own weaker deletion path.
//
// # Plan and apply
//
// Work is split in two. Plan takes candidates and produces a manifest: an
// ordered, inspectable list of what would happen, with a reason on every entry
// and a digest over the whole thing. Apply takes a manifest and nothing else.
// It recomputes the digest first, so a plan that was altered after review is
// refused rather than executed.
//
// This split exists so that a preview is a real artifact rather than a printout
// that happens to resemble what will run. The thing shown to a person and the
// thing executed are the same object.
//
// # What protects an entry between plan and apply
//
// Within a single resolution, path validation pins the containing directory
// open, so the directory that was checked is the directory acted on. That
// guarantee cannot span plan and apply: a manifest is meant to be saved,
// reviewed, and applied later, and descriptors do not survive that.
//
// So the plan to apply boundary is protected differently. Each entry carries
// the device and inode of the object that was validated. Apply re-resolves the
// path, and refuses the entry unless the identity still matches. An object
// swapped in the meantime is skipped and recorded, never removed on the
// strength of having the right name. This is a deliberate trade of some
// strength for the ability to persist and review a plan, and it is the reason
// identity is stored in the manifest rather than derived at apply time.
//
// # Reversibility
//
// Staging is the default and permanent removal is the exception, for every
// command rather than for some of them. Entries move into a directory wtff
// owns, with a record of where each came from, and undo puts them back. Space
// is reclaimed when the retention window passes.
package deletionengine
