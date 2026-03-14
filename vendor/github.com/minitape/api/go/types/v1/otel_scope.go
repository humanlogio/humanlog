package typesv1

func NewScope(schemaURL, name, version string, kvs []*KV) *Scope {
	return &Scope{
		ScopeHash_64: Hash64Scope(schemaURL, name, version, KVsToValuer(kvs)),
		SchemaUrl:    schemaURL,
		Name:         name,
		Version:      version,
		Attributes:   kvs,
	}
}
