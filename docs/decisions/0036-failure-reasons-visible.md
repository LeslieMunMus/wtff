# 0036: failure reasons are shown, not folded away

Status: done.

## The defect, found by using it

A real run reported:

```
  ✓ Deleted 0 item(s) permanently · 0B reclaimed
      ▸ details (1 lines) · ctrl+o
  ✗ 1 item(s) could not be deleted and are still staged.
```

Two things wrong in three lines. The reason the item failed was sitting in the
details of the line announcing success, which is the last place anyone would
look for it. And a run that deleted nothing carried a green tick reading
"Deleted 0 item(s) permanently," directly above an error saying it had not.

The command line path had already been corrected in decision 0032. This is the
same defect in the shell, which was missed because the fix was made where the
report was noticed rather than everywhere the report is produced.

## What changed

`errorEntry` now takes details and renders them expanded. Every other entry
keeps the disclosure toggle, which earns its place on a list of successes where
the headline is the whole story and the paths are reference. It is exactly
wrong on a failure: "one item could not be deleted" without the reason is a
sentence someone then has to go hunting to make actionable, when the reason was
already in hand as the line was written.

Successes and failures are now built as separate lists rather than one
interleaved one, in both purge and undo, so a reason can no longer end up
attached to the wrong headline. The success line is omitted entirely when
nothing succeeded.

Undo additionally reports why an item was left in staging, most often that
something else now occupies its original location, which is as actionable as
an outright failure.

## What the reason turned out to be

`~/Library/Caches/Homebrew` failed with "cannot remove decode_test.go:
permission denied." Homebrew unpacks Go source tarballs into its cache, and Go
extracts module files read only at mode 0444. A read only file cannot be
unlinked without first making its containing directory or the file writable.

wtff does not do that, and should not: changing permissions on a person's files
in order to force a deletion through is a larger act than the deletion itself,
and it would happen at exactly the moment nobody is watching. The honest
outcome is a failure that says why.

## Red team

The fix was verified by reverting it: with details collapsed again, the test
asserting a reason is visible without pressing anything fails. Two further
tests pin the boundary, that an error with no details stays a single line, and
that success details remain behind the toggle, so the change does not turn
every long list into a wall of text.

## Known limitations

Read only files inside a staged batch will keep failing this way. Offering to
clear the read only bit is a possible future option, and would need to be
explicit rather than automatic.
