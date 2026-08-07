# 0033: scoping process and memory management

Status: dropped. No code exists for this and none is planned.

This entry records what was investigated before any decision was made, so the
decision that follows rests on measured facts rather than on what a feature
request implied was possible.

## What was measured on a real machine

Darwin 25.6, Apple silicon, unprivileged shell.

**Available without root:**

- `ps -axo pid,ppid,%cpu,rss,user,comm`: process identity, parent, CPU
  percentage, resident memory, owning user, executable path.
- `launchctl list`: 515 jobs in the user domain, with labels and last exit
  status.
- Process ownership. Of 512 processes running as this user, 470 live under
  `/System` or `/usr/libexec`, meaning Apple's own. Roughly 42 are genuinely
  third party.

**Not available without root:**

- `powermetrics`. Its help text prints unprivileged, which is misleading;
  actually sampling refuses with "powermetrics must be invoked as the
  superuser." This is the tool that produces the per process energy figures
  Activity Monitor's Energy tab shows.
- `purge`. Returns "Unable to purge disk buffers: Operation not permitted."

## The two findings that reshape the request

### Energy impact cannot be measured, only approximated

The request was to find processes "draining the battery." wtff runs
unprivileged by design, and the only real per process energy data on macOS
requires root. What remains is CPU percentage and resident memory, which
correlate with power draw but are not the same thing: a process can hold a
large resident set and cost almost nothing in power, and a process can wake
the CPU frequently at low average CPU percentage and cost a great deal.

So a feature built on `ps` alone cannot honestly be called battery diagnosis.
It can honestly be called "what is using CPU and memory right now," which is
useful and is a different claim. Presenting the second as the first would be
the kind of asserted-but-unverified property this project's own rules exist to
prevent.

### Freeing memory is not a thing worth building

On this machine right now, system-wide memory free is 83 percent, pageouts are
156,392 against 14.3 million pageins. Memory pressure, not free memory, is the
figure that determines whether a Mac is struggling.

macOS deliberately uses otherwise idle RAM for caches and compressed pages.
Free RAM is unused RAM. The `purge` command, which drops disk buffers, needs
root and would in most cases make performance worse by discarding caches the
system would immediately have to rebuild. Cleaner tools that advertise "free
up memory" typically allocate a large block and release it, forcing the system
to evict useful cache, which measurably degrades performance while producing a
satisfying number in a progress bar.

Recommendation: wtff should not offer a memory freeing button. It could
honestly report memory pressure and the largest consumers, and let a person
quit an application themselves. That is the useful half of the request without
the theatre.

## Why this cannot reuse the deletion engine's safety model

Every safety guarantee wtff currently makes rests on one property: a removal
is staged first and reversible until explicitly purged. That property does not
exist for a running process.

- There is no staging. A signalled process is signalled.
- There is no undo. wtff would not know how to relaunch a process, with what
  arguments, in what environment, under what parent.
- The blast radius is immediate and can include unsaved work. A file removal
  costs disk space if wrong; a process kill can cost a person an unsaved
  document.

The top memory consumer on this machine during this investigation was Claude
itself at 746MB. A naive "kill the biggest memory consumer" heuristic would
have ended the session that was writing this document. That is not a
hypothetical illustration; it is what the measurement returned.

So this needs its own safety model, designed from scratch, in the way the
deletion engine was. It is not an extension of what exists.

## The open questions, for decision before any code

1. **What may wtff signal at all?** The 470 Apple processes and anything under
   `/System` are an obvious floor, mirroring the `com.apple.` exclusion the
   clean catalog already uses. Beyond that: only what a person selects by hand
   from a list, or may wtff ever recommend?

2. **Which signal?** `SIGTERM` lets an application save and exit cleanly and
   may be ignored. `SIGKILL` is immediate and loses unsaved work. A tool built
   on this project's stated values should probably never send `SIGKILL`, and
   should say so rather than offer it as a "force" option.

3. **Is this a separate command or part of the shell?** It shares nothing with
   the deletion engine, so it could reasonably be a separate binary or a
   clearly separated subsystem with its own rules directory and its own
   provenance requirements.

4. **Does it need a protection rule schema of its own?** The existing schema
   describes paths. Processes need different predicates: bundle identifier,
   executable path, parent, whether it is a login item, whether it owns a
   system extension. Reusing the path schema would be forcing a shape.

## Decision

Dropped, by the project manager, after the scoping above.

The reasoning is the toolkit's own standard: wtff carries only functional
features. What remained after removing the parts the data cannot support was
a list of processes with their CPU and memory, and a way to quit one. Activity
Monitor is installed on every Mac, does exactly that, and does it well. A
second copy inside wtff would be a feature that exists to look complete rather
than to do something, which is the one kind of feature this project does not
carry.

The reframing considered before it was dropped, kept here because it is the
shape any future attempt should take:

Reframed as process visibility only. When
built, it shows what is using CPU and memory and lets a person quit something
themselves with `SIGTERM`, which an application can catch in order to save
first. It will not send `SIGKILL`, will not offer a memory freeing button, and
will not describe anything as battery optimisation, because the data available
without root does not support that claim.

Requiring `sudo` to get real energy figures was considered and rejected: it
would introduce privilege escalation into a toolkit that deliberately has
none, which is a larger architectural change than the feature is worth.

## Recommendation

Build the disk usage browser and the duplicate finder first. Both extend the
existing safety model rather than needing a new one, and both deliver visible
value against the same staging guarantees already tested.

Return to this with a dedicated design pass. When it is built, scope it as
"process visibility and manual quitting" rather than battery optimisation,
because the second is a claim the available data cannot support.
