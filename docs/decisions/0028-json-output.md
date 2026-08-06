# 0028: machine readable output

Status: done.

`--json` on `clean`, `purge`, `remove`, `uninstall`, `staged`, and `doctor`
emits one document instead of human readable output. It pairs with the
diagnostic from decision 0026: a scheduled job that watches a machine wants
the exit status to decide and the document to explain.

## One document, and nothing else

The property the whole feature rests on is that stdout holds exactly one JSON
document. A stray line of prose alongside it makes the stream unparseable, and
does so only in the circumstance that produced the extra line, which is the
worst way for a contract to break: it works in testing and fails in the
situation nobody rehearsed.

So every message that is not the document goes to stderr, and the tests decode
stdout strictly, failing on a leading byte of prose or a second document.

## Refusing to ask

With `--json`, a command that would stop for confirmation refuses to start.
Printing the prompt into the stream would corrupt the document; printing it
elsewhere would leave a script waiting on an answer nobody is there to give.
`--dry-run` or `--yes` is required, which forces the decision to be made
before the run rather than during it.

## Shape decisions

The types are declared separately from the engine's own rather than by
marshalling internals. Those are free to change; this is a contract, and
sharing one declaration would turn an internal refactor into an unannounced
break for anything parsing the output.

`reversible` is stated rather than left to be inferred from `action`, because
whether a removal can be undone is the single most consequential fact in the
document and nobody should have to know that "stage" implies it.

`size_complete` accompanies `total_bytes`, so a consumer never presents a floor
as a total. Empty collections are empty arrays rather than null.

Output is indented. It is read by people at least as often as by programs, `jq`
handles either, and a wall of single line JSON is a poor thing to hand someone
debugging a machine at midnight.

## Red team

**Uninstall corrupted its own document.** It prints the application it resolved
and any privileged components that will survive, both to stdout, both before
the plan. Under `--json` those landed in front of the document and made it
unparseable from the first byte.

This was caught by writing the strict decode test first and watching it fail,
rather than by reading the code, and the mutation restoring the unguarded print
fails it again. The privileged component note is now suppressed under `--json`
and is not yet represented in the JSON surface, which is recorded below as a
gap rather than papered over by printing it anyway.

## Verification

Full suite green. Verified against the installed binary: `doctor --json`,
`clean --dry-run --json`, `clean --yes --json`, and `staged --json` each parse,
carry the version that produced them, and report the fields they promise, and
`clean --json` without `--dry-run` or `--yes` exits 2 having written nothing to
stdout.

## Known limitations

The privileged components uninstall would leave behind are omitted from the
JSON output rather than represented in it. A script cannot currently learn that
a root daemon will survive an uninstall, which is exactly the sort of thing a
script should be able to check.

`undo` has no JSON output, so a script can list and purge staged batches but
must parse human readable text to confirm a restore.
