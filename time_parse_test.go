package humanlog

import (
	"testing"
)

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
}
