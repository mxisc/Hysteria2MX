//go:build !release_embed

package embeddedassets

import "io/fs"

func HasWeb() bool {
	return false
}

func Web() fs.FS {
	return nil
}
