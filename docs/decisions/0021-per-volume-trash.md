# 0021: the Trash on other volumes

Status: done.

Anything discarded while working on an external drive was invisible to wtff.
macOS does not move it to the home Trash, because a file on another volume
cannot be moved to the startup disk without copying it. Each volume keeps its
own Trash instead, at `.Trashes/<uid>`, and it occupies space on that drive
until something empties it.

## Purge only, and why

The entry is marked `purge_only`, a new field, so `clean` does not offer it.

Staging is a rename into wtff's staging area, and a rename cannot cross a
filesystem boundary. The deletion engine already refuses that outright rather
than falling back to a copy, because a copy that silently loses extended
attributes or hard link structure would make undo a lie. Offering a cross
volume entry under `clean` would therefore mean every run on a machine with an
external drive reports failures it was always going to report. The loader
refuses `purge_only` on an entry that is not also `purgeable`, since such an
entry would be reachable by no command at all.

The home Trash is unaffected and remains stageable under `clean`, which a test
pins.

## Only this user, and only real volumes

Three guards, each derived from what the machine actually looks like rather
than from what seemed likely.

**Only this user's subdirectory.** `.Trashes` holds one directory per user who
has discarded something on that volume. On a shared external drive the others
belong to people whose data this tool has no business touching.

**Mount points that are links are skipped.** macOS ships exactly this trap:
`/Volumes/Macintosh HD` is a symbolic link to `/`. Following it would treat the
startup disk as an external drive.

**Filesystem device identity decides, not the name.** A firmlink, a bind mount,
or anything else contriving to make the startup disk appear under another name
still reports the same device number. This catches all of them at once,
including the symlink above, which is why the link check is belt and braces
rather than the real defense. If the startup disk's own device cannot be read,
discovery refuses to enumerate at all rather than guess, since nothing can be
told apart from a thing you cannot identify.

## Red team

**A check that never fires proves nothing.** Nothing was mounted on the
development machine but the symbolic link, so every code path returned empty,
which is indistinguishable from a check that does not work. The locations
became variables so tests can point them at a fixture tree.

**The device lookup had to be indirected too, for a reason worth recording.**
APFS firmlinks make `/` and `/System/Volumes/Data` report the same device
number on this system, confirmed by inspection. That is exactly the behavior
the guard depends on, and it also means no two paths in a temporary directory
can be made to look like different volumes. The lookup is injected for the
discovery tests and has its own test against real paths.

**A mutation that did not compile.** The first attempt to prove the user id
scoping mattered left an unused variable, so the suite failed to build and the
result was mistaken for a pass. Rerun so it compiled, four tests failed, which
is the real evidence.

Four guards were each removed in turn and the matching test confirmed to fail:
user id scoping, the symbolic link check, device identity, and the refusal when
the startup disk is unidentifiable.

## Verification

Beyond the fixtures, this was proven against a real mounted volume. A twenty
megabyte APFS disk image was created and attached, which appeared at
`/Volumes/wtfftest` reporting device 16777239 against the startup disk's
16777232. Trash was planted for two users, 501 and 502. `wtff purge --dry-run`
listed the home Trash and both of this user's items on the external volume and
nothing belonging to 502; `wtff clean --dry-run` listed only the home Trash.
A real `wtff purge --yes` removed all three, and 502's file was still there
afterwards. The image was detached and deleted, and `/Volumes` is back to
holding only the symbolic link.

## Known limitations

Network volumes are treated like any other. A slow or unresponsive mount will
be walked like a local disk, bounded only by the measurement deadline from
decision 0017.

Only `/Volumes` is scanned. A volume mounted somewhere else by hand will not be
seen.
