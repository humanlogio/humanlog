// Copyright 2023 The Connect Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package cors provides helpers to configure cross-origin resource sharing
// (CORS) for Connect servers.
package cors

// AllowedMethods returns the allowed HTTP methods that scripts running in the
// browser are permitted to use.
//
// To support cross-domain requests with the protocols supported by Connect,
// these headers fields must be included in the preflight response header
// Access-Control-Allow-Methods.
func AllowedMethods() []string {
	return []string{
		"GET",  // for Connect
		"POST", // for all protocols
	}
}

// AllowedHeaders returns the headers that scripts running in the browser send
// when making RPC requests. To support cross-domain requests with the
// protocols supported by Connect, these field names must be included in the
// Access-Control-Allow-Headers header of the preflight response.
//
// When configuring CORS, make sure to also include any application-specific
// headers your server expects to receive from the browser.
func AllowedHeaders() []string {
	return []string{
		"Content-Type",             // for all protocols
		"Connect-Protocol-Version", // for Connect
		"Connect-Timeout-Ms",       // for Connect
		"Grpc-Timeout",             // for gRPC-web
		"X-Grpc-Web",               // for gRPC-web
		"X-User-Agent",             // for all protocols
	}
}

// ExposedHeaders returns the headers that scripts running in the
// browser expect to access when receiving RPC responses. To support
// cross-domain requests with the protocols supported by Connect, these field
// names must be included in the Access-Control-Expose-Headers header of the
// actual response.
//
// When configuring CORS, make sure to also include any application-specific
// headers your server expects to send to the browser. If your application uses
// trailers, they will be sent as headers with a `Trailer-` prefix for
// unary Connect RPCs - make sure to expose them!
func ExposedHeaders() []string {
	return []string{
		"Grpc-Status",             // for gRPC-web
		"Grpc-Message",            // for gRPC-web
		"Grpc-Status-Details-Bin", // for gRPC-web
	}
}
