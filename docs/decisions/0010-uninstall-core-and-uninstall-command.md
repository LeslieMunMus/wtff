# 0010: uninstall core and the uninstall command

Status: done.

## Context

Two of three planned commands existed. This entry covers `internal/uninstall-core`, application
discovery and leftover matching, and `wtff uninstall`, the command built on it. It also covers a
flag-parsing defect found while testing this stage that affected every existing command, not
just the new one, and was fixed across all of them in the same pass.

## What was built

`internal/uninstall-core`: `DiscoverApps` scans `/Applications` and `~/Applications` for `.app`
bundles and reads identity from `Info.plist` via `plutil`, the same tool that ships with every
real macOS install. `FindApp` resolves a query against discovered applications by exact,
case-insensitive match on display name, bundle identifier, or filename, never a partial or
substring match. `IsProtectedApp` refuses any application whose bundle identifier carries the
`com.apple.` prefix, checked by identity rather than inferred from install location.
`DiscoverLeftovers` proposes candidate paths built from the exact bundle identifier and exact
display name against the conventional locations macOS uses for per-application data, with no
name-variant generation.

`wtff uninstall [--dry-run] [--purge] [--yes] <app name or bundle id>` wires this to the same
plan, apply, and confirmation flow `clean` and `remove` already use.

## Decisions worth recording

**Matching is exact only, deliberately less capable than it could be.** No space, hyphen, or
underscore substitution, no case folding beyond what the filesystem already applies, no
stripping of version or channel suffixes. Every one of those would find more real leftovers and
would also risk more wrong matches, and the second risk is the one this package exists to avoid.
A leftover this misses can still be found and removed by hand with `wtff remove`; a leftover
wrongly matched here would already be staged before anyone had a chance to notice.

**Zero matches and multiple matches are both reported, never resolved automatically.** An exact
match that finds nothing gives a person the chance to correct a typo. An ambiguous match is
listed in full rather than picked between, since choosing the wrong one of two similarly named
applications is exactly the kind of mistake this discipline exists to prevent.

**Apple's own applications are refused by bundle identity, not by install path.** Path
validation's structural floor denies `/System` entirely, which was assumed to be enough
protection for Apple's own software. It is not: Safari, and two Creator Studio launcher bundles
for Final Cut Pro and Logic Pro, were all found installed directly under `/Applications` during
this package's design, with `com.apple.` bundle identifiers, entirely outside that floor.
Checking identity catches all three uniformly and would continue to catch a similarly placed
case this project has not yet seen.

## A defect in discovery itself, found while trying to prove the identity check worked

Testing `wtff uninstall Safari` against the compiled binary, expecting a clear refusal, instead
produced "no installed application matches." Safari was not being discovered at all, which meant
the bundle identity check built specifically for it was never being reached.

The cause: `/Applications/Safari.app` is a symlink into a separate sealed system volume,
`../System/Cryptexes/App/System/Applications/Safari.app`, part of how modern macOS packages its
own applications. `os.ReadDir`'s `DirEntry.IsDir()` reports a directory entry's own type, which
for a symlink is false regardless of what it resolves to, and discovery's filter excluded
anything that was not a directory by that check. Safari was invisible, silently: not found, not
skipped, not reported anywhere. The refusal that appeared to be working in design review had
never actually been exercised against the one application it was written for.

Fixed by resolving with `os.Stat`, which follows the link, rather than trusting the directory
entry's own type. Safari, and the two Creator Studio bundles, are now correctly discovered and
correctly refused, confirmed against the compiled binary, not only against a test.

## A flag ordering defect, found while testing this stage, affecting every command

Testing `wtff uninstall SomeApp --dry-run` led to testing the same flag placement against
`remove`, since both share argument handling. `wtff remove /tmp/cache --dry-run` did not behave
as a dry run. The standard library's `flag` package stops looking for flags at the first
argument that is not one; everything after that point becomes positional, silently, with no
error. `--dry-run` became a second path argument, literally named `--dry-run`, which did not
exist and was reported as an ordinary skip. The dry-run flag itself was never set, and the
command fell through to a real confirmation prompt. Confirmed directly: the compiled binary was
run this exact way, and only an empty piped stdin prevented a real removal, since the prompt
defaulted to no. A person who typed the flag in that position, which many other command line
tools accept without complaint, and who answered that prompt trusting the dry run they believed
they had requested, would have had a real removal instead.

This was treated as a defect affecting the whole command surface, not a one-off in the new
command, because `clean`, `undo`, and `staged` all parse arguments the same way and were all
equally exposed. Fixed with `reorderFlagsFirst`, which moves recognized boolean flags to the
front of the argument list before `flag.Parse` runs, preserving order within both groups, and
respecting a bare `--` as the point after which nothing is reordered, matching `flag.FlagSet`'s
own convention for a path that happens to start with a dash. Applied to every command's parse
call, not only the two where the defect was found.

## Verification

Three hundred and twenty nine tests passing across the tree, `go vet` clean, `gofmt` clean.

Discovery tests build real `.app` bundles with real `Info.plist` files and exercise the actual
`plutil` binary rather than a mock of its output shape. Matching tests specifically assert that
a query such as "Code" does not match "Code Editor Pro". Safety tests cover the three concrete
cases found during design, plus a case built to confirm the check matches on the namespace
prefix and not merely the substring "apple" appearing anywhere in an identifier. Leftover tests
include a direct regression for an empty identity value, which would otherwise collapse a
candidate into its shared parent directory rather than a specific application's entry within it.

The full stack was also run against this real machine: a synthetic application with real
leftover data was placed under the real `~/Applications`, discovered, staged, and undone, with
restored file contents read back and compared byte for byte, then the test artifacts were
removed by hand. `~/Library/Cookies` returned a permission error during that run, evidently a
TCC restriction on this terminal session; wtff reported it as an ordinary skip with a clear
reason rather than failing the run, which is the correct behavior for a location the tool
legitimately cannot read.

## Known limitations

No vendor-specific protected-application list exists yet, for VPN clients, endpoint security
agents, or similar software where uninstalling through wtff rather than the vendor's own
mechanism could leave the system in a worse state than either uninstalling cleanly or not at
all. This is real, deferred work, not an oversight; it needs research per vendor, the same
discipline applied to the Apple service state findings in decision 0009, and should not be
rushed to close out this entry.

Leftover matching is bounded to the templates listed in this entry. Group Containers, which use
a `group.<vendor>...` naming convention that does not reliably derive from an application's own
bundle identifier, are not covered, on the same reasoning that excluded fuzzy name matching: weak
evidence is worse than a missed leftover.

There is no elevated-privilege support. An application bundle or leftover the invoking user
cannot write to fails at apply time with a reported error, which is honest but not yet actionable
beyond that.

## How this could improve with time

Extend leftover templates as real evidence justifies each one, the same incremental discipline
used for the Apple service state protections. Build the vendor-specific protected-application
list once real cases exist to research rather than guessing at which vendors need it. Revisit
whether `wtff uninstall` should accept a path directly, for the case where two applications
share every matchable field and only their install location distinguishes them.
