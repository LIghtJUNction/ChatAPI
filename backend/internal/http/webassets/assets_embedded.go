//go:build embed_frontend

package webassets

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

func FS() (fs.FS, bool) {
	assets, err := fs.Sub(embedded, "dist")
	return assets, err == nil
}
