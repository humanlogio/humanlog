package typesv1

import (
	"bytes"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otlpv1 "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

func FromOTLPKVs(attrs []*otlpv1.KeyValue) []*KV {
	out := make([]*KV, 0, len(attrs))
	for _, el := range attrs {
		out = append(out, FromOTLPKV(el))
	}
	return out
}

func ToOTLPKVs(attrs []*KV) []*otlpv1.KeyValue {
	out := make([]*otlpv1.KeyValue, 0, len(attrs))
	for _, el := range attrs {
		out = append(out, ToOTLPKV(el))
	}
	return out
}

func FromOTLPKV(attr *otlpv1.KeyValue) *KV {
	return KeyVal(attr.Key, FromOTLPVal(attr.Value))
}

func ToOTLPKV(attr *KV) *otlpv1.KeyValue {
	return &otlpv1.KeyValue{Key: attr.Key, Value: ToOTLPVal(attr.Value)}
}

func FromOTLPVal(v *otlpv1.AnyValue) *Val {
	switch tt := v.Value.(type) {
	case *otlpv1.AnyValue_BoolValue:
		return ValBool(tt.BoolValue)
	case *otlpv1.AnyValue_IntValue:
		return ValI64(tt.IntValue)
	case *otlpv1.AnyValue_DoubleValue:
		return ValF64(tt.DoubleValue)
	case *otlpv1.AnyValue_StringValue:
		return ValStr(tt.StringValue)
	case *otlpv1.AnyValue_BytesValue:
		return ValBlob(tt.BytesValue)

	case *otlpv1.AnyValue_ArrayValue:
		arr := tt.ArrayValue.Values
		out := make([]*Val, 0, len(arr))
		for _, el := range arr {
			out = append(out, FromOTLPVal(el))
		}
		return ValArr(out...)
	case *otlpv1.AnyValue_KvlistValue:
		kvs := tt.KvlistValue.Values
		out := make([]*KV, 0, len(kvs))
		for _, kv := range kvs {
			out = append(out, KeyVal(kv.Key, FromOTLPVal(kv.Value)))
		}
		return ValObj(out...)
	default:
		panic(fmt.Sprintf("missing case: %#v (%T)", tt, tt))
	}
}

func ToOTLPVal(v *Val) *otlpv1.AnyValue {
	switch tt := v.Kind.(type) {
	case *Val_Str:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_StringValue{StringValue: tt.Str}}
	case *Val_F64:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_DoubleValue{DoubleValue: tt.F64}}
	case *Val_I64:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_IntValue{IntValue: tt.I64}}
	case *Val_Hash64:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_IntValue{IntValue: int64(tt.Hash64)}}
	case *Val_Bool:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_BoolValue{BoolValue: tt.Bool}}
	case *Val_Ts:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_IntValue{IntValue: tt.Ts.AsTime().UnixNano()}}
	case *Val_Dur:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_IntValue{IntValue: tt.Dur.AsDuration().Nanoseconds()}}
	case *Val_Blob:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_BytesValue{BytesValue: tt.Blob}}
	case *Val_TraceId:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_BytesValue{BytesValue: TraceIDToBytes(tt.TraceId)}}
	case *Val_SpanId:
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_BytesValue{BytesValue: SpanIDToBytes(tt.SpanId)}}
	case *Val_Ulid:
		bytes := ULIDToBytes(nil, tt.Ulid)
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_BytesValue{BytesValue: bytes[:]}}
	case *Val_Arr:
		arr := tt.Arr.Items
		out := make([]*otlpv1.AnyValue, 0, len(arr))
		for _, el := range arr {
			out = append(out, ToOTLPVal(el))
		}
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_ArrayValue{ArrayValue: &otlpv1.ArrayValue{Values: out}}}
	case *Val_Obj:
		kvs := tt.Obj.Kvs
		out := make([]*otlpv1.KeyValue, 0, len(kvs))
		for _, kv := range kvs {
			out = append(out, ToOTLPKV(kv))
		}
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_KvlistValue{KvlistValue: &otlpv1.KeyValueList{Values: out}}}
	case *Val_Map:
		kvs := tt.Map.Entries
		out := make([]*otlpv1.KeyValue, 0, len(kvs))
		for _, kv := range kvs {
			el := &otlpv1.KeyValue{Value: ToOTLPVal(kv.Value)}
			switch kk := kv.Key.Kind.(type) {
			case *Val_Str:
				el.Key = kk.Str
			default:
				key, err := protojson.Marshal(kv.Key)
				if err != nil {
					panic(err)
				}
				el.Key = string(key)
			}
			out = append(out, el)
		}
		return &otlpv1.AnyValue{Value: &otlpv1.AnyValue_KvlistValue{KvlistValue: &otlpv1.KeyValueList{Values: out}}}
	case *Val_Null:
		return &otlpv1.AnyValue{}
	default:
		panic(fmt.Sprintf("missing case: %#v (%T)", tt, tt))
	}
}

