package typesv1

import (
	"fmt"
	"iter"
	"slices"

	"github.com/cespare/xxhash"
)

const (
	tagNull       = uint8(0x0)
	tagBool       = uint8(0x1)
	tagI64        = uint8(0x2)
	tagF64        = uint8(0x3)
	tagTs         = uint8(0x4)
	tagDur        = uint8(0x5)
	tagStr        = uint8(0x6)
	tagBlob       = uint8(0x7)
	tagArray      = uint8(0x8)
	tagMapEntries = uint8(0x9)
	tagKVs        = uint8(0xA)
	tagHash64     = uint8(0xB)
	tagTraceID    = uint8(0xC)
	tagSpanID     = uint8(0xD)
	tagULID       = uint8(0xE)
)

var (
	nullh          = xxhash.Sum64([]byte{tagNull, 0x0})
	trueh          = xxhash.Sum64([]byte{tagBool, 0x1})
	falseh         = xxhash.Sum64([]byte{tagBool, 0x0})
	tagArrayh      = xxhash.Sum64([]byte{tagArray})
	tagMapEntriesh = xxhash.Sum64([]byte{tagMapEntries})
	tagKVsh        = xxhash.Sum64([]byte{tagKVs})
)

func Hash64Resource(schemaURL string, kvs iter.Seq2[string, Valuer]) uint64 {
	h64 := Hash64String(schemaURL)
	h64 ^= Hash64AnyKVs_orderDoesntMatter(kvs)
	return h64
}

func Hash64Scope(schemaURL, name, version string, kvs iter.Seq2[string, Valuer]) uint64 {
	h64 := Hash64String(schemaURL)
	h64 ^= Hash64String(name)
	h64 ^= Hash64String(version)
	h64 ^= Hash64AnyKVs_orderDoesntMatter(kvs)
	return h64
}

func Hash64Value(val *Val) uint64 {
	switch vv := val.Kind.(type) {
	case *Val_Str:
		return Hash64String(vv.Str)
	case *Val_TraceId:
		return Hash64TraceID(vv.TraceId.Raw)
	case *Val_SpanId:
		return Hash64SpanID(vv.SpanId.Raw)
	case *Val_Ulid:
		return Hash64ULID(vv.Ulid.High, vv.Ulid.Low)
	case *Val_Blob:
		return Hash64Blob(vv.Blob)
	case *Val_Null:
		return Hash64Null()
	case *Val_Bool:
		return Hash64Bool(vv.Bool)
	case *Val_I64:
		return Hash64I64(vv.I64)
	case *Val_F64:
		return Hash64F64(vv.F64)
	case *Val_Hash64:
		return Hash64Hash64(vv.Hash64)
	case *Val_Ts:
		return Hash64Ts(vv.Ts.Seconds, vv.Ts.Nanos)
	case *Val_Dur:
		return Hash64Dur(vv.Dur.Seconds, vv.Dur.Nanos)
	case *Val_Arr:
		return Hash64Values_orderDoesntMatter(vv.Arr.Items)
	case *Val_Obj:
		return Hash64KeyValues_orderDoesntMatter(vv.Obj.Kvs)
	case *Val_Map:
		return Hash64MapEntries_orderDoesntMatter(vv.Map.Entries)
	default:
		panic(fmt.Sprintf("missing case: %T (%#v)", vv, vv))
	}
}

func Hash64KeyValues_orderDoesntMatter(kvs []*KV) uint64 {
	xored := tagKVsh
	for _, kv := range kvs {
		h := xxhash.Sum64String(kv.Key) ^ Hash64Value(kv.Value)
		xored ^= h
	}
	return xored
}

func Hash64MapEntries_orderDoesntMatter(kvs []*Map_Entry) uint64 {
	xored := tagMapEntriesh
	for _, kv := range kvs {
		h := Hash64Value(kv.Key) ^ Hash64Value(kv.Value)
		xored ^= h
	}
	return xored
}

func Hash64Values_orderDoesntMatter(arr []*Val) uint64 {
	xored := tagArrayh
	for _, el := range arr {
		h := Hash64Value(el)
		xored ^= h
	}
	return xored
}

func Hash64Any(v Valuer) uint64 {
	return v(
		Hash64String,
		Hash64TraceID,
		Hash64SpanID,
		Hash64ULID,
		Hash64Blob,
		Hash64Null,
		Hash64Bool,
		Hash64I64,
		Hash64F64,
		Hash64Hash64,
		Hash64Ts,
		Hash64Dur,
		Hash64AnyArray_orderDoesntMatter,
		Hash64AnyKVs_orderDoesntMatter,
		Hash64AnyMaps_orderDoesntMatter,
	)
}

func Hash64AnyArray_orderDoesntMatter(items iter.Seq[Valuer]) uint64 {
	xored := tagArrayh
	for el := range items {
		h := Hash64Any(el)
		xored ^= h
	}
	return xored
}

