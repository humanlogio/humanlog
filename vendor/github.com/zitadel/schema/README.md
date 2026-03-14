# schema

[![semantic-release](https://img.shields.io/badge/%20%20%F0%9F%93%A6%F0%9F%9A%80-semantic--release-e10079.svg)](https://github.com/semantic-release/semantic-release)
[![Release](https://github.com/zitadel/schema/workflows/Release/badge.svg)](https://github.com/zitadel/schema/actions)
[![Go Reference](https://pkg.go.dev/badge/github.com/zitadel/schema.svg)](https://pkg.go.dev/github.com/zitadel/schema)
[![license](https://badgen.net/github/license/zitadel/schema/)](https://github.com/zitadel/schema/blob/master/LICENSE)
[![release](https://badgen.net/github/release/zitadel/schema/stable)](https://github.com/zitadel/schema/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/zitadel/schema)](https://goreportcard.com/report/github.com/zitadel/schema)
[![codecov](https://codecov.io/gh/zitadel/schema/branch/main/graph/badge.svg?token=5QS2VEMCt2)](https://codecov.io/gh/zitadel/schema)

Package zitadel/schema converts structs to and from form values. This is a maintained fork of [gorilla/schema](https://github.com/gorilla/schema)

## Example

Here's a quick example: we parse POST form values and then decode them into a struct:

```go
// Set a Decoder instance as a package global, because it caches
// meta-data about structs, and an instance can be shared safely.
var decoder = schema.NewDecoder()

type Person struct {
    Name  string
    Phone string
}

func MyHandler(w http.ResponseWriter, r *http.Request) {
    err := r.ParseForm()
    if err != nil {
        // Handle error
    }

    var person Person

    // r.PostForm is a map of our POST form values
    err = decoder.Decode(&person, r.PostForm)
    if err != nil {
        // Handle error
    }

    // Do something with person.Name or person.Phone
}
```

Conversely, contents of a struct can be encoded into form values. Here's a variant of the previous example using the Encoder:

```go
var encoder = schema.NewEncoder()

func MyHttpRequest() {
    person := Person{"Jane Doe", "555-5555"}
    form := url.Values{}

    err := encoder.Encode(person, form)

    if err != nil {
        // Handle error
    }

    // Use form values, for example, with an http client
    client := new(http.Client)
    res, err := client.PostForm("http://my-api.test", form)
}

```

To define custom names for fields, use a struct tag "schema". To not populate certain fields, use a dash for the name and it will be ignored:

```go
type Person struct {
    Name  string `schema:"name,required"`  // custom name, must be supplied
    Phone string `schema:"phone"`          // custom name
    Admin bool   `schema:"-"`              // this field is never set
}
```

The supported field types in the struct are:

* bool
* float variants (float32, float64)
* int variants (int, int8, int16, int32, int64)
* string
* uint variants (uint, uint8, uint16, uint32, uint64)
* struct
* a pointer to one of the above types
* a slice or a pointer to a slice of one of the above types

Unsupported types are simply ignored, however custom types can be registered to be converted.

More examples are available on the Gorilla website: https://www.gorillatoolkit.org/pkg/schema

## License

BSD licensed. See the LICENSE file for details.
