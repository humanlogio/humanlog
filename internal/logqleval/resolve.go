package logqleval

import (
	"fmt"
	"strconv"
	"strings"

	typesv1 "github.com/humanlogio/api/go/types/v1"
)

func VisitScalars(v *typesv1.Val, onScalar func(keys []string, scalar *typesv1.Val) error, prefixes ...string) error {
	return visitScalars(prefixes, v, onScalar)
}

func visitScalars(keys []string, v *typesv1.Val, onScalar func(keys []string, scalar *typesv1.Val) error) error {
	switch val := v.Kind.(type) {
	case *typesv1.Val_Str:
		return onScalar(keys, v)
	case *typesv1.Val_F64:
		return onScalar(keys, v)
	case *typesv1.Val_I64:
		return onScalar(keys, v)
	case *typesv1.Val_Bool:
		return onScalar(keys, v)
	case *typesv1.Val_Ts:
		return onScalar(keys, v)
	case *typesv1.Val_Dur:
		return onScalar(keys, v)
	case *typesv1.Val_Arr:
		for i, item := range val.Arr.Items {
			k := strconv.Itoa(i)
			if err := onScalar(append(keys, k), item); err != nil {
				return fmt.Errorf("arr[%d] %v", i, err)
			}
		}
	case *typesv1.Val_Obj:
		for _, kval := range val.Obj.Kvs {
			if err := onScalar(append(keys, kval.Key), kval.Value); err != nil {
				return fmt.Errorf("obj[%q] %v", kval.Key, err)
			}
		}
	default:
		return fmt.Errorf("unsupported type %T (%v)", val, val)
	}
	return nil
}

type MakeMap func(prefixes []string, kvs []*typesv1.KV, mkMap MakeMap, mkSlice MakeSlice, setVal func([]string, any) error) error
type MakeSlice func(prefixes []string, items []*typesv1.Val, mkMap MakeMap, mkSlice MakeSlice, setVal func([]string, any) error) error

func ResolveVal(v *typesv1.Val, mkMap MakeMap, mkSlice MakeSlice) (any, error) {
	var val any
	return val, resolveVal(nil, v, mkMap, mkSlice, func(s []string, a any) error {
		val = a
		return nil
	})
}

var (
	MakeFlatGoMap MakeMap = func(prefixes []string, kvs []*typesv1.KV, mkMap MakeMap, mkSlice MakeSlice, setVal func([]string, any) error) error {
		if len(prefixes) != 0 {
			// dive deeper until we hit literals
			for _, kv := range kvs {
				keys := append(prefixes, kv.Key)
				err := resolveVal(keys, kv.Value, mkMap, mkSlice, setVal)
				if err != nil {
					return err
				}
			}
			return nil
		}
		out := make(map[string]any, len(kvs))
		for _, kv := range kvs {
			keys := []string{kv.Key}
			err := resolveVal(keys, kv.Value, mkMap, mkSlice, func(s []string, a any) error {
				k := strings.Join(s, ".")
				out[k] = a
				return nil
			})
			if err != nil {
				return err
			}
		}
		return setVal(nil, out)
	}
	MakeFlatMapGoSlice MakeSlice = func(prefixes []string, elems []*typesv1.Val, mkMap MakeMap, mkSlice MakeSlice, setVal func([]string, any) error) error {
		if len(prefixes) != 0 {
			// dive deeper until we hit literals
			for i, el := range elems {
				keys := append(prefixes, strconv.Itoa(i))
				err := resolveVal(keys, el, mkMap, mkSlice, setVal)
				if err != nil {
					return err
				}
			}
			return nil
		}
		out := make(map[string]any, len(elems))
		for i, el := range elems {
			keys := []string{strconv.Itoa(i)}
			err := resolveVal(keys, el, mkMap, mkSlice, func(s []string, a any) error {
				k := strings.Join(s, ".")
				out[k] = a
				return nil
			})
			if err != nil {
				return err
			}
		}
		return setVal(nil, out)
	}
	MakeFlatGoSlice MakeSlice = func(prefixes []string, elems []*typesv1.Val, mkMap MakeMap, mkSlice MakeSlice, setVal func([]string, any) error) error {
		if len(prefixes) != 0 {
			// dive deeper until we hit literals
			for i, el := range elems {
				keys := append(prefixes, strconv.Itoa(i))
				err := resolveVal(keys, el, mkMap, mkSlice, setVal)
				if err != nil {
					return err
				}
			}
			return nil
		}
		out := make([]any, 0, len(elems))
		for _, el := range elems {
			err := resolveVal(nil, el, mkMap, mkSlice, func(s []string, a any) error {
				out = append(out, a)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return setVal(nil, out)
	}
)

func resolveVal(
	prefixes []string,
	v *typesv1.Val,
	mkMap MakeMap,
	mkSlice MakeSlice,
	setVal func([]string, any) error,
) error {
	switch val := v.Kind.(type) {
	case *typesv1.Val_Str:
		return setVal(prefixes, val.Str)
	case *typesv1.Val_F64:
		return setVal(prefixes, val.F64)
	case *typesv1.Val_I64:
		return setVal(prefixes, val.I64)
	case *typesv1.Val_Bool:
		return setVal(prefixes, val.Bool)
	case *typesv1.Val_Ts:
		return setVal(prefixes, val.Ts.AsTime())
	case *typesv1.Val_Dur:
		return setVal(prefixes, val.Dur.AsDuration())
	case *typesv1.Val_Arr:
		return mkSlice(prefixes, val.Arr.Items, mkMap, mkSlice, setVal)
	case *typesv1.Val_Obj:
		return mkMap(prefixes, val.Obj.Kvs, mkMap, mkSlice, setVal)
	case *typesv1.Val_Null:
		return setVal(prefixes, nil)
	default:
		return fmt.Errorf("unsupported type %v (%T)", val, val)
	}
}
