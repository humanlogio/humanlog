package humanlog

import (
	"strconv"
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
	"06-01-02 15:04:05,999",
	"2006-01-02T15:04:05.999999999",
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
		decimals := value - float64(v)
		nsec := int64(decimals * float64(time.Second))
		return fixTimebeforeUnixZero(time.Unix(v, nsec))
	}

	return fixTimebeforeUnixZero(time.Unix(v/1e9, v%1e9))
}

var zeroTime = time.Time{}

func fixTimebeforeUnixZero(t time.Time) time.Time {
	if t.Unix() >= 0 {
		return t
	}
	// fast forward in the future at unix 0... unfortunately
	// we can't handle times before that, the JSON API stack
	// fails to marshal negative UNIX seconds (ConnectRPC)
	t = t.AddDate(zeroTime.Year(), 0, 0)
	return t
}

// tries to parse time using a couple of formats before giving up
func tryParseTime(value interface{}) (time.Time, bool) {
	var t time.Time
	switch v := value.(type) {
	case string:
		for i, layout := range TimeFormats {
			t, err := time.Parse(layout, value.(string))
			if err == nil {
				if dynamicReordering {
					TimeFormats = moveToFront(i, TimeFormats)
				}
				t = fixTimebeforeUnixZero(t)
				return t, true
			}
		}
		// try to parse unix time number from string
		floatVal, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return parseTimeFloat64(floatVal), true
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
	case []interface{}:
		if len(v) == 1 {
			if timeStr, ok := v[0].(string); ok {
				for _, layout := range TimeFormats {
					t, err := time.Parse(layout, timeStr)
					if err == nil {
						t = fixTimebeforeUnixZero(t)
						return t, true
					}
				}
			}
		}
	}
	return t, false
}
