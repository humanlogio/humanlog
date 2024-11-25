package humanlog

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/humanlogio/humanlog/pkg/sink/bufsink"
	"github.com/stretchr/testify/require"
)

func BenchmarkHarness(b *testing.B) {
	ctx := context.Background()
	root := "test/benchmark"
	des, err := os.ReadDir(root)
	if err != nil {
		b.Fatal(err)
	}

	for _, de := range des {
		if !de.IsDir() {
			continue
		}

		dir := filepath.Join(root, de.Name())
		fileName, err := findfirstMatchedFileName(dir, "*.gz")
		require.NoError(b, err)

		b.ResetTimer()
		testCase := dir
		b.Run(testCase, func(bb *testing.B) {
			path := filepath.Join(dir, fileName)
			f, err := os.Open(path)
			require.NoError(bb, err)
			defer f.Close()

			gzipReader, err := gzip.NewReader(f)
			require.NoError(bb, err)

			buf := make([]byte, 0, 100*1024) // set initial capacity to 100kb
			err = loadAll(&buf, gzipReader)

			src := bytes.NewBuffer(buf)
			require.NoError(bb, err)

			sink := bufsink.NewSizedBufferedSink(len(buf), nil) // size must be greater than src size
			opt := DefaultOptions()

			bb.SetBytes(int64(src.Len()))
			bb.StartTimer()
			err = Scan(ctx, src, sink, opt)
			bb.StopTimer()

			require.NoError(bb, err)
		})
	}
}

func findfirstMatchedFileName(dirPath string, pattern string) (string, error) {
	firstMatched := ""
	walkError := filepath.Walk(dirPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		fileName := filepath.Base(path)
		match, err := filepath.Match(pattern, fileName)
		if err != nil {
			return err
		}
		if match {
			firstMatched = fileName
			return filepath.SkipAll
		}
		return nil
	})
	return firstMatched, walkError
}

func loadAll(dest *[]byte, r io.Reader) error {
	for {
		temp := make([]byte, 1024)
		n, err := r.Read(temp)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		*dest = append(*dest, temp[:n]...)
	}
	return nil
}
