package memfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6"
)

type storage struct {
	files    map[string]*file
	children map[string]map[string]*file

	mf sync.RWMutex
	mc sync.RWMutex
}

func newStorage() *storage {
	return &storage{
		files:    make(map[string]*file, 0),
		children: make(map[string]map[string]*file, 0),
	}
}

func (s *storage) Has(path string) bool {
	_, ok := s.get(path)
	return ok
}

func (s *storage) New(path string, mode fs.FileMode, flag int) (*file, error) {
	path = clean(path)
	if f, ok := s.get(path); ok {
		if !f.mode.IsDir() {
			return nil, fmt.Errorf("file already exists %q", path)
		}

		return nil, nil
	}

	name := filepath.Base(path)
	f := &file{
		name:    name,
		content: &content{name: name},
		mode:    mode,
		flag:    flag,
		modTime: time.Now(),
	}

	s.mf.Lock()
	s.files[path] = f
	s.mf.Unlock()

	err := s.createParent(path, mode, f)
	if err != nil {
		return nil, fmt.Errorf("failed to create parent: %w", err)
	}

	return f, nil
}

func (s *storage) createParent(path string, mode fs.FileMode, f *file) error {
	base := filepath.Dir(path)
	base = clean(base)
	if f.Name() == string(separator) {
		return nil
	}

	if _, err := s.New(base, mode.Perm()|os.ModeDir, 0); err != nil {
		return err
	}

	s.mc.Lock()
	if _, ok := s.children[base]; !ok {
		s.children[base] = make(map[string]*file, 0)
	}

	s.children[base][f.Name()] = f
	s.mc.Unlock()

	return nil
}

func (s *storage) Children(path string) []*file {
	path = clean(path)

	s.mc.RLock()
	l := make([]*file, 0, len(s.children))
	for _, f := range s.children[path] {
		l = append(l, f)
	}
	s.mc.RUnlock()

	return l
}

func (s *storage) MustGet(path string) *file {
	f, ok := s.get(path)
	if !ok {
		panic(fmt.Errorf("couldn't find %q", path))
	}

	return f
}

func (s *storage) Get(path string) (*file, bool) {
	return s.get(path)
}

func (s *storage) get(path string) (*file, bool) {
	path = clean(path)

	s.mf.RLock()
	file, ok := s.files[path]
	s.mf.RUnlock()
	if !ok {
		return nil, false
	}

	return file, ok
}

func (s *storage) Rename(from, to string) error {
	from = clean(from)
	to = clean(to)

	if from == "/" || from == "." {
		return billy.ErrBaseDirCannotBeRenamed
	}

	if !s.Has(from) {
		return os.ErrNotExist
	}

	move := [][2]string{{from, to}}
	s.mf.RLock()
	for pathFrom := range s.files {
		if pathFrom == from || !strings.HasPrefix(pathFrom, from) {
			continue
		}

		rel, _ := filepath.Rel(from, pathFrom)
		pathTo := filepath.Join(to, rel)

		move = append(move, [2]string{pathFrom, pathTo})
	}
	s.mf.RUnlock()

	for _, ops := range move {
		from := ops[0]
		to := ops[1]

		if err := s.move(from, to); err != nil {
			return err
		}
	}

	return nil
}

func (s *storage) move(from, to string) error {
	s.mf.Lock()
	s.files[to] = s.files[from]
	s.files[to].name = filepath.Base(to)
	file := s.files[to]
	s.mf.Unlock()

	s.mc.Lock()
	s.children[to] = s.children[from]
	s.mc.Unlock()

	defer func() {
		s.mf.Lock()
		delete(s.files, from)
		s.mf.Unlock()

		s.mc.Lock()
		delete(s.children, from)
		delete(s.children[filepath.Dir(from)], filepath.Base(from))
		s.mc.Unlock()
	}()

	return s.createParent(to, 0644, file)
}

func (s *storage) Remove(path string) error {
	path = clean(path)
	if path == "/" || path == "." {
		return billy.ErrBaseDirCannotBeRemoved
	}

	f, has := s.get(path)
	if !has {
		return os.ErrNotExist
	}

	if f.mode.IsDir() {
		s.mc.RLock()
		if len(s.children[path]) != 0 {
			s.mc.RUnlock()
			return fmt.Errorf("dir: %s contains files", path)
		}
		s.mc.RUnlock()
	}

	base, file := filepath.Split(path)
	base = filepath.Clean(base)

	s.mf.Lock()
	delete(s.files, path)
	s.mf.Unlock()

	s.mc.Lock()
	delete(s.children[base], file)
	s.mc.Unlock()
	return nil
}

func clean(path string) string {
	return filepath.Clean(filepath.FromSlash(path))
}
