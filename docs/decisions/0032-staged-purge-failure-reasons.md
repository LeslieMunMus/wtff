# 0032: staged purge now names why an item failed

Status: done.

## The gap

`wtff staged --purge` reported a failure as a bare count: "N item(s) could not
be deleted and are still staged." No path, no reason. Every other command that
can fail an item, the initial clean and purge, uninstall, remove, and undo,
already printed the underlying OS error alongside the path. `staged --purge`
was the one place in the CLI that had the detail sitting in
`PurgeResult.Outcomes` and aggregated it away before printing anything.

This was found by checking, not assumed: `PurgeOutcome.Err` was already
populated by the engine with the real syscall error, `staged --purge`'s loop
simply summed `FailedCount` and never read `Outcomes`.

## The fix

Failed outcomes are collected across every targeted batch, and each one is
printed under the summary line with its path and `outcome.Err.Error()`, the
same shape `printResult` already uses elsewhere in this package.

## Verification

A test stages a real item, makes its batch's `items/` directory unwritable
with `chmod 0500`, so the kernel refuses the unlink with a real permission
error, and asserts the output names the path and contains actual permission
denied text, not a placeholder. Confirmed by reverting the fix and watching
the test fail before restoring it: the old code passed the same scenario
silently, printing only a count.
