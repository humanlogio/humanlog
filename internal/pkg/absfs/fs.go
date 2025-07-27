package absfs

import (
	"io/fs"
	"os"
	"strings"
)

type AbsFS struct {
	root string
	fs.FS
}

func New(root string) *AbsFS {
	return &AbsFS{root: root, FS: os.DirFS(root)}
}

func (a *AbsFS) Open(name string) (fs.File, error) {
	name = strings.TrimPrefix(name, a.root)
	return a.FS.Open(name)
}
