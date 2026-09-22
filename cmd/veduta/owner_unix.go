// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build unix

package main

import (
	"io/fs"
	"syscall"
)

// ownerUID reports the uid owning a file, and whether it could be determined. `veduta init
// --fix-permissions` needs it to hand a newly written veduta.yaml back to whoever owns the
// mounted directory: the file is created by root, and a root-owned configuration in a bind
// mount is one the operator cannot edit without sudo - the opposite of the intent.
func ownerUID(info fs.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}
