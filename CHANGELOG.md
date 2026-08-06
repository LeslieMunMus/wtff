# Changelog

Notable changes to wtff. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [semantic versioning](https://semver.org/spec/v2.0.0.html).

While the major version is zero the interface may change between minor
versions. Anything that changes what gets removed, or how reversible a removal
is, is called out under its own heading regardless of version.

## Unreleased

### Added

- `purge`, which permanently empties the places holding things already
  discarded, currently the Trash in the home directory and on every mounted
  volume. Nothing is staged, because nothing here was inferred.
- Permanent deletion of staged batches, from `wtff staged --purge <batch-id>`
  or from the shell's staged menu, gated behind typing a confirmation word.
- Refusal to uninstall applications that ship a system extension or a kernel
  extension, which covers drivers, network filters, VPN clients, and endpoint
  security agents without naming any vendor.
- A report, before an uninstall proceeds, of privileged helpers and launchd
  jobs that will survive it, since wtff runs unprivileged and cannot remove
  them.
- A live progress figure on the activity line during long scans.
- A draggable scrollbar in the interactive shell.

### Changed

- The shell is a scrolling transcript rather than a stack of screens. Running
  a command no longer replaces the view, and the prompt never moves.
- Directory size measurement is bounded in wall clock time, not only in entry
  count. A walk stalled inside a single filesystem call previously hung a plan
  indefinitely.

### Fixed

- Mouse wheel scrolling and the blinking cursor, both of which reached nothing
  at all and failed silently.
- Transcript history is capped, where it previously grew without limit.
