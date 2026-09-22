// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !unix

package main

import "io/fs"

// ownerUID has no meaning on platforms without uids. --fix-permissions is a container concern and
// the images are Linux, so reporting "unknown" and skipping the chown is the whole behaviour.
func ownerUID(fs.FileInfo) (int, bool) { return 0, false }
