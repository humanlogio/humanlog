package humanlog

import (
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

// tries to parse time using a couple of formats before giving up
func tryParseTime(value string) (time.Time, bool) {
	var t time.Time
	var err error
	for _, layout := range formats {
		t, err = time.Parse(layout, value)
		if err == nil {
			return t, true
		}
	}
	return t, false
}
