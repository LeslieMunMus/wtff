# 0039: the duplicates interface

Status: done.

`duplicates` searches the home directory, lists groups of identical files by
what acting on them would free, and offers two things to do with a group.

## Two actions, neither of them the default

Merge gathers every copy beside the oldest one and deletes nothing. Staging
removes the copies a person marks, reversibly, through the deletion engine.

Both are present because they answer different questions. Merge is for copies
someone wants to keep and gather; staging is for copies that are genuinely
surplus. A tool offering only the second would be assuming an answer that
belongs to the person whose files these are.

Nothing arrives selected, for the same reason the space browser selects
nothing: this shows what is there rather than proposing what to do.

## What the screen has to show

The oldest copy is labelled "oldest, kept by merge" rather than merely being
first. Someone deciding what to remove needs to see which copy a merge would
leave in place, and which one they are about to mark.

Pressing `o` opens the copy under the cursor in the Finder. Comparing two
images or two documents is not something a terminal can do, and pretending
otherwise by showing sizes and dates would leave the actual question
unanswered. Opening reveals rather than changes, so it needs no confirmation,
and its errors are dropped because a preview that will not open is not worth
interrupting a decision over.

## Selecting every copy is refused

Marking all of them would remove the file entirely, which is almost certainly
not what someone browsing duplicates meant. The refusal says so and leaves the
selection alone. A confirmation would have been the weaker choice: this is a
case where the right answer is known, so asking would only be a chance to
click through.

## Staging is not a bypass

A copy marked here goes through the same `Plan` call, policy, and staging as a
catalog candidate, and stays an `ActionStage` rather than becoming permanent
because it was chosen from this screen. Two tests pin both.

## Red team

Three guards were each broken and the matching test confirmed to fail:
allowing every copy to be selected, swapping the policy for `AllowAll`, and
changing the action to purge.

**A comment claimed something the code did not do.** The merge report's
destinations were built with the constructor that folds details behind the
disclosure toggle, directly under a comment saying they were not hidden because
they are the point of a merge. The test asserting the report names where each
copy went is what caught it. A separate constructor now shows details expanded,
and the comment describes what the code does.

## Verification

Against the installed binary through a pseudo terminal, with three identical
files in Documents, Downloads, and Desktop, and the Documents copy backdated:

```
1 group(s) of identical files · 40.0KB reclaimable · compared 5 file(s)
  Merged 2 copy(s) into ~/Documents, nothing was deleted
      ~/Downloads/report.pdf is now ~/Documents/report copy.pdf
      ~/Desktop/report.pdf is now ~/Documents/report copy 2.pdf
```

The filesystem afterwards held all three copies in Documents under those
names, with Downloads and Desktop empty.

## Known limitations

The scan runs once when the flow starts. Merging or staging and then returning
to the group list shows the state as it was measured.

Opening a file uses Finder's reveal, which shows it in place rather than
previewing it. Quick Look would be closer to what comparing images wants.
