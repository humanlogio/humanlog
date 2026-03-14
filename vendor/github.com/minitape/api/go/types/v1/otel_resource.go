package typesv1

import semconv "go.opentelemetry.io/otel/semconv/v1.34.0"

func NewResource(schemaURL string, kvs []*KV) *Resource {
	return &Resource{
		ResourceHash_64: Hash64Resource(schemaURL, KVsToValuer(kvs)),
		SchemaUrl:       schemaURL,
		Attributes:      kvs,
	}
}

const svcNameKey = string(semconv.ServiceNameKey)

func (v *Resource) LookupServiceName() string {
	for _, kv := range v.Attributes {
		if svcNameKey == kv.Key {
			return kv.Value.GetStr()
		}
	}
	return ""
}
