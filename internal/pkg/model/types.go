package model

import "time"

type KV struct {
	Key   string
	Value string
}

type Structured struct {
	Time  time.Time
	Level string
	Msg   string
	KVs   []KV
}

type Event struct {
	Raw        []byte
	Structured *Structured
}
