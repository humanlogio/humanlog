package typesv1

import (
	"github.com/oklog/ulid/v2"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

func (x *Log) IsStructured() bool {
	if x.Timestamp != nil || x.SeverityText != "" || x.Body != "" || x.Attributes != nil {
		return true
	}
	return false
}

func (x *Log) FindAttr(k string) *Val {
	if k == string(semconv.LogRecordUIDKey) {
		u := ulid.ULID(ULIDToBytes(nil, x.Ulid))
		return ValStr(u.String())
	}
	if k == string(semconv.LogRecordOriginalKey) {
		return ValBlob(x.Raw)
	}
	for _, kv := range x.Attributes {
		if kv.Key == k {
			return kv.Value
		}
	}
	return nil
}
