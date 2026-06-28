package dispatcher

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/respond"
)

// statusClientClosedRequest is Nginx's non-standard 499 convention for a
// client closing the request before the server writes a response. net/http
// intentionally has no constant for it because it is not an IETF status code.
const statusClientClosedRequest = 499

// StatusClientClosedRequest exposes the non-standard 499 status for callers
// outside this package that need to reject on a client-aborted request while
// preserving the shared error-code contract.
const StatusClientClosedRequest = statusClientClosedRequest

// IsRequestContextError reports whether err represents a client-aborted request
// or a request deadline being exceeded.
func IsRequestContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// Bind adapts a typed handler into an http.HandlerFunc using route options,
// input binders, and the shared envelope/logging rules.
func Bind[Auth any, Req, Resp proto.Message](
	where string,
	input Input[Req],
	handler func(*RequestContext[Auth], Req) (Result[Resp], error),
	options ...BindOption[Req],
) http.HandlerFunc {
	cfg := ptrext.Of(bindConfig[Req]{binders: []Binder[Req]{input.bind}})
	for _, opt := range options {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.auth == nil {
		panic("dispatcher: Bind requires WithAuth")
	}
	if !cfg.authType.AssignableTo(typeOf[Auth]()) {
		panic("dispatcher: WithAuth type does not match handler auth")
	}
	for _, before := range cfg.before {
		if !cfg.authType.AssignableTo(before.authType) {
			panic("dispatcher: WithBefore auth type does not match WithAuth")
		}
	}
	return bind(where, input.new, handler, cfg)
}

// BindOption configures one dispatcher phase for a route. Options are request
// typed because binding/pre-handler behavior depends on the proto request; auth
// is only typed at the option constructor and the final handler boundary.
type BindOption[Req proto.Message] func(*bindConfig[Req])

type authenticator[Req proto.Message] func(*http.Request, Req) (any, error)

type bindConfig[Req proto.Message] struct {
	binders  []Binder[Req]
	auth     authenticator[Req]
	authType reflect.Type
	before   []beforeHandler[Req]
}

// WithAuth sets the route authenticator. The authenticator receives the raw
// request and typed proto request, so ordinary context-auth routes and
// body-signed routes use the same option surface.
func WithAuth[Auth any, Req proto.Message](authFn func(*http.Request, Req) (Auth, error)) BindOption[Req] {
	return func(cfg *bindConfig[Req]) {
		if cfg.auth != nil {
			panic("dispatcher: auth option already set")
		}
		cfg.auth = func(r *http.Request, req Req) (any, error) {
			return authFn(r, req)
		}
		cfg.authType = typeOf[Auth]()
	}
}

// BeforeHandler runs after request binding/auth and before the endpoint
// handler. It is useful for shared dispatcher-level checks that need typed auth
// + request context but should still emit through the standard error path.
type BeforeHandler[Auth any, Req proto.Message] func(*RequestContext[Auth], Req) error

type beforeHandler[Req proto.Message] struct {
	authType reflect.Type
	run      func(context.Context, any, http.ResponseWriter, Req) error
}

// WithBefore appends dispatcher-owned pre-handler checks that run after
// binding/auth and before the endpoint handler.
func WithBefore[Auth any, Req proto.Message](checks ...BeforeHandler[Auth, Req]) BindOption[Req] {
	return func(cfg *bindConfig[Req]) {
		for _, check := range checks {
			if check == nil {
				continue
			}
			cfg.before = append(cfg.before, beforeHandler[Req]{
				authType: typeOf[Auth](),
				run: func(ctx context.Context, auth any, w http.ResponseWriter, req Req) error {
					typed, ok := auth.(Auth)
					if !ok {
						return authTypeMismatchError[Auth]()
					}
					return check(ptrext.Of(RequestContext[Auth]{Context: ctx, Auth: typed, response: w}), req)
				},
			})
		}
	}
}

// WithBinders appends binders after the Input's binder. A route that needs auth
// to own body parsing can pass Empty(...) as its Input and do the parsing from
// WithAuth.
func WithBinders[Req proto.Message](binders ...Binder[Req]) BindOption[Req] {
	return func(cfg *bindConfig[Req]) {
		cfg.binders = append(cfg.binders, binders...)
	}
}

func bind[Auth any, Req, Resp proto.Message](
	where string,
	newReq func() Req,
	handler func(*RequestContext[Auth], Req) (Result[Resp], error),
	cfg *bindConfig[Req],
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		start := time.Now()

		logext.Infof(ctx, "[%s] start,method:%s,path:%s", where, r.Method, r.URL.Path)

		req := newReq()
		if err := runBinders(r, req, cfg.binders...); err != nil {
			writeBindError(ctx, w, err, where, start)
			return
		}
		auth, err := runAuth[Auth](cfg.auth, r, req)
		if err != nil {
			writeBindError(ctx, w, err, where, start)
			return
		}

		rc := ptrext.Of(RequestContext[Auth]{Context: ctx, Auth: auth, response: w, request: r})
		if err := runBefore(ctx, auth, w, req, cfg.before); err != nil {
			writeHandlerError(ctx, w, err, where, start)
			return
		}
		result, err := handler(rc, req)
		if err != nil {
			writeHandlerError(ctx, w, err, where, start)
			return
		}
		writeSuccess(ctx, w, result, where, start)
	}
}

