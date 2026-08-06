# 0020: refusing to uninstall drivers and security agents

Status: done.

Built on Opus, agreed in advance, because this is the guard between a person
and removing their own security software or a storage driver from under a
running system.

## Not a vendor list

The gap was described as a missing vendor protection list: names of VPN
clients, endpoint security agents, and kernel extension owners. A named list
was considered and rejected.

A list only protects software someone thought to name. It goes stale as
products are renamed and vendors are acquired. It silently fails for the small
vendor nobody has heard of, whose disk driver is no less load bearing for being
obscure. And a curated list of names is exactly the kind of compilation this
project's licensing position says to re-derive rather than assemble.

What makes an application dangerous to remove is a property it has, not a brand
it carries, and macOS makes that property visible in the filesystem. An
extension has to sit where the loader looks for it. A privileged helper has to
be registered where launchd reads it. So the check is structural, derived from
Apple's own documented mechanisms and confirmed against a machine running
Darwin 25.6.

## Two tiers

**Refused outright:** an application shipping a system extension or a kernel
extension, or whose vendor namespace owns a kernel extension installed system
wide. Since macOS 10.15 an Endpoint Security client must ship as a system
extension, so that one directory check covers network filters, VPN clients, and
security agents without naming any of them. The refusal names the paths it
found, so the claim can be checked rather than trusted, and points at the
vendor's own uninstaller, which knows how to unload an extension before
deleting what provides it.

**Reported, not refused:** privileged helpers and launchd jobs. wtff runs
unprivileged by design and cannot remove these, so both front ends now say what
will survive the uninstall while the choice is still open, rather than leaving
a person to discover a root daemon still running afterwards.

Deliberately no code signing entitlement inspection, which would name Endpoint
Security clients directly. It needs an external tool whose absence would
silently weaken the check, and the system extension directory already covers
that category using nothing but a directory listing.

## What the real machine taught, twice

The first implementation was run against all 35 applications installed here
before any of it was believed, and it was wrong in two ways that no unit test
would have shown.

**It reported shared components as orphans.** One Microsoft AutoUpdate helper
served Word, Excel, Teams, and OneDrive; one Google updater served Chrome and
Gemini. Uninstalling Word would have announced an orphaned root daemon that
three installed applications still depended on. A warning that cries wolf is
one people learn to click past, so leftovers are now suppressed when another
installed application shares the vendor namespace. Sharing never suppresses the
refusal tier: a driver is a driver whether or not the vendor ships other
applications.

**It attributed unrelated software.** Matching launchd jobs by vendor namespace
credited MySQL Workbench with an Oracle database daemon and a Java updater, both
installed separately and neither its doing. Jobs are now matched only when
their program path is inside the bundle about to be deleted, which is a fact,
rather than sharing a prefix with it, which is a guess.

After both corrections the whole machine reports clean: nothing refused,
nothing flagged, 35 applications still uninstallable.

## Red team

**A check that never fires proves nothing.** With no drivers installed here,
every structural check returned empty, which is indistinguishable from a check
that does not work. The system directories were changed from constants to
variables so tests can point them at a fixture tree, and the suite now builds
real bundles carrying a system extension, a kernel extension, an in bundle
launchd job, and an installed privileged helper, and requires each to be found.

**A test that chose a bad example.** Containment was asserted with FooBar.app
against Foo.app, which does not collide under a plain string prefix, so the test
passed with the component comparison removed. The paths that actually collide
extend the bundle name rather than a component of it, such as the Foo.app.old
copies installers leave behind, and the test uses those now.

Six guards were each removed in turn and the matching test confirmed to fail:
the system extension check, the kernel extension check, the wiring into
IsProtectedApp, the two component vendor prefix, sharing suppression, and
component wise containment.

## Verification

Full suite green. The detection was run against every application installed on
the development machine before and after each correction, which is where both
real defects were found.

## Known limitations

Configuration profiles and MDM management are not consulted, since reading them
needs privileges wtff does not take. A managed application is currently treated
as ordinary.

Vendor namespace comparison uses the first two identifier components, which
groups a vendor's whole product family. That is right for suppressing shared
updaters and wrong for a vendor whose products are genuinely independent, where
it will under report leftovers rather than over report them.
