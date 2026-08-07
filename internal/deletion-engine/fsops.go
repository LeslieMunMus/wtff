package deletionengine

import (
	pathvalidation "github.com/lesliemunmus/wtff/internal/path-validation"
	"golang.org/x/sys/unix"
)

// statLeaf reports the size of a non directory target.
//
// It goes through the pinned parent descriptor rather than the path, so the
// measurement describes the object validation resolved rather than whatever
// currently answers to that name.
func statLeaf(resolved *pathvalidation.Resolved) (int64, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(resolved.ParentFD(), resolved.LeafName(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, err
	}
	return st.Size, nil
}
