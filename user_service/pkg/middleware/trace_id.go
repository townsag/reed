package middleware

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"go.opentelemetry.io/otel/trace"
)

type contextKey string
const (
	traceIdKey contextKey = "x-reed-request-id"
	traceIdUnknown string = "unknown"
)

func TraceIdInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, 
		req any, 
		info *grpc.UnaryServerInfo, 
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		span := trace.SpanFromContext(ctx)
		if span.SpanContext().HasTraceID() {
			traceId := span.SpanContext().TraceID()
			grpc.SetHeader(ctx, metadata.Pairs(string(traceIdKey), uuid.UUID(traceId).String()))
		} else {
			grpc.SetHeader(ctx, metadata.Pairs(string(traceIdKey), traceIdUnknown))
		}
		return handler(ctx, req)
	}
}