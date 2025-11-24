package middleware


import (
	"context"
	"log/slog"
	"fmt"

	"google.golang.org/grpc"
)

// create a simple interceptor that logs the request information like which rpc was called etc
func LoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		slog.DebugContext(ctx, fmt.Sprintf("received a call to method %s", info.FullMethod))
		return handler(ctx, req)
	}
}