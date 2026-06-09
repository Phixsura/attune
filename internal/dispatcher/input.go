package dispatcher

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/proto"

	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Binder fills a typed request from the incoming HTTP request.
type Binder[Req proto.Message] func(*http.Request, Req) error

// Input describes how a route creates and binds its typed request.
type Input[Req proto.Message] struct {
	new  func() Req
	bind Binder[Req]
}

// Empty creates an input for endpoints without a request body or route params.
func Empty[Req proto.Message](newReq func() Req) Input[Req] {
	return Input[Req]{new: newReq}
}

// JSON creates an input that decodes the HTTP body as protoJSON.
func JSON[Req proto.Message](newReq func() Req) Input[Req] {
	return Input[Req]{new: newReq, bind: JSONBody[Req]}
}

// Combine creates an input from multiple binders, run in the order supplied.
func Combine[Req proto.Message](newReq func() Req, binders ...Binder[Req]) Input[Req] {
	return Custom(newReq, bindAll(binders...))
}

// Path creates an input for endpoints whose request is filled from route params.
func Path[Req proto.Message](newReq func() Req, binders ...Binder[Req]) Input[Req] {
	return Combine(newReq, binders...)
}

// Query creates an input for endpoints whose request is filled from query params.
func Query[Req proto.Message](newReq func() Req, binders ...Binder[Req]) Input[Req] {
	return Combine(newReq, binders...)
}

// Param copies a chi route param into the typed request.
func Param[Req proto.Message](name string, set func(Req, string)) Binder[Req] {
	return func(r *http.Request, req Req) error {
		set(req, chi.URLParam(r, name))
		return nil
	}
}

// ParamInt64 parses a chi route param into an int64 request field.
func ParamInt64[Req proto.Message](name string, set func(Req, int64), message string) Binder[Req] {
	return func(r *http.Request, req Req) error {
		v, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
		if err != nil {
			return NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, message)
		}
		set(req, v)
		return nil
	}
}

// Custom creates an input with endpoint-specific binding, usually for path
// params or mixed path/body requests.
func Custom[Req proto.Message](newReq func() Req, bindReq Binder[Req]) Input[Req] {
	return Input[Req]{new: newReq, bind: bindReq}
}

// JSONBody decodes a protoJSON request body with the shared dispatcher limit.
func JSONBody[Req proto.Message](r *http.Request, req Req) error {
	return DecodeJSON(r.Body, req)
}

func bindAll[Req proto.Message](binders ...Binder[Req]) Binder[Req] {
	return func(r *http.Request, req Req) error {
		for _, bind := range binders {
			if bind == nil {
				continue
			}
			if err := bind(r, req); err != nil {
				return err
			}
		}
		return nil
	}
}
