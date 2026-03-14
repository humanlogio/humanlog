# flatjson

Fast and dirty parsing of JSON! Decode only the parts you care about!

## Why

This parser is very fast (or I mean it's not slow?) and allows you to only parse the things you care about. It also allows you to "visit" all the keys of a JSON entity recursively, in a single pass.

```go
data := []byte(`{
    "hello":["world"],
    "bonjour": {"le": "monde"}
}`)

flatjson.ScanObject(data, 0, &flatjson.Callbacks{
    MaxDepth: 99,
    OnNumber: func(prefixes flatjson.Prefixes, val flatjson.Number) {
        // handle
    },
    OnString: func(prefixes flatjson.Prefixes, val flatjson.String) {
        // handle
    },
    OnBoolean: func(prefixes flatjson.Prefixes, val flatjson.Bool) {
        // handle
    },
    OnNull: func(prefixes flatjson.Prefixes, val flatjson.Null) {
        // handle
    },
})
```

## Speed

In a dumb benchmark:

```
BenchmarkFlatJSON-12        	 1660015	       722.9 ns/op	 930.93 MB/s	      32 B/op	       2 allocs/op
BenchmarkEncodingJSON-12    	  594927	      2394 ns/op	 311.13 MB/s	     168 B/op	       3 allocs/op
```

## About that name

This library used to support only what I called a "flat" subset of JSON. But now it supports all JSON, but you can still decide how "flat" you want to go. The flatter, the faster :).

> Flat JSON is a subset of JSON where the only supported types are objects containing
> strings, numbers, booleans or null values. There can't be nested objects or
> arrays. The root element must be an object.
