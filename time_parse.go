package humanlog

import (
	"time"
)

var TimeFormats = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05-0700",
	time.RFC3339,
	time.RFC3339Nano,
	time.RFC822,
	time.RFC822Z,
	time.RFC850,
	time.RFC1123,
	time.RFC1123Z,
	time.UnixDate,
	time.RubyDate,
	time.ANSIC,
	time.Kitchen,
	time.Stamp,
	time.StampMilli,
	time.StampMicro,
	time.StampNano,
	"2006/01/02 15:04:05",
	"2006/01/02 15:04:05.999999999",
}

func parseTimeFloat64(value float64) time.Time {
	v := int64(value)
	switch {
	case v > 1e18:
	case v > 1e15:
		v *= 1e3
	case v > 1e12:
		v *= 1e6
	default:
		return time.Unix(v, 0)
	}

	return time.Unix(v/1e9, v%1e9)
}

// tries to parse time using a couple of formats before giving up
func tryParseTime(value interface{}) (time.Time, bool) {
	var t time.Time
	var err error
	switch v := value.(type) {
	case string:
		for _, layout := range TimeFormats {
			t, err = time.Parse(layout, v)
			if err == nil {
				return t, true
			}
		}
	case float32:
		return parseTimeFloat64(float64(v)), true
	case float64:
		return parseTimeFloat64(v), true
	case int:
		return parseTimeFloat64(float64(v)), true
	case int32:
		return parseTimeFloat64(float64(v)), true
	case int64:
		return parseTimeFloat64(float64(v)), true
	}
	return t, false
}
