# 0029: shell completion

Status: done.

`wtff completion zsh` and `wtff completion bash` print a script. Completion
covers commands, per command flags, staged batch identifiers, and installed
application names.

## Printed, not installed

The script is emitted to standard output rather than written into a shell's
completion directory. Choosing that directory means guessing which of several
locations is on a particular user's fpath, and a tool whose job is removing
files should not quietly add one somewhere nobody asked for.

## The dynamic half

Two hidden commands answer what depends on the machine: `__complete-batches`
lists staged batch identifiers, `__complete-apps` lists installed application
names. Batch identifiers are long and random by design, so completing one is
the difference between typing it and copying it.

They are hidden rather than documented because their output shape is whatever
the scripts need, and describing them to a person would make that shape a
contract. Both exit zero and print nothing on failure, because a completion
that reports an error into the middle of a prompt is worse than one offering
no suggestions.

Names are emitted one per line. Application names routinely contain spaces, and
splitting on whitespace turns "Google Chrome" into two useless suggestions.

## Written for the shells that are actually installed

The bash script targets bash 3.2, which is what macOS ships. No `mapfile`, no
`readarray`, no associative arrays: all arrived in bash 4 and would fail on the
exact system this is written for. A test refuses those three by name.

## Keeping it from going stale

The failure mode for completion is silence. A command gets added, the script is
never updated, and the only symptom is someone pressing tab and being told the
thing they just read about does not exist.

So a test reads the dispatcher's own switch statement and requires every
command it answers to appear in both scripts. Another test takes each flag the
completion offers and runs the command with it, failing if the command replies
that the flag is not defined. Neither can be satisfied by remembering to update
a list.

`make check` now also runs `zsh -n` and `bash -n` against the generated
scripts. These are built by string concatenation, which is exactly where an
unbalanced quote hides until someone sources the file, and a Go test can check
structure but not whether a shell will parse it.

## What was not verified

An end to end completion was not driven successfully. Two attempts to capture
real candidates through `zpty`, which is how zsh's own suite tests completions,
returned the setup echo rather than the completion output, and the
synchronisation was not worth more time against what it would have added.

So the evidence is: both scripts parse in their real shells, the function loads
and runs under `compinit`, every dispatched command appears, every offered flag
is accepted by its command, and the dynamic helpers return real data from this
machine. What has not been shown is a candidate list appearing after a tab
press. That is worth trying by hand once installed.

## Known limitations

`staged --purge` completion offers batch identifiers through `_alternative`
alongside flags, which is less precise than making the identifier conditional
on `--purge` already being present.

`__complete-apps` runs the full application discovery, which takes a noticeable
fraction of a second on a machine with many applications. Acceptable for a tab
press, and not free.

Only zsh and bash. Fish and nushell users get nothing.
