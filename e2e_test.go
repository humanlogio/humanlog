package humanlog

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/humanlogio/humanlog/internal/pkg/config"
	"github.com/humanlogio/humanlog/pkg/sink/stdiosink"
)

func TestHarness(t *testing.T) {
	ctx := context.Background()
	root := "test/cases"
	des, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		t.Log(de.Name())
		if !de.IsDir() {
			continue
		}
		testCase := de.Name()
		t.Run(testCase, func(t *testing.T) {
			// testCase := testCase
			// if testCase != "00001-json" {
			// 	t.Skip()
			// }
			input, err := os.ReadFile(filepath.Join(root, de.Name(), "input"))
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}
			want, err := os.ReadFile(filepath.Join(root, de.Name(), "want"))
			if err != nil {
				t.Fatalf("reading expected output: %v", err)
			}
			cfgjson, err := os.ReadFile(filepath.Join(root, de.Name(), "config.json"))
			if err != nil {
				t.Fatalf("reading config: %v", err)
			}

			if runtime.GOOS == "windows" {
				input = replaceCRLF(input, "\r\n", "\n")
				want = replaceCRLF(want, "\r\n", "\n")
				cfgjson = replaceCRLF(cfgjson, "\r\n", "\n")
			}

			var cfg config.Config
			if err := json.Unmarshal(cfgjson, &cfg); err != nil {
				t.Fatalf("unmarshaling config: %v", err)
			}
			gotw := bytes.NewBuffer(nil)
			sinkOpts, errs := stdiosink.StdioOptsFrom(cfg.Formatter)
			if len(errs) > 0 {
				t.Fatalf("errs=%v", errs)
			}
			s := stdiosink.NewStdio(gotw, sinkOpts)
			err = Scan(ctx, bytes.NewReader(input), s, HandlerOptionsFrom(cfg.Parser))
			if err != nil {
				t.Fatalf("scanning input: %v", err)
			}
			// minl := len(want)
			got := gotw.Bytes()
			if len(want) < len(got) {
				t.Errorf("want len %d got %d", len(want), len(got))
			}
			if len(want) > len(got) {
				t.Errorf("want len %d got %d", len(want), len(got))
				// minl = len(got)
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Error(diff)
			}

			// ranges := newByteRanges()
			// for i, w := range want[:minl] {
			// 	g := got[i]
			// 	if w != g {
			// 		ranges.track(i)
			// 	}
			// }
			// mismatches := len(ranges.ranges)
			// if mismatches == 0 {
			// 	return
			// }
			// t.Errorf("total of %d ranges mismatch", mismatches)
			// if len(ranges.ranges) > 10 {
			// 	mismatches = 10
			// 	t.Errorf("only showing first %d mismatches", mismatches)
			// }
			// for _, br := range ranges.ranges[:mismatches] {
			// 	t.Errorf("- mismatch from byte %d to %d", br.start, br.end)
			// 	wantPart := want[br.start:br.end]
			// 	gotPart := got[br.start:br.end]
			// 	t.Errorf("want %q", wantPart)
			// 	t.Errorf("got  %q", gotPart)
			// }

			dir, err := os.MkdirTemp(os.TempDir(), "humanlog-tests-*")
			if err != nil {
				t.Fatal(err)
			}
			gotf, err := os.CreateTemp(dir, de.Name())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := gotf.Write(got); err != nil {
				t.Fatal(err)
			}
			if err := gotf.Close(); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote output to %q", gotf.Name())
		})
	}
}

type byteranges struct {
	ranges []*byterange
}

func replaceCRLF(org []byte, oldCRLF string, newCRLF string) []byte {
	replaced := strings.ReplaceAll(string(org), oldCRLF, newCRLF)
	return []byte(replaced)
}

func newByteRanges() *byteranges {
	return &byteranges{}
}

func (br *byteranges) track(idx int) {
	if len(br.ranges) == 0 {
		br.ranges = append(br.ranges, &byterange{start: idx, end: idx + 1})
		return
	}
	lastRange := br.ranges[len(br.ranges)-1]
	if lastRange.end == idx {
		lastRange.end = idx + 1
	} else {
		br.ranges = append(br.ranges, &byterange{start: idx, end: idx + 1})
	}
}

type byterange struct {
	start int
	end   int
}
