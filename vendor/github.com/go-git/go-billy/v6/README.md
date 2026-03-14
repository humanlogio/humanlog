# go-billy [![GoDoc](https://godoc.org/gopkg.in/go-git/go-billy.v6?status.svg)](https://pkg.go.dev/github.com/go-git/go-billy/v6) [![Test](https://github.com/go-git/go-billy/workflows/Test/badge.svg)](https://github.com/go-git/go-billy/actions?query=workflow%3ATest) [![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/go-git/go-billy/badge)](https://scorecard.dev/viewer/?uri=github.com/go-git/go-billy)

The missing interface filesystem abstraction for Go.
Billy implements an interface based on the `os` standard library, allowing to develop applications without dependency on the underlying storage. Makes it virtually free to implement mocks and testing over filesystem operations.

Billy was born as part of [go-git/go-git](https://github.com/go-git/go-git) project.

## Installation

```go
import "github.com/go-git/go-billy/v6" // with go modules enabled (GO111MODULE=on or outside GOPATH)
import "github.com/go-git/go-billy" // with go modules disabled
```

## Usage

Billy exposes filesystems using the
[`Filesystem` interface](https://pkg.go.dev/github.com/go-git/go-billy/v6?tab=doc#Filesystem).
Each filesystem implementation gives you a `New` method, whose arguments depend on
the implementation itself, that returns a new `Filesystem`.

The following example caches in memory all readable files in a directory from any
billy's filesystem implementation.

```go
func LoadToMemory(origin billy.Filesystem, path string) (*memory.Memory, error) {
	memory := memory.New()

	files, err := origin.ReadDir("/")
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		src, err := origin.Open(file.Name())
		if err != nil {
			return nil, err
		}

		dst, err := memory.Create(file.Name())
		if err != nil {
			return nil, err
		}

		if _, err = io.Copy(dst, src); err != nil {
			return nil, err
		}

		if err := dst.Close(); err != nil {
			return nil, err
		}

		if err := src.Close(); err != nil {
			return nil, err
		}
	}

	return memory, nil
}
```

## Why billy?

The library billy deals with storage systems and Billy is the name of a well-known, IKEA
bookcase. That's it.

## License

Apache License Version 2.0, see [LICENSE](LICENSE)
