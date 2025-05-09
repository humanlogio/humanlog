package localsvc

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	otlplogssvcpb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	otlpmetricssvcpb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	otlpprofilevcpb "go.opentelemetry.io/proto/otlp/collector/profiles/v1development"
	otlptracesvcpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// everything that follows is largely lifted from:
// - github.com/open-telemetry/opentelemetry-collector/receiver/otlpreceiver

var fallbackMsg = []byte(`{"code": 13, "message": "failed to marshal error message"}`)

const fallbackContentType = "application/json"

func readContentType(resp http.ResponseWriter, req *http.Request) (encoder, bool) {
	if req.Method != http.MethodPost {
		handleUnmatchedMethod(resp)
		return nil, false
	}

	switch getMimeTypeFromContentType(req.Header.Get("Content-Type")) {
	case pbContentType:
		return pbEncoder, true
	case jsonContentType:
		return jsEncoder, true
	default:
		handleUnmatchedContentType(resp)
		return nil, false
	}
}

func readAndCloseBody(resp http.ResponseWriter, req *http.Request, enc encoder) ([]byte, bool) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeError(resp, enc, err, http.StatusBadRequest)
		return nil, false
	}
	if err = req.Body.Close(); err != nil {
		writeError(resp, enc, err, http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// writeError encodes the HTTP error inside a rpc.Status message as required by the OTLP protocol.
func writeError(w http.ResponseWriter, encoder encoder, err error, statusCode int) {
	s, ok := status.FromError(err)
	if ok {
		statusCode = getHTTPStatusCodeFromStatus(s)
	} else {
		s = newStatusFromMsgAndHTTPCode(err.Error(), statusCode)
	}
	writeStatusResponse(w, encoder, statusCode, s)
}

// errorHandler encodes the HTTP error message inside a rpc.Status message as required
// by the OTLP protocol.
func errorHandler(w http.ResponseWriter, r *http.Request, errMsg string, statusCode int) {
	s := newStatusFromMsgAndHTTPCode(errMsg, statusCode)
	switch getMimeTypeFromContentType(r.Header.Get("Content-Type")) {
	case pbContentType:
		writeStatusResponse(w, pbEncoder, statusCode, s)
		return
	case jsonContentType:
		writeStatusResponse(w, jsEncoder, statusCode, s)
		return
	}
	writeResponse(w, fallbackContentType, http.StatusInternalServerError, fallbackMsg)
}

func writeStatusResponse(w http.ResponseWriter, enc encoder, statusCode int, st *status.Status) {
	// https://github.com/open-telemetry/opentelemetry-proto/blob/main/docs/specification.md#otlphttp-throttling
	if statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable {
		retryInfo := getRetryInfo(st)
		// Check if server returned throttling information.
		if retryInfo != nil {
			// We are throttled. Wait before retrying as requested by the server.
			// The value of Retry-After field can be either an HTTP-date or a number of
			// seconds to delay after the response is received. See https://datatracker.ietf.org/doc/html/rfc7231#section-7.1.3
			//
			// Retry-After = HTTP-date / delay-seconds
			//
			// Use delay-seconds since is easier to format as well as does not require clock synchronization.
			w.Header().Set("Retry-After", strconv.FormatInt(int64(retryInfo.GetRetryDelay().AsDuration()/time.Second), 10))
		}
	}
	msg, err := enc.marshalStatus(st.Proto())
	if err != nil {
		writeResponse(w, fallbackContentType, http.StatusInternalServerError, fallbackMsg)
		return
	}

	writeResponse(w, enc.contentType(), statusCode, msg)
}

func handleUnmatchedContentType(resp http.ResponseWriter) {
	hst := http.StatusUnsupportedMediaType
	writeResponse(resp, "text/plain", hst, []byte(fmt.Sprintf("%v unsupported media type, supported: [%s, %s]", hst, jsonContentType, pbContentType)))
}

func writeResponse(w http.ResponseWriter, contentType string, statusCode int, msg []byte) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	// Nothing we can do with the error if we cannot write to the response.
	_, _ = w.Write(msg)
}

func getMimeTypeFromContentType(contentType string) string {
	mediatype, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return mediatype
}

func handleUnmatchedMethod(resp http.ResponseWriter) {
	hst := http.StatusMethodNotAllowed
	writeResponse(resp, "text/plain", hst, []byte(fmt.Sprintf("%v method not allowed, supported: [POST]", hst)))
}

// newStatusFromMsgAndHTTPCode returns a gRPC status based on an error message string and a http status code.
// This function is shared between the http receiver and http exporter for error propagation.
func newStatusFromMsgAndHTTPCode(errMsg string, statusCode int) *status.Status {
	var c codes.Code
	// Mapping based on https://github.com/grpc/grpc/blob/master/doc/http-grpc-status-mapping.md
	// 429 mapping to ResourceExhausted and 400 mapping to StatusBadRequest are exceptions.
	switch statusCode {
	case http.StatusBadRequest:
		c = codes.InvalidArgument
	case http.StatusUnauthorized:
		c = codes.Unauthenticated
	case http.StatusForbidden:
		c = codes.PermissionDenied
	case http.StatusNotFound:
		c = codes.Unimplemented
	case http.StatusTooManyRequests:
		c = codes.ResourceExhausted
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		c = codes.Unavailable
	default:
		c = codes.Unknown
	}
	return status.New(c, errMsg)
}

func getHTTPStatusCodeFromStatus(s *status.Status) int {
	// See https://github.com/open-telemetry/opentelemetry-proto/blob/main/docs/specification.md#failures
	// to see if a code is retryable.
	// See https://github.com/open-telemetry/opentelemetry-proto/blob/main/docs/specification.md#failures-1
	// to see a list of retryable http status codes.
	switch s.Code() {
	// Retryable
	case codes.Canceled, codes.DeadlineExceeded, codes.Aborted, codes.OutOfRange, codes.Unavailable, codes.DataLoss:
		return http.StatusServiceUnavailable
	// Retryable
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	// Not Retryable
	case codes.InvalidArgument:
		return http.StatusBadRequest
	// Not Retryable
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	// Not Retryable
	case codes.PermissionDenied:
		return http.StatusForbidden
	// Not Retryable
	case codes.Unimplemented:
		return http.StatusNotFound
	// Not Retryable
	default:
		return http.StatusInternalServerError
	}
}

func getRetryInfo(status *status.Status) *errdetails.RetryInfo {
	for _, detail := range status.Details() {
		if t, ok := detail.(*errdetails.RetryInfo); ok {
			return t
		}
	}
	return nil
}

const (
	pbContentType   = "application/x-protobuf"
	jsonContentType = "application/json"
)

type encoder interface {
	unmarshalTracesRequest(buf []byte) (*otlptracesvcpb.ExportTraceServiceRequest, error)
	unmarshalMetricsRequest(buf []byte) (*otlpmetricssvcpb.ExportMetricsServiceRequest, error)
	unmarshalLogsRequest(buf []byte) (*otlplogssvcpb.ExportLogsServiceRequest, error)
	unmarshalProfilesRequest(buf []byte) (*otlpprofilevcpb.ExportProfilesServiceRequest, error)

	marshalTracesResponse(*otlptracesvcpb.ExportTraceServiceResponse) ([]byte, error)
	marshalMetricsResponse(*otlpmetricssvcpb.ExportMetricsServiceResponse) ([]byte, error)
	marshalLogsResponse(*otlplogssvcpb.ExportLogsServiceResponse) ([]byte, error)
	marshalProfilesResponse(*otlpprofilevcpb.ExportProfilesServiceResponse) ([]byte, error)

	marshalStatus(rsp *spb.Status) ([]byte, error)

	contentType() string
}

var (
	pbEncoder       = &protoEncoder{}
	jsEncoder       = &jsonEncoder{}
	jsonPbMarshaler = &protojson.MarshalOptions{}
)

type protoEncoder struct{}

func (protoEncoder) unmarshalTracesRequest(buf []byte) (*otlptracesvcpb.ExportTraceServiceRequest, error) {
	req := new(otlptracesvcpb.ExportTraceServiceRequest)
	return req, proto.Unmarshal(buf, req)
}

func (protoEncoder) unmarshalMetricsRequest(buf []byte) (*otlpmetricssvcpb.ExportMetricsServiceRequest, error) {
	req := new(otlpmetricssvcpb.ExportMetricsServiceRequest)
	return req, proto.Unmarshal(buf, req)
}

func (protoEncoder) unmarshalLogsRequest(buf []byte) (*otlplogssvcpb.ExportLogsServiceRequest, error) {
	req := new(otlplogssvcpb.ExportLogsServiceRequest)
	return req, proto.Unmarshal(buf, req)
}

func (protoEncoder) unmarshalProfilesRequest(buf []byte) (*otlpprofilevcpb.ExportProfilesServiceRequest, error) {
	req := new(otlpprofilevcpb.ExportProfilesServiceRequest)
	return req, proto.Unmarshal(buf, req)
}

func (protoEncoder) marshalTracesResponse(resp *otlptracesvcpb.ExportTraceServiceResponse) ([]byte, error) {
	return proto.Marshal(resp)
}

func (protoEncoder) marshalMetricsResponse(resp *otlpmetricssvcpb.ExportMetricsServiceResponse) ([]byte, error) {
	return proto.Marshal(resp)
}

func (protoEncoder) marshalLogsResponse(resp *otlplogssvcpb.ExportLogsServiceResponse) ([]byte, error) {
	return proto.Marshal(resp)
}

func (protoEncoder) marshalProfilesResponse(resp *otlpprofilevcpb.ExportProfilesServiceResponse) ([]byte, error) {
	return proto.Marshal(resp)
}

func (protoEncoder) marshalStatus(resp *spb.Status) ([]byte, error) {
	return proto.Marshal(resp)
}

func (protoEncoder) contentType() string {
	return pbContentType
}

type jsonEncoder struct{}

func (jsonEncoder) unmarshalTracesRequest(buf []byte) (*otlptracesvcpb.ExportTraceServiceRequest, error) {
	req := new(otlptracesvcpb.ExportTraceServiceRequest)
	return req, protojson.Unmarshal(buf, req)
}

func (jsonEncoder) unmarshalMetricsRequest(buf []byte) (*otlpmetricssvcpb.ExportMetricsServiceRequest, error) {
	req := new(otlpmetricssvcpb.ExportMetricsServiceRequest)
	return req, protojson.Unmarshal(buf, req)
}

func (jsonEncoder) unmarshalLogsRequest(buf []byte) (*otlplogssvcpb.ExportLogsServiceRequest, error) {
	req := new(otlplogssvcpb.ExportLogsServiceRequest)
	return req, protojson.Unmarshal(buf, req)
}

func (jsonEncoder) unmarshalProfilesRequest(buf []byte) (*otlpprofilevcpb.ExportProfilesServiceRequest, error) {
	req := new(otlpprofilevcpb.ExportProfilesServiceRequest)
	return req, protojson.Unmarshal(buf, req)
}

func (jsonEncoder) marshalTracesResponse(resp *otlptracesvcpb.ExportTraceServiceResponse) ([]byte, error) {
	return jsonPbMarshaler.Marshal(resp)
}

func (jsonEncoder) marshalMetricsResponse(resp *otlpmetricssvcpb.ExportMetricsServiceResponse) ([]byte, error) {
	return jsonPbMarshaler.Marshal(resp)
}

func (jsonEncoder) marshalLogsResponse(resp *otlplogssvcpb.ExportLogsServiceResponse) ([]byte, error) {
	return jsonPbMarshaler.Marshal(resp)
}

func (jsonEncoder) marshalProfilesResponse(resp *otlpprofilevcpb.ExportProfilesServiceResponse) ([]byte, error) {
	return jsonPbMarshaler.Marshal(resp)
}

func (jsonEncoder) marshalStatus(resp *spb.Status) ([]byte, error) {
	return jsonPbMarshaler.Marshal(resp)
}

func (jsonEncoder) contentType() string {
	return jsonContentType
}
