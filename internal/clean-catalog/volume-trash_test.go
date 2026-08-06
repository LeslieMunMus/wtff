package cleancatalog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// redirectVolumes points discovery at a fixture tree for one test.
//
// The real /Volumes cannot be populated without mounting something, so a test
// unable to redirect it could only observe whatever happens to be plugged in.
// On the machine this was written against that is one symbolic link and
// nothing else, which is a check that never fires and therefore proves
// nothing about whether it works.
func redirectVolumes(t *testing.T, uid int) (volumes, boot string) {
	t.Helper()
	root := t.TempDir()
	volumes = filepath.Join(root, "Volumes")
	boot = filepath.Join(root, "boot")
	for _, dir := range []string{volumes, boot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	previousVolumes, previousBoot := volumesDir, bootRootDir
	previousUID, previousDevice := currentUID, deviceOf
	t.Cleanup(func() {
		volumesDir, bootRootDir = previousVolumes, previousBoot
		currentUID, deviceOf = previousUID, previousDevice
	})
	volumesDir, bootRootDir = volumes, boot
	currentUID = func() int { return uid }

	// Every fixture path is presented as its own volume unless a test says
	// otherwise, since the real device numbers here are all identical.
	deviceOf = func(path string) (uint64, error) {
		if _, err := os.Stat(path); err != nil {
			return 0, err
		}
		if path == boot {
			return 1, nil
		}
		return 2, nil
	}
	return volumes, boot
}

// makeVolumeTrash creates a volume holding one discarded item for a user.
func makeVolumeTrash(t *testing.T, volumes, name string, uid int, items ...string) string {
	t.Helper()
	trash := filepath.Join(volumes, name, trashesDirName, strconv.Itoa(uid))
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	for _, item := range items {
		if err := os.WriteFile(filepath.Join(trash, item), []byte("discarded"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return trash
}

func volumeTrashEntry() Entry {
	return Entry{
		ID:     "volume-trash-contents",
		Kind:   KindVolumeTrash,
		Path:   "/Volumes",
		Reason: "items discarded on another volume",
	}
}

func TestVolumeTrashIsDiscovered(t *testing.T) {
	volumes, _ := redirectVolumes(t, 501)
	trash := makeVolumeTrash(t, volumes, "Backup Drive", 501, "old-file.txt", "old-folder")

	candidates, _ := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 2 {
		t.Fatalf("expected both discarded items, got %d: %+v", len(candidates), candidates)
	}
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.Path, trash) {
			t.Fatalf("candidate %q is outside the volume trash", candidate.Path)
		}
	}
	// The volume name belongs in the reason, since the same filename can sit
	// in the trash of several drives at once.
	if !strings.Contains(candidates[0].Reason, "Backup Drive") {
		t.Fatalf("the reason should name the volume, got %q", candidates[0].Reason)
	}
}

// Another user's discarded files are theirs. On a shared external drive this
// is the difference between emptying your trash and emptying someone else's.
func TestAnotherUsersVolumeTrashIsNeverTouched(t *testing.T) {
	volumes, _ := redirectVolumes(t, 501)
	makeVolumeTrash(t, volumes, "Shared Drive", 502, "their-file.txt")

	candidates, _ := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 0 {
		t.Fatalf("another user's trash must not be discovered, got %+v", candidates)
	}
}

func TestOnlyThisUsersTrashIsTakenFromASharedVolume(t *testing.T) {
	volumes, _ := redirectVolumes(t, 501)
	makeVolumeTrash(t, volumes, "Shared Drive", 501, "mine.txt")
	makeVolumeTrash(t, volumes, "Shared Drive", 502, "theirs.txt")

	candidates, _ := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 1 {
		t.Fatalf("expected only this user's item, got %+v", candidates)
	}
	if !strings.HasSuffix(candidates[0].Path, "mine.txt") {
		t.Fatalf("took the wrong user's file: %s", candidates[0].Path)
	}
}

// macOS ships /Volumes/Macintosh HD as a symbolic link to the startup disk.
// Following it would treat the whole system as an external drive.
func TestSymlinkedMountPointIsSkipped(t *testing.T) {
	volumes, boot := redirectVolumes(t, 501)

	// A real volume with real trash, reached through a link rather than a mount.
	target := t.TempDir()
	trash := filepath.Join(target, trashesDirName, "501")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trash, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(volumes, "Macintosh HD")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_ = boot

	candidates, skips := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 0 {
		t.Fatalf("a symlinked mount point must be skipped, got %+v", candidates)
	}
	if len(skips) == 0 || !strings.Contains(skips[0].Reason, "symbolic link") {
		t.Fatalf("the skip should say why, got %+v", skips)
	}
}

// The identity check is what actually decides, so a mount point resolving to
// the startup disk is skipped whatever it is called and however it got there.
func TestMountPointOnTheStartupDiskIsSkipped(t *testing.T) {
	volumes, boot := redirectVolumes(t, 501)

	makeVolumeTrash(t, volumes, "Not Really A Volume", 501, "file.txt")

	// Presented as resolving to the same device as the startup disk, which is
	// what a firmlink, a bind mount, or a link followed to its target all look
	// like from here.
	deviceOf = func(path string) (uint64, error) {
		if _, err := os.Stat(path); err != nil {
			return 0, err
		}
		return 1, nil
	}
	_ = boot

	candidates, skips := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 0 {
		t.Fatalf("a mount point on the startup disk must be skipped, got %+v", candidates)
	}
	var explained bool
	for _, skip := range skips {
		if strings.Contains(skip.Reason, "startup disk") {
			explained = true
		}
	}
	if !explained {
		t.Fatalf("the skip should say it is the startup disk, got %+v", skips)
	}
}

// A volume with no trash for this user is the ordinary case and must not be
// reported as something worth explaining.
func TestVolumeWithoutTrashIsQuietlySkipped(t *testing.T) {
	volumes, _ := redirectVolumes(t, 501)
	if err := os.MkdirAll(filepath.Join(volumes, "Empty Drive"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	candidates, skips := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 0 {
		t.Fatalf("expected nothing, got %+v", candidates)
	}
	for _, skip := range skips {
		if !skip.CategoryAbsent {
			t.Fatalf("an absent trash should be a quiet skip, got %+v", skip)
		}
	}
}

func TestAbsentVolumesDirectoryIsHandled(t *testing.T) {
	volumes, _ := redirectVolumes(t, 501)
	if err := os.RemoveAll(volumes); err != nil {
		t.Fatalf("setup: %v", err)
	}

	candidates, skips := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 0 {
		t.Fatalf("expected nothing, got %+v", candidates)
	}
	if len(skips) != 1 || !skips[0].CategoryAbsent {
		t.Fatalf("expected one quiet skip, got %+v", skips)
	}
}

// If the startup disk cannot be identified, nothing can be told apart from it,
// and enumerating anyway would risk treating the system as an external drive.
func TestUnidentifiableStartupDiskRefusesToEnumerate(t *testing.T) {
	volumes, boot := redirectVolumes(t, 501)
	makeVolumeTrash(t, volumes, "Some Drive", 501, "file.txt")
	if err := os.RemoveAll(boot); err != nil {
		t.Fatalf("setup: %v", err)
	}

	candidates, skips := discoverVolumeTrash(volumeTrashEntry())
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates when the startup disk is unknown, got %+v", candidates)
	}
	if len(skips) != 1 || skips[0].CategoryAbsent {
		t.Fatalf("this is a real problem worth reporting, got %+v", skips)
	}
}

// Discover must route this kind through the volume walker rather than treating
// the path as a directory to enumerate, or it would list mount points as
// candidates and propose deleting whole drives.
func TestDiscoverRoutesTheVolumeTrashKind(t *testing.T) {
	volumes, _ := redirectVolumes(t, 501)
	makeVolumeTrash(t, volumes, "Backup Drive", 501, "old-file.txt")

	candidates, _ := Discover([]Entry{volumeTrashEntry()}, t.TempDir())
	if len(candidates) != 1 {
		t.Fatalf("expected the discarded item, got %+v", candidates)
	}
	if !strings.HasSuffix(candidates[0].Path, "old-file.txt") {
		t.Fatalf("expected the item, got %s", candidates[0].Path)
	}
	if strings.HasSuffix(candidates[0].Path, "Backup Drive") {
		t.Fatal("discovery proposed removing an entire volume")
	}
}

// The injected device lookup means the discovery tests above never exercise
// the real one, so it gets its own test against real paths.
func TestRealDeviceLookup(t *testing.T) {
	root, err := realDeviceOf("/")
	if err != nil {
		t.Fatalf("cannot read the startup disk's device: %v", err)
	}
	if root == 0 {
		t.Fatal("the startup disk reported device zero")
	}

	// A path inside the same APFS container reports the same device, which is
	// the property the guard depends on: on this system /System/Volumes/Data
	// is a separate volume joined by a firmlink and still reports the startup
	// disk's device number, so anything reached through one is correctly
	// treated as the startup disk.
	if data, err := realDeviceOf("/System/Volumes/Data"); err == nil && data != root {
		t.Logf("data volume device %d differs from root %d on this system", data, root)
	}

	if _, err := realDeviceOf(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("a missing path should report an error, not a device")
	}
}
