package path

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

var (
	homeDirAlias       string
	homeDir            string
	errHomeDirNotFound error
)

func init() {
	homeDirAlias = HomeDir
	homeDir, errHomeDirNotFound = os.UserHomeDir()
}

type Path struct {
	mapping map[string]string
	elems   []string
}

func New(elems ...string) (*Path, error) {
	if errHomeDirNotFound != nil {
		return nil, fmt.Errorf("home-directory must be set on the system: %v", errHomeDirNotFound)
	}
	return &Path{
		mapping: map[string]string{
			homeDirAlias: homeDir,
		},
		elems: elems,
	}, nil
}

func Parse(path string) (*Path, error) {
	if errHomeDirNotFound != nil {
		return nil, fmt.Errorf("home-directory must be set on the system: %v", errHomeDirNotFound)
	}
	elems := filepath.SplitList(path)
	if len(elems) == 0 {
		return nil, fmt.Errorf("parsing path %q: %v", path, fmt.Errorf("given path is not valid"))
	}
	return &Path{
		mapping: map[string]string{
			homeDirAlias: homeDir,
		},
		elems: filepath.SplitList(path),
	}, nil
}

func (p *Path) Append(e ...string) *Path {
	elems := slices.Clone(p.elems)
	elems = append(elems, e...)
	return &Path{
		mapping: maps.Clone(p.mapping),
		elems:   elems,
	}
}

// Returns path string via filepath.Join(p.elems...)
func (p *Path) String() string {
	return filepath.Join(p.elems...)
}

// Returns an path string via filepath.Join(p.elems)
// Converts each elems to theirs original string if mapping[elem] is exist
func (p *Path) Expand() string {
	elems := make([]string, 0, len(p.elems))
	for _, e := range p.elems {
		elems = append(elems, p.original(e))
	}
	return filepath.Join(elems...)
}

func (p *Path) original(e string) string {
	if o, exists := p.mapping[e]; exists {
		return o
	}
	return e
}
