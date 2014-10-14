package logfmt

import (
	"reflect"
	"strconv"
	"testing"
)

func TestScannerSimple(t *testing.T) {
	type T struct {
		k string
		v string
	}

	tests := []struct {
		data string
		want []T
	}{
		{
			`a=1 b="bar" ƒ=2h3s r="esc\t" d x=sf   `,
			[]T{
				{"a", "1"},
				{"b", "bar"},
				{"ƒ", "2h3s"},
				{"r", "esc\t"},
				{"d", ""},
				{"x", "sf"},
			},
		},
		{
			`quotes=` + strconv.Quote(`outside double quotes "inside double quotes 'inside single quotes'"`),
			[]T{{"quotes", `outside double quotes "inside double quotes 'inside single quotes'"`}},
		},
		{
			`equals="UPDATE herp SET is_derp=0 WHERE herp_id=9000 AND derp_id=1"`,
			[]T{{"equals", `UPDATE herp SET is_derp=0 WHERE herp_id=9000 AND derp_id=1`}},
		},
		{`x= `, []T{{"x", ""}}},
		{`y=`, []T{{"y", ""}}},
		{`y`, []T{{"y", ""}}},
		{`y=f`, []T{{"y", "f"}}},
	}

	for _, test := range tests {
		var got []T
		h := func(key, val []byte) error {
			got = append(got, T{string(key), string(val)})
			return nil
		}
		gotoScanner([]byte(test.data), HandlerFunc(h))
		if !reflect.DeepEqual(test.want, got) {
			t.Errorf("want %q, got %q", test.want, got)
		}
	}

	var called bool
	h := func(key, val []byte) error { called = true; return nil }
	err := gotoScanner([]byte(`foo="b`), HandlerFunc(h))
	if err != ErrUnterminatedString {
		t.Errorf("want %v, got %v", ErrUnterminatedString, err)
	}
	if called {
		t.Error("did not expect call to handler")
	}
}

func TestScannerAllocs(t *testing.T) {
	data := []byte(`a=1 b="bar" ƒ=2h3s r="esc\t" d x=sf   `)
	h := func(key, val []byte) error { return nil }
	allocs := testing.AllocsPerRun(1000, func() {
		gotoScanner(data, HandlerFunc(h))
	})
	if allocs > 1 {
		t.Errorf("got %f, want <=1", allocs)
	}
}
