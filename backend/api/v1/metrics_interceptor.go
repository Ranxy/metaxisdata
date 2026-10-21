package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// MetricsInterceptor records ConnectRPC calls. It is installed first in the
// interceptor chain, so it observes the codes Auth, Throttle and ACL refuse with
// as well as the mapped code ErrorMapping returns last.
type MetricsInterceptor struct {
	metrics *metrics.Metrics
}

// NewMetricsInterceptor creates the interceptor. A nil Metrics disables
// recording.
func NewMetricsInterceptor(m *metrics.Metrics) *MetricsInterceptor {
	return &MetricsInterceptor{metrics: m}
}

// WrapUnary records one unary call with its duration.
func (in *MetricsInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		started := time.Now()
		response, err := next(ctx, request)
		service, method := procedureParts(request.Spec().Procedure)
		in.metrics.ObserveRPCUnary(service, method, rpcCode(err), time.Since(started))
		return response, err
	}
}

// WrapStreamingClient returns next unchanged: clients are not this server's
// traffic.
func (*MetricsInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler records one finished server-streaming call.
func (in *MetricsInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		err := next(ctx, conn)
		service, method := procedureParts(conn.Spec().Procedure)
		in.metrics.ObserveRPCStream(service, method, rpcCode(err))
		return err
	}
}

// rpcCode renders the client-visible code. connect.CodeOf returns CodeUnknown
// for a nil error, so success is handled explicitly.
func rpcCode(err error) string {
	if err == nil {
		return "ok"
	}
	return connect.CodeOf(err).String()
}

// procedureParts splits "/metaxisdata.v1.InstanceService/SyncInstance" into the
// short service name and the method. Both are bounded by the generated services.
func procedureParts(procedure string) (string, string) {
	procedure = strings.TrimPrefix(procedure, "/")
	slash := strings.IndexByte(procedure, '/')
	if slash < 0 {
		return "unknown", "unknown"
	}
	service := procedure[:slash]
	if dot := strings.LastIndexByte(service, '.'); dot >= 0 {
		service = service[dot+1:]
	}
	return service, procedure[slash+1:]
}
