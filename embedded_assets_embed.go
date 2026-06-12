//go:build release_embed

package embeddedassets

import (
	"embed"
	"io/fs"
)

// webFiles stores the built frontend assets inside the release panel binary.
//
//go:embed public
var webFiles embed.FS

func HasWeb() bool {
	return true
}

func Web() fs.FS {
	subtree, err := fs.Sub(webFiles, "public")
	if err != nil {
		panic(err)
	}
	return subtree
}

