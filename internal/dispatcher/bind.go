package dispatcher

import (
	"context"
	"errors"
	"net/http"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/respond"
)

// Bind adapts a typed handler into an http.HandlerFunc using the supplied auth
// extractor, input binder, and the shared envelope/logging rules.
func Bind[Auth any, Req, Resp proto.Message](
	where string,
	authFn func(context.Context) Auth,
	input Input[Req],
	handler func(*RequestContext[Auth], Req) (Result[Resp], error),
) http.HandlerFunc {
	return bind(where, authFn, input.new, input.bind, handler)
}

func bind[Auth any, Req, Resp proto.Message](
	where string,
	authFn func(context.Context) Auth,
	newReq func() Req,
	bindReq func(*http.Request, Req) error,
	handler func(*RequestContext[Auth], Req) (Result[Resp], error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		start := time.Now()
		auth := authFn(ctx)
		rc := ptrext.Of(RequestContext[Auth]{Context: ctx, Auth: auth, response: w})

		logext.Infof(ctx, "[%s] start,method:%s,path:%s", where, r.Method, r.URL.Path)

		req := newReq()
		if bindReq != nil {
			if err := bindReq(r, req); err != nil {
				writeBindError(ctx, w, err, where, start)
				return
			}
		}

		result, err := handler(rc, req)
		if err != nil {
			writeHandlerError(ctx, w, err, where, start)
			return
		}
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
		msg = "request body exceeds the 1 MiB limit"
	}
	logext.Warnf(ctx, "[%s] reject: decode failed,err:%v", where, err)
	respond.Error(ctx, w, status, code, msg)
}

func writeHandlerError(ctx context.Context, w http.ResponseWriter, err error, where string, start time.Time) {
	if errors.Is(err, context.Canceled) {
		logext.Warnf(ctx, "[%s] canceled,latency_ms:%d", where, time.Since(start).Milliseconds())
		respond.Error(ctx, w, 499, attunev1.ErrorCode_CLIENT_CANCELED, "client canceled request")
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
