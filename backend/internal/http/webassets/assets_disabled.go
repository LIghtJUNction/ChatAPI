//go:build !embed_frontend

package webassets

import "io/fs"

func FS() (fs.FS, bool) {
	return nil, false
}
