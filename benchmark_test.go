package humanlog

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	typesv1 "github.com/humanlogio/api/go/types/v1"
	"github.com/stretchr/testify/require"
)

type NopSink struct{}

func (*NopSink) Receive(ctx context.Context, ev *typesv1.Log) error {
	return nil
}

func (*NopSink) Close(ctx context.Context) error {
	return nil
}

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

		testCase := dir
		b.Run(testCase, func(bb *testing.B) {

			p := filepath.Join(dir, fileName)
			f, err := os.Open(p)
			require.NoError(bb, err)
			defer f.Close()

			gzipReader, err := gzip.NewReader(f)
			require.NoError(bb, err)

			src := bytes.NewBuffer(nil)
			_, err = io.Copy(src, gzipReader)
			require.NoError(bb, err)

			sink := &NopSink{}
			opt := DefaultOptions()

			bb.SetBytes(int64(src.Len()))
			for range bb.N {
				copy := bytes.NewBuffer(src.Bytes())
				bb.StartTimer()
				_ = Scan(ctx, copy, sink, opt)
				bb.StopTimer()
			}
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
