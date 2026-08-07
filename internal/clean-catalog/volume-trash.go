package cleancatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	deletionengine "github.com/lesliemunmus/wtff/internal/deletion-engine"
)

// Overridable so tests can point discovery at a fixture tree. The real
// locations cannot be created without mounting a volume, so a test unable to
// redirect them could only observe whatever happens to be plugged in, which on
// a machine with nothing attached is a check that never runs.
var (
	volumesDir  = "/Volumes"
	bootRootDir = "/"
	currentUID  = os.Getuid

	// deviceOf is indirected so tests can present two paths as living on
	// different volumes. They cannot do that for real without mounting one,
	// and on this machine they could not do it at all: APFS firmlinks make the
	// startup disk and its data volume report the same device number, which is
	// correct for the guard and useless for building a fixture.
	deviceOf = realDeviceOf
)

// trashesDirName is where macOS keeps per user trash on a volume that is not
// the boot volume. Each user gets a subdirectory named for their numeric user
// id, inside a directory owned by root.
const trashesDirName = ".Trashes"

// discoverVolumeTrash enumerates the current user's trash on mounted volumes
// other than the boot volume.
//
// Only this user's subdirectory is ever considered. The parent holds one
// directory per user who has discarded something on that volume, and the
// others belong to people whose data this tool has no business touching, on a
// shared external drive most of all.
func discoverVolumeTrash(entry Entry) ([]deletionengine.Candidate, []Skip) {
	mounts, err := os.ReadDir(volumesDir)
	if err != nil {
		return nil, []Skip{{
			EntryID: entry.ID, Path: volumesDir,
			Reason: "no mounted volumes directory on this machine", CategoryAbsent: true,
		}}
	}

	bootDevice, bootErr := deviceOf(bootRootDir)
	if bootErr != nil {
		// Without the boot device there is no way to tell a real volume from a
		// link back to the startup disk, and guessing wrong means treating the
		// whole system as an external drive. Refusing to enumerate is the
		// conservative failure.
		return nil, []Skip{{
			EntryID: entry.ID, Path: volumesDir,
			Reason: "cannot identify the startup disk, so no volume can be told apart from it",
		}}
	}

	uid := strconv.Itoa(currentUID())
	var candidates []deletionengine.Candidate
	var skips []Skip

	for _, mount := range mounts {
		volumePath := filepath.Join(volumesDir, mount.Name())

		// A mount point that is a link is not a volume. macOS ships exactly
		// this: /Volumes/Macintosh HD is a symlink to /, so following it would
		// treat the startup disk as an external drive and enumerate its root
		// trash. Confirmed present on Darwin 25.6.
		if mount.Type()&os.ModeSymlink != 0 {
			skips = append(skips, Skip{
				EntryID: entry.ID, Path: volumePath,
				Reason: "mount point is a symbolic link rather than a volume", CategoryAbsent: true,
			})
			continue
		}

		// The identity check, not the name or the link type, is what actually
		// decides. A firmlink, a bind mount, or anything else contriving to
		// make the startup disk appear under another name still reports the
		// same device number, and this catches all of them at once.
		device, err := deviceOf(volumePath)
		if err != nil {
			skips = append(skips, Skip{
				EntryID: entry.ID, Path: volumePath,
				Reason: "cannot inspect this mount point", CategoryAbsent: true,
			})
			continue
		}
		if device == bootDevice {
			skips = append(skips, Skip{
				EntryID: entry.ID, Path: volumePath,
				Reason:         "same volume as the startup disk, whose trash is the home trash",
				CategoryAbsent: true,
			})
			continue
		}

		trashDir := filepath.Join(volumePath, trashesDirName, uid)
		items, err := os.ReadDir(trashDir)
		if err != nil {
			skips = append(skips, Skip{
				EntryID: entry.ID, Path: trashDir,
				Reason: "nothing discarded on this volume by this user", CategoryAbsent: true,
			})
			continue
		}

		for _, item := range items {
			candidates = append(candidates, deletionengine.Candidate{
				Path:   filepath.Join(trashDir, item.Name()),
				RuleID: entry.ID,
				Reason: fmt.Sprintf("%s (discarded on the volume %s)", entry.Reason, mount.Name()),
			})
		}
	}

	return candidates, skips
}

// realDeviceOf reports the filesystem device a path lives on, following links
// so that what is measured is the thing a mount point actually resolves to.
func realDeviceOf(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	status, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("cannot read device identity for %s", path)
	}
	return uint64(status.Dev), nil
}
