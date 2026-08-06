# 0023: cutting releases, and distributing without notarization

Status: done.

## The release

`make dist` builds for Apple silicon and Intel, joins them with `lipo`,
archives the result, and writes a SHA-256 checksum. It depends on `check`, so
a release cannot be cut from a tree that does not pass its own tests.

`lipo` is spelled `/usr/bin/lipo` rather than found on `PATH`. Several
toolchains ship their own, and this machine resolves the bare name to
Anaconda's copy. Both happen to work here, but a release that silently picks up
whichever one is first on `PATH` is a release that will one day produce an
archive failing on half the machines it is offered to.

Tagging publishes. A `v*` tag runs a workflow that rebuilds from a clean full
depth checkout, asserts the binary reports the tag it was built from, and opens
a draft release with the archive and checksums attached. Full depth matters
because the version is stamped from `git describe`, which has nothing to
describe in the default shallow clone. The draft is deliberate: a chance to
read the notes before anyone else does.

## Homebrew, building from source

The formula compiles on the user's machine rather than downloading a prebuilt
binary, and that is the point rather than a shortcut.

A binary downloaded from the internet carries a quarantine attribute, and
Gatekeeper refuses to open it until it is either notarized, which needs a paid
Apple Developer account, or cleared by hand with `xattr -d`. The second option
is the one that looks free and is not: telling people to strip quarantine from
a downloaded cleanup tool teaches them to disarm the exact protection that
would have caught a genuinely malicious download. Compiling locally sidesteps
the question honestly, because nothing arrives already built and so nothing is
quarantined.

The formula's test runs `wtff version`, the only command that needs neither a
terminal nor the filesystem, and a `clean --dry-run` against an empty home,
which proves the embedded rules and catalog actually load. A build broken in
that way would pass a version check and fail the moment anyone used it.

## Red team

**A rehearsal that rehearsed the wrong thing.** The first release rehearsal
stashed the working tree to get a clean checkout, which also stashed the
Makefile containing the `dist` target being tested. Make then found the
existing `dist/` directory, had no rule for it, and reported "nothing to be
done", so the rehearsal reported success while running the previous build's
stale artifacts. Rerun after committing, against a real tag.

**Two assertions checked rather than assumed.** The workflow requires
`git describe --tags --always --dirty` on an exact tag to equal that tag, which
is the difference between a release pipeline that works and one that fails on
every tag forever. Confirmed by tagging and reading the output. The formula's
two test commands were likewise run against the real binary rather than written
from memory.

## Verification

A full rehearsal against the tag `v0.1.0-rehearsal`: `make dist` produced an
archive whose binary reports `x86_64 arm64`, reports `v0.1.0-rehearsal` as its
version, satisfies its own checksum file under `shasum -c`, and runs a real
`purge --dry-run` against an isolated home. The tag and artifacts were then
removed.

## Known limitations

The repository has no remote, so neither workflow has ever run on GitHub. Their
first push is their first real execution, and the release workflow in
particular has only been exercised through the local `make dist` it wraps.

The tap does not exist yet, and the formula's `url` and `sha256` are
placeholders until there is a tag to point at.

Building from source means installing needs a Go toolchain, which Homebrew
pulls in as a build dependency. That is a heavier install than a downloaded
binary, and the trade is deliberate.