func Hash64AnyKVs_orderDoesntMatter(kvs iter.Seq2[string, Valuer]) uint64 {
	xored := tagKVsh
	for k, v := range kvs {
		kh := xxhash.Sum64String(k)
		vh := Hash64Any(v)
		h := kh ^ vh
		xored ^= h
	}
	return xored
}

func Hash64AnyMaps_orderDoesntMatter(kvs iter.Seq2[Valuer, Valuer]) uint64 {
	xored := tagMapEntriesh
	for k, v := range kvs {
		kh := Hash64Any(k)
		vh := Hash64Any(v)
		h := kh ^ vh
		xored ^= h
	}
	return xored
}

func Hash64String(v string) uint64 {
	fp := slices.Concat([]byte{tagStr}, []byte(v))
	return xxhash.Sum64(fp)
}

func Hash64TraceID(v []byte) uint64 {
	fp := slices.Concat([]byte{tagTraceID}, v)
	return xxhash.Sum64(fp[:])
}

func Hash64SpanID(v []byte) uint64 {
	fp := slices.Concat([]byte{tagSpanID}, v)
	return xxhash.Sum64(fp[:])
}

func Hash64ULID(hi, lo uint64) uint64 {
	fp := [17]byte{
		tagULID,
		byte(hi >> 56),
		byte(hi >> 48),
		byte(hi >> 40),
		byte(hi >> 32),
		byte(hi >> 24),
		byte(hi >> 16),
		byte(hi >> 8),
		byte(hi),
		byte(lo >> 56),
		byte(lo >> 48),
		byte(lo >> 40),
		byte(lo >> 32),
		byte(lo >> 24),
		byte(lo >> 16),
		byte(lo >> 8),
		byte(lo),
	}
	return xxhash.Sum64(fp[:])
}

func Hash64Blob(v []byte) uint64 {
	fp := slices.Concat([]byte{tagBlob}, v)
	return xxhash.Sum64(fp)
}

func Hash64Null() uint64 {
	return nullh
}

func Hash64Bool(v bool) uint64 {
	if v {
		return trueh
	}
	return falseh
}

func Hash64I64(v int64) uint64 {
	fp := [9]byte{
		tagI64,
		byte(v >> 56),
		byte(v >> 48),
		byte(v >> 40),
		byte(v >> 32),
		byte(v >> 24),
		byte(v >> 16),
		byte(v >> 8),
		byte(v),
	}
	return xxhash.Sum64(fp[:])
}

func Hash64F64(v float64) uint64 {
	fp := [9]byte{
		tagF64,
		byte(int64(v) >> 56),
		byte(int64(v) >> 48),
		byte(int64(v) >> 40),
		byte(int64(v) >> 32),
		byte(int64(v) >> 24),
		byte(int64(v) >> 16),
		byte(int64(v) >> 8),
		byte(int64(v)),
	}
	return xxhash.Sum64(fp[:])
}

func Hash64Hash64(v uint64) uint64 {
	fp := [9]byte{
		tagHash64,
		byte(v >> 56),
		byte(v >> 48),
		byte(v >> 40),
		byte(v >> 32),
		byte(v >> 24),
		byte(v >> 16),
		byte(v >> 8),
		byte(v),
	}
	return xxhash.Sum64(fp[:])
}

func Hash64Ts(seconds int64, nanos int32) uint64 {
	fp := [13]byte{
		tagTs,
		byte(nanos >> 24),
		byte(nanos >> 16),
		byte(nanos >> 8),
		byte(nanos),

		byte(seconds >> 56),
		byte(seconds >> 48),
		byte(seconds >> 40),
		byte(seconds >> 32),
		byte(seconds >> 24),
		byte(seconds >> 16),
		byte(seconds >> 8),
		byte(seconds),
	}
	return xxhash.Sum64(fp[:])
}

func Hash64Dur(seconds int64, nanos int32) uint64 {
	fp := [13]byte{
		tagDur,
		byte(nanos >> 24),
		byte(nanos >> 16),
		byte(nanos >> 8),
		byte(nanos),

		byte(seconds >> 56),
		byte(seconds >> 48),
		byte(seconds >> 40),
		byte(seconds >> 32),
		byte(seconds >> 24),
		byte(seconds >> 16),
		byte(seconds >> 8),
		byte(seconds),
	}
	return xxhash.Sum64(fp[:])
}

type Valuer func(
	onStr func(string) uint64,
	onTraceId func([]byte) uint64,
	onSpanId func([]byte) uint64,
	onUlid func(hi uint64, lo uint64) uint64,
	onBlob func([]byte) uint64,
	onNull func() uint64,
	onBool func(bool) uint64,
	onI64 func(int64) uint64,
	onF64 func(float64) uint64,
	onHash64 func(uint64) uint64,
	onTs func(seconds int64, nanos int32) uint64,
	onDur func(seconds int64, nanos int32) uint64,
	onArr func(iter.Seq[Valuer]) uint64,
	onObj func(iter.Seq2[string, Valuer]) uint64,
	onMap func(iter.Seq2[Valuer, Valuer]) uint64,
) uint64

