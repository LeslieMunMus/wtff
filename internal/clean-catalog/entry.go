package cleancatalog

// Kind is how an entry's path is turned into candidates.
type Kind string

const (
	// KindContainer enumerates the immediate children of a directory, each
	// becoming its own candidate. Used where a directory holds many
	// independent, individually meaningful items, such as one subdirectory per
	// application under ~/Library/Caches.
	KindContainer Kind = "container"

	// KindOpaque treats the entire path as one candidate and does not look
	// inside it. Used where a directory's internal layout is not meaningful to
	// a person reviewing what would be removed, such as a content addressed
	// store, so that reviewing a hundred hash named entries individually would
	// not communicate anything a single line does not already say.
	KindOpaque Kind = "opaque"
)

// Provenance records where an entry's justification came from, so a later
// reader can check it rather than trust it.
//
// This duplicates the shape of protectionrules.Provenance rather than
// importing it. The two packages describe opposite things for different
// reasons, and keeping them independently defined means a change to one
// schema cannot silently reshape the other; the cost is a few duplicated
// field names, which is cheaper than the coupling.
type Provenance struct {
	Source   string `yaml:"source"`
	Method   string `yaml:"method"`
	Verified string `yaml:"verified"`
}

// Entry is one proposed cache category.
type Entry struct {
	ID       string `yaml:"id"`
	Category string `yaml:"category"`
	Kind     Kind   `yaml:"kind"`
	Path     string `yaml:"path"`
	Reason   string `yaml:"reason"`

	// ExcludePrefixes skips a container's immediate children whose name starts
	// with one of these values, compared case-insensitively. Meaningless for
	// KindOpaque, since that kind never enumerates children.
	ExcludePrefixes []string `yaml:"exclude_prefixes,omitempty"`

	Provenance Provenance `yaml:"provenance"`

	// origin names the file this entry came from, for diagnostics.
	origin string
}

// Origin reports which catalog file defined this entry.
func (e Entry) Origin() string { return e.origin }

// entryDocument is the on disk shape of a catalog file.
type entryDocument struct {
	Version int     `yaml:"version"`
	Entries []Entry `yaml:"entries"`
}
