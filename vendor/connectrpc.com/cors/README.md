cors
====
[![Build](https://github.com/connectrpc/cors-go/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/connectrpc/cors-go/actions/workflows/ci.yaml)
[![Report Card](https://goreportcard.com/badge/connectrpc.com/cors)](https://goreportcard.com/report/connectrpc.com/cors)
[![GoDoc](https://pkg.go.dev/badge/connectrpc.com/cors.svg)](https://pkg.go.dev/connectrpc.com/cors)
[![Slack](https://img.shields.io/badge/slack-buf-%23e01563)][slack]

`connectrpc.com/cors` simplifies Cross-Origin Resource Sharing (CORS) for
[Connect](https://github.com/connectrpc/connect-go) servers. CORS is usually
required for the Connect and gRPC-Web protocols to work correctly in
web browsers.

For background, more details, and best practices, see [Connect's CORS
documentation](https://connectrpc.com/docs/cors). For more on Connect, see the
[announcement blog post][blog], the documentation on [connectrpc.com][docs]
(especially the [Getting Started] guide for Go), the [demo
service][examples-go], or the [protocol specification][protocol].

## Example

This package should work with any CORS implementation. As an example, we'll use
it with [github.com/rs/cors](https://github.com/rs/cors).

```go
import (
  connectcors "connectrpc.com/cors"
  "github.com/rs/cors"
)

// withCORS adds CORS support to a Connect HTTP handler.
func withCORS(connectHandler http.Handler) http.Handler {
  c := cors.New(cors.Options{
    AllowedOrigins: []string{"https://acme.com"}, // replace with your domain
    AllowedMethods: connectcors.AllowedMethods(),
    AllowedHeaders: connectcors.AllowedHeaders(),
    ExposedHeaders: connectcors.ExposedHeaders(),
    MaxAge: 7200, // 2 hours in seconds
  })
  return c.Handler(connectHandler)
}
```

## Ecosystem

* [connect-go]: the Go implementation of Connect's RPC runtime
* [examples-go]: service powering [demo.connectrpc.com][demo], including bidi streaming
* [grpchealth]: gRPC-compatible health checks
* [grpcreflect]: gRPC-compatible server reflection
* [authn]: pluggable authentication for Connect servers
* [connect-es]: Type-safe APIs with Protobuf and TypeScript
* [conformance]: Connect, gRPC, and gRPC-Web interoperability tests

## Status: Unstable

This module isn't stable yet, but it's fairly small &mdash; we expect to reach
a stable release quickly.

It supports the three most recent major releases of Go.
Keep in mind that [only the last two releases receive security
patches][go-support-policy].

Within those parameters, `cors` follows semantic versioning. We will _not_
make breaking changes in the 1.x series of releases.

## Legal

Offered under the [Apache 2 license][license].

[Getting Started]: https://connectrpc.com/docs/go/getting-started
[authn]: https://github.com/connectrpc/authn-go
[blog]: https://buf.build/blog/connect-a-better-grpc
[conformance]: https://github.com/connectrpc/conformance
[connect-es]: https://github.com/connectrpc/connect-es
[connect-go]: https://github.com/connectrpc/connect-go
[cors]: https://github.com/connectrpc/cors-go
[demo]: https://demo.connectrpc.com
[docs]: https://connectrpc.com
[examples-go]: https://github.com/connectrpc/examples-go
[go-support-policy]: https://golang.org/doc/devel/release#policy
[godoc]: https://pkg.go.dev/connectrpc.com/authn
[grpchealth]: https://github.com/connectrpc/grpchealth-go
[grpcreflect]: https://github.com/connectrpc/grpcreflect-go
[license]: https://github.com/connectrpc/cors-go/blob/main/LICENSE
[protocol]: https://connectrpc.com/docs/protocol
[slack]: https://buf.build/links/slack