func KVsToValuer(kvs []*KV) iter.Seq2[string, Valuer] {
	return func(yield func(string, Valuer) bool) {
		for _, kv := range kvs {
			if !yield(kv.Key, ValuerVal(kv.Value)) {
				return
			}
		}
	}
}

func ValsToValuer(vals []*Val) iter.Seq[Valuer] {
	return func(yield func(Valuer) bool) {
		for _, el := range vals {
			if !yield(ValuerVal(el)) {
				return
			}
		}
	}
}

func ObjectToValuer(obj *Obj) iter.Seq2[string, Valuer] {
	return KVsToValuer(obj.Kvs)
}

func MapToValuer(mm *Map) iter.Seq2[Valuer, Valuer] {
	return func(yield func(Valuer, Valuer) bool) {
		for _, kv := range mm.Entries {
			if !yield(ValuerVal(kv.Key), ValuerVal(kv.Value)) {
				return
			}
		}
	}
}

func ValuerVal(val *Val) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		switch vv := val.Kind.(type) {
		case *Val_Str:
			return onStr(vv.Str)
		case *Val_F64:
			return onF64(vv.F64)
		case *Val_I64:
			return onI64(vv.I64)
		case *Val_Hash64:
			return onHash64(vv.Hash64)
		case *Val_Bool:
			return onBool(vv.Bool)
		case *Val_Ts:
			return onTs(vv.Ts.Seconds, vv.Ts.Nanos)
		case *Val_Dur:
			return onDur(vv.Dur.Seconds, vv.Dur.Nanos)
		case *Val_Blob:
			return onBlob(vv.Blob)
		case *Val_TraceId:
			return onTraceId(vv.TraceId.Raw)
		case *Val_SpanId:
			return onSpanId(vv.SpanId.Raw)
		case *Val_Ulid:
			return onUlid(vv.Ulid.High, vv.Ulid.Low)
		case *Val_Arr:
			return onArr(ValsToValuer(vv.Arr.Items))
		case *Val_Obj:
			return onObj(ObjectToValuer(vv.Obj))
		case *Val_Map:
			return onMap(MapToValuer(vv.Map))
		case *Val_Null:
			return onNull()
		default:
			panic(fmt.Sprintf("missing case: %T (%v)", vv, vv))
		}
	}
}

func ValuerString(v string) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onStr(v)
	}
}

func ValuerTraceID(v []byte) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onTraceId(v)
	}
}

func ValuerSpanID(v []byte) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onSpanId(v)
	}
}

func ValuerULID(hi, lo uint64) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onUlid(hi, lo)
	}
}

func ValuerBlob(v []byte) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onBlob(v)
	}
}
func ValuerNull() Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onNull()
	}
}

func ValuerBool(v bool) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onBool(v)
	}
}

func ValuerI64(v int64) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onI64(v)
	}
}

func ValuerF64(v float64) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onF64(v)
	}
}

func ValuerHash64(v uint64) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onHash64(v)
	}
}

func ValuerTimestamp(seconds int64, nanos int32) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onTs(seconds, nanos)
	}
}

func ValuerDuration(seconds int64, nanos int32) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onDur(seconds, nanos)
	}
}

func ValuerIteratorFromSlice(v []Valuer) iter.Seq[Valuer] {
	return func(yield func(Valuer) bool) {
		for _, el := range v {
			if !yield(el) {
				return
			}
		}
	}
}

func ValuerArr(v iter.Seq[Valuer]) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onArr(v)
	}
}

func ValuerIteratorFromObjMap(v map[string]Valuer) iter.Seq2[string, Valuer] {
	return func(yield func(string, Valuer) bool) {
		for k, v := range v {
			if !yield(k, v) {
				return
			}
		}
	}
}

func ValuerObj(v iter.Seq2[string, Valuer]) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onObj(v)
	}
}

type ValuerMapEntry struct {
	Key    Valuer
	Valuer Valuer
}

func ValuerIteratorFromMap(kvs []ValuerMapEntry) iter.Seq2[Valuer, Valuer] {
	return func(yield func(Valuer, Valuer) bool) {
		for _, kv := range kvs {
			if !yield(kv.Key, kv.Valuer) {
				return
			}
		}
	}
}

func ValuerMap(v iter.Seq2[Valuer, Valuer]) Valuer {
	return func(
		onStr func(string) uint64,
		onTraceId, onSpanId func([]byte) uint64,
		onUlid func(hi uint64, lo uint64) uint64,
		onBlob func([]byte) uint64,
		onNull func() uint64,
		onBool func(bool) uint64,
		onI64 func(int64) uint64,
		onF64 func(float64) uint64,
		onHash64 func(uint64) uint64,
		onTs,
		onDur func(seconds int64, nanos int32) uint64,
		onArr func(iter.Seq[Valuer]) uint64,
		onObj func(iter.Seq2[string, Valuer]) uint64,
		onMap func(iter.Seq2[Valuer, Valuer]) uint64,
	) uint64 {
		return onMap(v)
	}
}
