package humanlog

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestHarness(t *testing.T) {
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
			input, err := ioutil.ReadFile(filepath.Join(root, de.Name(), "input"))
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}
			want, err := ioutil.ReadFile(filepath.Join(root, de.Name(), "want"))
			if err != nil {
				t.Fatalf("reading expected output: %v", err)
			}
			optsjson, err := ioutil.ReadFile(filepath.Join(root, de.Name(), "opts.json"))
			if err != nil {
				t.Fatalf("reading options: %v", err)
			}
			opts := new(HandlerOptions)
			if err := json.Unmarshal(optsjson, &opts); err != nil {
				t.Fatalf("unmarshaling options: %v", err)
			}
			gotw := bytes.NewBuffer(nil)
			err = Scanner(bytes.NewReader(input), gotw, opts)
			if err != nil {
				t.Fatalf("scanning input: %v", err)
			}
			minl := len(want)
			got := gotw.Bytes()
			if len(want) < len(got) {
				t.Errorf("want len %d got %d", len(want), len(got))
			}
			if len(want) > len(got) {
				t.Errorf("want len %d got %d", len(want), len(got))
				minl = len(got)
			}

			ranges := newByteRanges()
			for i, w := range want[:minl] {
				g := got[i]
				if w != g {
					ranges.track(i)
				}
			}
			mismatches := len(ranges.ranges)
			t.Errorf("total of %d ranges mismatch", mismatches)
			if len(ranges.ranges) > 10 {
				mismatches = 10
				t.Errorf("only showing first %d mismatches", mismatches)
			}
			for _, br := range ranges.ranges[:mismatches] {
				t.Errorf("- mismatch from byte %d to %d", br.start, br.end)
				wantPart := want[br.start:br.end]
				gotPart := got[br.start:br.end]
				t.Errorf("want %q", wantPart)
				t.Errorf("got  %q", gotPart)
			}
		})
	}
}

type byteranges struct {
	ranges []*byterange
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
