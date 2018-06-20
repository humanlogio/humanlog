package humanlog

import (
	"math"
	"time"
)

var formats = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
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
}

func parseTimeFloat64(value float64) (time.Time, bool) {
	if value/math.Pow10(15) > 1 { // Nanoseconds
		secs := int64(math.Trunc(value / math.Pow10(6)))
		nsecs := int64(math.Mod(value, math.Pow10(6)))
		return time.Unix(secs, nsecs), true
	} else if value/math.Pow10(12) > 1 { // Milliseconds
		secs := int64(math.Trunc(value / math.Pow10(3)))
		nsecs := int64(math.Mod(value, math.Pow10(3))) * int64(math.Pow10(3))
		return time.Unix(secs, nsecs), true
	} else {
		return time.Unix(int64(value), 0), true
	}
}

// tries to parse time using a couple of formats before giving up
func tryParseTime(value interface{}) (time.Time, bool) {
	var t time.Time
	var err error
	switch value.(type) {
	case string:
		for _, layout := range formats {
			t, err = time.Parse(layout, value.(string))
			if err == nil {
				return t, true
			}
		}
	case float32:
		return parseTimeFloat64(float64(value.(float32)))
	case float64:
		return parseTimeFloat64(value.(float64))
	case int:
		return parseTimeFloat64(float64(value.(int)))
	case int32:
		return parseTimeFloat64(float64(value.(int32)))
	case int64:
		return parseTimeFloat64(float64(value.(int64)))
	}
	return t, false
}
