# 0035: the space browser

Status: done.

`space` in the shell measures the home directory and opens a browser over the
result, largest first, with arrow keys to descend and the same checklist every
other command uses to stage a selection.

## Nothing is preselected

Clean's checklist arrives with everything ticked, because clean proposes a set
it can justify from a catalog with written provenance for each entry. This
proposes nothing. It shows what is there and waits.

Preselecting a person's own files because they happen to be large would be the
tool forming an opinion it has no basis for, and the difference between "these
are caches I can account for" and "this is your Downloads folder" is the whole
distinction the catalog exists to draw.

## Selection is per directory

Marks do not carry across a descent. Allowing them to would let someone select
a directory, walk into it, select a child, and stage both a parent and
something inside it, leaving the engine to reconcile an overlap that never
needed to exist. Staging what is marked where it is marked keeps that situation
from arising at all.

## Manual selection is not a bypass

A path chosen by hand goes through the same `Plan` call as clean's catalog
candidates, with the same policy, the same structural floor, and the same
staging. Someone can select their own keychain in this browser; the engine is
what refuses it, and a test proves that by selecting a keychain directory and
requiring an empty manifest.

The action stays `stage`. A hand picked selection is as reversible as anything
else and must not quietly become permanent because a person chose it rather
than a catalog proposing it.

## Escape climbs before it leaves

From a nested directory, escape moves up one level. Only at the top does it end
the flow. Someone four levels deep reaching for the key that means "back"
should not be thrown out of the whole browser.

## Partial totals say so

A directory the scan could not fully read carries "partly unreadable so this
total is a floor" on its own row, not only in the summary. That number is what
a person is about to make a deletion decision against, and the qualification
belongs where the number is.

## Red team

Three guards were each broken in turn and the matching test confirmed to fail:
swapping the policy for `AllowAll`, changing the action from stage to purge,
and preselecting every row.

## Verification

Full suite green. Against the installed binary through a pseudo terminal: a
fixture home measured 3.8MB across 11 entries, listed Projects at 3.0MB above
Downloads at 800KB above a 40 byte directory, and the right arrow descended
into `~/Projects` with the title tracking position.

## Known limitations

The scan runs once when the flow starts. Deleting something and continuing to
browse shows the pre deletion tree until the flow is restarted.

Only the home directory is measured, which was the agreed default. There is no
way yet to point it at another path.
