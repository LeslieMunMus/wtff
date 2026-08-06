package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	deletionengine "github.com/lesliemusengi/wtff/internal/deletion-engine"
	"github.com/lesliemusengi/wtff/internal/diagnostics"
)

// The JSON surface is a stable contract, so it is defined here as its own
// types rather than by marshalling the engine's internals directly. Those are
// free to change shape; this is not, and having them be the same declaration
// would make an internal refactor an unannounced break for anything parsing
// the output.
//
// Every document carries the version that produced it, so a consumer can tell
// what it is reading without inferring it from which fields exist.

type jsonDocument struct {
	WtffVersion string `json:"wtff_version"`
	Command     string `json:"command"`

	// Exactly one of the following is populated, chosen by the command.
	Plan   *jsonPlan       `json:"plan,omitempty"`
	Result *jsonResult     `json:"result,omitempty"`
	Staged []jsonBatch     `json:"staged,omitempty"`
	Doctor *jsonDiagnostic `json:"doctor,omitempty"`
}

type jsonPlan struct {
	Action string      `json:"action"`
	DryRun bool        `json:"dry_run"`
	Items  []jsonEntry `json:"items"`

	// TotalBytes sums only the items whose size is known. SizeComplete says
	// whether that is the whole story, so a consumer never presents a floor as
	// a total.
	TotalBytes   int64      `json:"total_bytes"`
	SizeComplete bool       `json:"size_complete"`
	Skipped      []jsonSkip `json:"skipped"`
}

type jsonEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	SizeKnown bool   `json:"size_known"`
	RuleID    string `json:"rule_id"`
	Reason    string `json:"reason"`
}

type jsonSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type jsonResult struct {
	Action       string       `json:"action"`
	AppliedCount int          `json:"applied_count"`
	SkippedCount int          `json:"skipped_count"`
	FailedCount  int          `json:"failed_count"`
	BytesApplied int64        `json:"bytes_applied"`
	BatchID      string       `json:"batch_id,omitempty"`
	Reversible   bool         `json:"reversible"`
	Outcomes     []jsonChange `json:"outcomes"`
}

type jsonChange struct {
	Path    string `json:"path"`
	Applied bool   `json:"applied"`
	Skipped bool   `json:"skipped"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
}

type jsonBatch struct {
	BatchID   string    `json:"batch_id"`
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
	ItemCount int       `json:"item_count"`
	Bytes     int64     `json:"bytes"`

	// SizeComplete is false when any item's size was unknown at staging time.
	SizeComplete bool `json:"size_complete"`
}

type jsonDiagnostic struct {
	NeedsAttention bool          `json:"needs_attention"`
	Findings       []jsonFinding `json:"findings"`
}

type jsonFinding struct {
	Area    string   `json:"area"`
	Level   string   `json:"level"`
	Summary string   `json:"summary"`
	Detail  []string `json:"detail,omitempty"`
}

// emitJSON writes one document and nothing else to the stream.
//
// Indented rather than compact. The output is read by people at least as often
// as by programs, `jq` handles either, and a wall of one line JSON is a poor
// thing to hand someone debugging a machine at midnight.
func emitJSON(w io.Writer, doc jsonDocument) error {
	doc.WtffVersion = Version
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(doc)
}

func planToJSON(manifest *deletionengine.Manifest, skips []skippedCandidate, dryRun bool) *jsonPlan {
	plan := &jsonPlan{
		Action:       string(manifest.Action),
		DryRun:       dryRun,
		Items:        make([]jsonEntry, 0, len(manifest.Entries)),
		TotalBytes:   manifest.TotalBytes,
		SizeComplete: !manifest.PartialSizing,
		Skipped:      make([]jsonSkip, 0, len(skips)),
	}
	for _, entry := range manifest.Entries {
		plan.Items = append(plan.Items, jsonEntry{
			Path:      entry.ResolvedPath,
			SizeBytes: entry.SizeBytes,
			SizeKnown: entry.SizeKnown,
			RuleID:    entry.RuleID,
			Reason:    entry.Reason,
		})
	}
	for _, skip := range skips {
		plan.Skipped = append(plan.Skipped, jsonSkip{Path: skip.path, Reason: skip.reason})
	}
	return plan
}

func resultToJSON(action deletionengine.Action, result *deletionengine.Result) *jsonResult {
	out := &jsonResult{
		Action:       string(action),
		AppliedCount: result.AppliedCount,
		SkippedCount: result.SkippedCount,
		FailedCount:  result.FailedCount,
		BytesApplied: result.BytesApplied,
		// Reversible is stated rather than left to be inferred from the
		// action, because whether the removal can be undone is the single
		// most consequential fact in this document.
		Reversible: action == deletionengine.ActionStage,
		Outcomes:   make([]jsonChange, 0, len(result.Outcomes)),
	}
	if result.Batch != nil {
		out.BatchID = result.Batch.BatchID
	}
	for _, outcome := range result.Outcomes {
		change := jsonChange{
			Path:    outcome.Entry.ResolvedPath,
			Applied: outcome.Applied,
			Skipped: outcome.Skipped,
			Reason:  outcome.Reason,
		}
		if outcome.Err != nil {
			change.Error = outcome.Err.Error()
		}
		out.Outcomes = append(out.Outcomes, change)
	}
	return out
}

func diagnosticToJSON(report diagnostics.Report) *jsonDiagnostic {
	out := &jsonDiagnostic{
		NeedsAttention: report.NeedsAttention(),
		Findings:       make([]jsonFinding, 0, len(report.Findings)),
	}
	for _, finding := range report.Findings {
		out.Findings = append(out.Findings, jsonFinding{
			Area:    finding.Area,
			Level:   finding.Level.String(),
			Summary: finding.Summary,
			Detail:  finding.Detail,
		})
	}
	return out
}

// refuseInteractiveJSON stops a command from writing a JSON document and then
// asking a question.
//
// A prompt printed into the same stream would corrupt the document, and a
// prompt printed elsewhere would leave a script waiting on an answer nobody is
// there to give. Requiring the decision to be made in advance keeps the output
// parseable and the behavior predictable.
func refuseInteractiveJSON(command string, stderr io.Writer, jsonOut, dryRun, yes bool) bool {
	if !jsonOut || dryRun || yes {
		return false
	}
	fmt.Fprintf(stderr, "wtff %s: --json needs --dry-run or --yes, because it cannot "+
		"stop to ask for confirmation without corrupting its own output\n", command)
	return true
}