func runAuth[Auth any, Req proto.Message](authFn authenticator[Req], r *http.Request, req Req) (Auth, error) {
	var zero Auth
	auth, err := authFn(r, req)
	if err != nil {
		return zero, err
	}
	typed, ok := auth.(Auth)
	if !ok {
		return zero, authTypeMismatchError[Auth]()
	}
	return typed, nil
}

func authTypeMismatchError[Auth any]() error {
	return NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL,
		"dispatcher auth option type mismatch")
}

func typeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

func runBinders[Req proto.Message](r *http.Request, req Req, binders ...Binder[Req]) error {
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

func runBefore[Auth any, Req proto.Message](
	ctx context.Context,
	auth Auth,
	w http.ResponseWriter,
	req Req,
	checks []beforeHandler[Req],
) error {
	for _, check := range checks {
		if err := check.run(ctx, auth, w, req); err != nil {
			return err
		}
	}
	return nil
}

// HealthzHandler writes the dispatcher-owned plain text liveness response.
func HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteText(w, http.StatusOK, "ok")
	}
}

// WriteText writes a dispatcher-owned plain text response for non-proto routes
// such as Kubernetes health probes.
func WriteText(w http.ResponseWriter, status int, body string) {
	writeText(w, status, "text/plain; charset=utf-8", body)
}

// Reject writes a standard error envelope for middleware and other pre-handler
// paths that cannot be expressed as a full Bind route. Middleware owns
// branch-specific rejection logging; Reject only owns the envelope write.
func Reject(ctx context.Context, w http.ResponseWriter, status int, code attunev1.ErrorCode, message string) {
	respond.Error(ctx, w, status, code, message)
}

func writeText(w http.ResponseWriter, status int, contentType, body string) {
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func writeSuccess[Resp proto.Message](ctx context.Context, w http.ResponseWriter, result Result[Resp], where string, start time.Time) {
	if result.Status == 0 {
		result.Status = http.StatusOK
	}
	if result.Status != http.StatusNoContent {
		respond.Proto(w, result.Status, result.Body)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
	logext.Infof(ctx, "[%s] OK,status:%d,latency_ms:%d", where, result.Status, time.Since(start).Milliseconds())
}

func writeBindError(ctx context.Context, w http.ResponseWriter, err error, where string, start time.Time) {
	var typed *Error
	if errors.As(err, &typed) {
		writeHandlerError(ctx, w, err, where, start)
		return
	}
	writeDecodeError(ctx, w, err, where)
}

func writeDecodeError(ctx context.Context, w http.ResponseWriter, err error, where string) {
	status := http.StatusBadRequest
	code := attunev1.ErrorCode_BAD_REQUEST
	msg := "request body is not valid JSON"
	if errors.Is(err, ErrBodyTooLarge) {
		status = http.StatusRequestEntityTooLarge
		code = attunev1.ErrorCode_BODY_TOO_LARGE
		msg = "request body exceeds the size limit"
	}
	logext.Warnf(ctx, "[%s] reject: decode failed,status:%d,code:%s,err:%v", where, status, code, err)
	respond.Error(ctx, w, status, code, msg)
}

func writeHandlerError(ctx context.Context, w http.ResponseWriter, err error, where string, start time.Time) {
	if errors.Is(err, context.Canceled) {
		logext.Warnf(ctx, "[%s] canceled,latency_ms:%d", where, time.Since(start).Milliseconds())
		respond.Error(ctx, w, statusClientClosedRequest, attunev1.ErrorCode_CLIENT_CANCELED, "client canceled request")
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		logext.Warnf(ctx, "[%s] deadline exceeded,latency_ms:%d", where, time.Since(start).Milliseconds())
		respond.Error(ctx, w, http.StatusGatewayTimeout, attunev1.ErrorCode_DEADLINE_EXCEEDED, "request deadline exceeded")
		return
	}
	var typed *Error
	if errors.As(err, &typed) {
		if typed.Status >= http.StatusInternalServerError {
			logext.Errorf(ctx, "[%s] internal,status:%d,code:%s,latency_ms:%d", where, typed.Status, typed.Code, time.Since(start).Milliseconds())
		} else {
			logext.Warnf(ctx, "[%s] reject,status:%d,code:%s,latency_ms:%d", where, typed.Status, typed.Code, time.Since(start).Milliseconds())
		}
		respond.Error(ctx, w, typed.Status, typed.Code, typed.Message)
		return
	}
	logext.Errorf(ctx, "[%s] internal,err:%+v,latency_ms:%d", where, err, time.Since(start).Milliseconds())
	respond.Error(ctx, w, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "internal server error")
}
