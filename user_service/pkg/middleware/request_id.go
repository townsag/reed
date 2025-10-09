package middleware

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type contextKey string
const (
	requestIdKey contextKey = "x-reed-request-id"
	requestIdUnknown string = "unknown"
)


func RequestIdInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context, 
		req any, 
		info *grpc.UnaryServerInfo, 
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		// parse the existing request id from the request metadata or conditionally generate a new request id
		requestId := parseOrGenerateId(ctx)
		// add the request_id to the context
		ctx = context.WithValue(ctx, requestIdKey, requestId)
		// add the request id to the grpc metadata so it can be accessed by the client
		grpc.SetHeader(ctx, metadata.Pairs(string(requestIdKey), requestId))
		return handler(ctx, req)
	}
}


func parseOrGenerateId(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := md.Get(string(requestIdKey)); len(ids) > 0 {
			return ids[0]
		}
	}
	return uuid.NewString()
}

func GetRequestId(ctx context.Context) string {
	if id, ok := ctx.Value(requestIdKey).(string); ok {
		return id
	}
	return requestIdUnknown
}