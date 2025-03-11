package humanlog

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMoveToFront(t *testing.T) {
	t.Run("already front", func(t *testing.T) {
		in := []string{
			"a",
			"b",
			"c",
		}
		want := []string{
			"a",
			"b",
			"c",
		}
		got := moveToFront(0, in)
		require.Equal(t, want, got)
	})
	t.Run("middle", func(t *testing.T) {
		in := []string{
			"a",
			"b",
			"c",
		}
		want := []string{
			"b",
			"a",
			"c",
		}
		got := moveToFront(1, in)
		require.Equal(t, want, got)
	})
	t.Run("last", func(t *testing.T) {
		in := []string{
			"a",
			"b",
			"c",
		}
		want := []string{
			"c",
			"a",
			"b",
		}
		got := moveToFront(2, in)
		require.Equal(t, want, got)
	})
}

func TestTimeParseFloat64(t *testing.T) {
	t.Run("nanoseconds", func(t *testing.T) {
		golden := float64(1540369190466951764)
		tm := parseTimeFloat64(golden)
		if int64(golden) != tm.UnixNano() {
			t.Fatal(tm.UnixNano())
		}
	})
	t.Run("microseconds", func(t *testing.T) {
		golden := float64(1540369190466951)
		tm := parseTimeFloat64(golden)
		if int64(golden)*1e3 != tm.UnixNano() {
			t.Fatal(tm.UnixNano())
		}
	})
	t.Run("milliseconds", func(t *testing.T) {
		golden := float64(1540369190466)
		tm := parseTimeFloat64(golden)
		if int64(golden)*1e6 != tm.UnixNano() {
			t.Fatal(tm.UnixNano())
		}
	})
	t.Run("seconds", func(t *testing.T) {
		golden := float64(1540369190)
		tm := parseTimeFloat64(golden)
		if int64(golden)*1e9 != tm.UnixNano() {
			t.Fatal(tm.UnixNano())
		}
	})
	t.Run("f64 timestamp with nanosec", func(t *testing.T) {
		input := float64(1540369190.123456)
		want := time.Unix(1540369190, 123456000)
		got := parseTimeFloat64(input)

		require.WithinDuration(t, want, got, time.Microsecond)
	})
}

func TestTryParseFloatTime(t *testing.T) {
	testTime := time.Now()

	t.Run("microseconds", func(t *testing.T) {
		actualTime, ok := tryParseTime(fmt.Sprintf("%d", testTime.UnixMicro()))
		if !ok {
			t.Fatal("time not parsed")
		}

		if actualTime.UnixMicro() != testTime.UnixMicro() {
			t.Fatalf("time not equal: %d != %d", actualTime.UnixMicro(), testTime.UnixMicro())
		}
	})

	t.Run("milliseconds", func(t *testing.T) {
		actualTime, ok := tryParseTime(fmt.Sprintf("%d", testTime.UnixMilli()))
		if !ok {
			t.Fatal("time not parsed")
		}

		if actualTime.UnixMilli() != testTime.UnixMilli() {
			t.Fatalf("time not equal: %d != %d", actualTime.UnixMilli(), testTime.UnixMilli())
		}
	})

	t.Run("seconds", func(t *testing.T) {
		actualTime, ok := tryParseTime(fmt.Sprintf("%d", testTime.Unix()))
		if !ok {
			t.Fatal("time not parsed")
		}

		if actualTime.Unix() != testTime.Unix() {
			t.Fatalf("time not equal: %d != %d", actualTime.Unix(), testTime.Unix())
		}
	})

}