func FromOTELAttributes(attrs []attribute.KeyValue) []*KV {
	out := make([]*KV, 0, len(attrs))
	for _, el := range attrs {
		out = append(out, FromOTELAttribute(el))
	}
	return out
}

func FromOTELAttribute(attr attribute.KeyValue) *KV {
	switch tt := attr.Value.Type(); tt {
	case attribute.BOOL:
		return KeyVal(string(attr.Key), ValBool(attr.Value.AsBool()))
	case attribute.INT64:
		return KeyVal(string(attr.Key), ValI64(attr.Value.AsInt64()))
	case attribute.FLOAT64:
		return KeyVal(string(attr.Key), ValF64(attr.Value.AsFloat64()))
	case attribute.STRING:
		return KeyVal(string(attr.Key), ValStr(attr.Value.AsString()))
	case attribute.BOOLSLICE:
		in := attr.Value.AsBoolSlice()
		out := make([]*Val, 0, len(in))
		for _, el := range in {
			out = append(out, ValBool(el))
		}
		return KeyVal(string(attr.Key), ValArr(out...))
	case attribute.INT64SLICE:
		in := attr.Value.AsInt64Slice()
		out := make([]*Val, 0, len(in))
		for _, el := range in {
			out = append(out, ValI64(el))
		}
		return KeyVal(string(attr.Key), ValArr(out...))
	case attribute.FLOAT64SLICE:
		in := attr.Value.AsFloat64Slice()
		out := make([]*Val, 0, len(in))
		for _, el := range in {
			out = append(out, ValF64(el))
		}
		return KeyVal(string(attr.Key), ValArr(out...))
	case attribute.STRINGSLICE:
		in := attr.Value.AsStringSlice()
		out := make([]*Val, 0, len(in))
		for _, el := range in {
			out = append(out, ValStr(el))
		}
		return KeyVal(string(attr.Key), ValArr(out...))
	default:
		panic(fmt.Sprintf("missing otel attribute type: %#v (%T)", tt, tt))
	}
}

func KeyVal(k string, v *Val) *KV {
	return &KV{Key: k, Value: v}
}

func EqualTypes(a, b *VarType) bool {
	return proto.Equal(a, b)
}

func EqualVal(a, b *Val) bool {
	switch ak := a.Kind.(type) {
	default:
		return false
	case *Val_Str:
		return ak.Str == b.GetStr()
	case *Val_F64:
		return ak.F64 == b.GetF64()
	case *Val_I64:
		return ak.I64 == b.GetI64()
	case *Val_Hash64:
		return ak.Hash64 == b.GetHash64()
	case *Val_Bool:
		return ak.Bool == b.GetBool()
	case *Val_Ts:
		return ak.Ts.AsTime().Equal(b.GetTs().AsTime())
	case *Val_Dur:
		return ak.Dur == b.GetDur()
	case *Val_Blob:
		return bytes.Equal(ak.Blob, b.GetBlob())
	case *Val_TraceId:
		return ak.TraceId == b.GetTraceId()
	case *Val_SpanId:
		return ak.SpanId == b.GetSpanId()
	case *Val_Ulid:
		return ak.Ulid == b.GetUlid()
	case *Val_Arr:
		if len(ak.Arr.Items) != len(b.GetArr().Items) {
			return false
		}
		for i, ael := range ak.Arr.Items {
			bel := b.GetArr().Items[i]
			if !EqualVal(ael, bel) {
				return false
			}
		}
		return true
	case *Val_Obj:
		if len(ak.Obj.Kvs) != len(b.GetObj().Kvs) {
			return false
		}
		// for simplicity, order matters
		// TODO: make it so the order doesn't matter
		for i, akv := range ak.Obj.Kvs {
			bkv := b.GetObj().Kvs[i]
			if akv.Key != bkv.Key {
				return false
			}
			if !EqualVal(akv.Value, bkv.Value) {
				return false
			}
		}
		return true
	case *Val_Map:
		if len(ak.Map.Entries) != len(b.GetMap().Entries) {
			return false
		}
		// for simplicity, order matters
		// TODO: make it so the order doesn't matter
		for i, akv := range ak.Map.Entries {
			bkv := b.GetMap().Entries[i]
			if !EqualVal(akv.Key, bkv.Key) {
				return false
			}
			if !EqualVal(akv.Value, bkv.Value) {
				return false
			}
		}
		return true
	case *Val_Null:
		_, ok := b.Kind.(*Val_Null)
		return ok
	}
}

