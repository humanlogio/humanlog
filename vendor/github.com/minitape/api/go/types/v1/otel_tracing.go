package typesv1

import (
	"encoding/hex"
)

func TraceIDFromBytesArray(out *TraceID, id [16]byte) *TraceID {
	if out == nil {
		out = new(TraceID)
	}
	out.Raw = id[:]
	return out
}

func TraceIDFromBytesSlice(out *TraceID, id []byte) *TraceID {
	if out == nil {
		out = new(TraceID)
	}
	out.Raw = id
	return out
}

func TraceIDFromHex(out *TraceID, id string) (*TraceID, error) {
	b, err := hex.DecodeString(id)
	if err != nil {
		return nil, err
	}
	return TraceIDFromBytesSlice(out, b), nil
}

func TraceIDToHex(in *TraceID) string {
	raw := TraceIDToBytes(in)
	return hex.EncodeToString(raw[:])
}

func TraceIDToBytes(in *TraceID) []byte {
	return in.Raw
}

func SpanIDFromBytesArray(out *SpanID, id [8]byte) *SpanID {
	if out == nil {
		out = new(SpanID)
	}
	out.Raw = id[:]
	return out
}

func SpanIDFromBytesSlice(out *SpanID, id []byte) *SpanID {
	if out == nil {
		out = new(SpanID)
	}
	out.Raw = id
	return out
}

func SpanIDFromHex(out *SpanID, id string) (*SpanID, error) {
	b, err := hex.DecodeString(id)
	if err != nil {
		return nil, err
	}
	return SpanIDFromBytesSlice(out, b), nil
}

func SpanIDToHex(in *SpanID) string {
	raw := SpanIDToBytes(in)
	return hex.EncodeToString(raw[:])
}

func SpanIDToBytes(in *SpanID) []byte {
	return in.Raw
}
