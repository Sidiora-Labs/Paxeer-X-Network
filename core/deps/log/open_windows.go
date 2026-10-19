//go:build windows

package paxlog

// O_NOFOLLOW is not available on Windows; symlink-following prevention
// is not applicable on this platform.
const noFollowFlag = 0