func TypeUnknown() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_unknown}}
}

func TypeStr() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_str}}
}

func TypeF64() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_f64}}
}

func TypeI64() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_i64}}
}

func TypeHash64() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_hash64}}
}

func TypeBool() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_bool}}
}

func TypeTimestamp() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_ts}}
}

func TypeDuration() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_dur}}
}

func TypeBlob() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_blob}}
}

func TypeTraceID() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_trace_id}}
}

func TypeSpanID() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_span_id}}
}

func TypeULID() *VarType {
	return &VarType{Type: &VarType_Scalar{Scalar: ScalarType_ulid}}
}

func TypeArr(v ...*VarType) *VarType {
	atyp := &VarType_ArrayType{Items: &VarType{Type: &VarType_Scalar{Scalar: ScalarType_unknown}}}
	for i, item := range v {
		if i == 0 {
			atyp.Items.Type = item.Type
		} else if item.Type != atyp.Items.Type {
			atyp.Items.Type = &VarType_Scalar{Scalar: ScalarType_unknown}
			break
		}
	}
	return &VarType{Type: &VarType_Array{Array: atyp}}
}

func TypeObj(types map[string]*VarType) *VarType {
	otyp := &VarType_ObjectType{Kvs: types}
	return &VarType{Type: &VarType_Object{Object: otyp}}
}

func TypeObjFromKVs(v ...*KV) *VarType {
	types := make(map[string]*VarType)
	for _, kv := range v {
		types[kv.Key] = kv.Value.Type
	}
	return TypeObj(types)
}

func TypeMap(k, v *VarType) *VarType {
	return &VarType{Type: &VarType_Map{Map: &VarType_MapType{Key: k, Value: v}}}
}

func TypeNull() *VarType {
	return &VarType{Type: &VarType_Null_{}}
}

func ValStr(v string) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_str}}
	return &Val{Type: typ, Kind: &Val_Str{Str: v}}
}

func ValF64(v float64) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_f64}}
	return &Val{Type: typ, Kind: &Val_F64{F64: v}}
}

func ValI64(v int64) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_i64}}
	return &Val{Type: typ, Kind: &Val_I64{I64: v}}
}

func ValHash64(v uint64) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_hash64}}
	return &Val{Type: typ, Kind: &Val_Hash64{Hash64: v}}
}

func ValBool(v bool) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_bool}}
	return &Val{Type: typ, Kind: &Val_Bool{Bool: v}}
}

func ValTime(v time.Time) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_ts}}
	return &Val{Type: typ, Kind: &Val_Ts{Ts: timestamppb.New(v)}}
}

func ValTimestamp(v *timestamppb.Timestamp) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_ts}}
	return &Val{Type: typ, Kind: &Val_Ts{Ts: v}}
}

func ValDuration(v time.Duration) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_dur}}
	return &Val{Type: typ, Kind: &Val_Dur{Dur: durationpb.New(v)}}
}

func ValDurationPB(v *durationpb.Duration) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_dur}}
	return &Val{Type: typ, Kind: &Val_Dur{Dur: v}}
}

func ValBlob(blob []byte) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_blob}}
	return &Val{Type: typ, Kind: &Val_Blob{Blob: blob}}
}

func ValTraceID(traceID *TraceID) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_trace_id}}
	return &Val{Type: typ, Kind: &Val_TraceId{TraceId: traceID}}
}
func ValSpanID(spanID *SpanID) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_span_id}}
	return &Val{Type: typ, Kind: &Val_SpanId{SpanId: spanID}}
}

func ValULID(ul *ULID) *Val {
	typ := &VarType{Type: &VarType_Scalar{Scalar: ScalarType_ulid}}
	return &Val{Type: typ, Kind: &Val_Ulid{Ulid: ul}}
}

func ValArr(v ...*Val) *Val {
	typs := make([]*VarType, 0, len(v))
	for _, val := range v {
		typs = append(typs, val.Type)
	}
	return &Val{Type: TypeArr(typs...), Kind: &Val_Arr{Arr: &Arr{
		Items: v,
	}}}
}

func ValMap(kt, vt *VarType, v ...*Map_Entry) *Val {
	return &Val{Type: TypeMap(kt, vt), Kind: &Val_Map{Map: &Map{
		Entries: v,
	}}}
}

func ValObj(v ...*KV) *Val {
	return &Val{Type: TypeObjFromKVs(v...), Kind: &Val_Obj{Obj: &Obj{Kvs: v}}}
}

func ValNull() *Val {
	return &Val{Type: TypeNull(), Kind: &Val_Null{}}
}
