package server

import (
	"net/http"
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SendError(w http.ResponseWriter, code int, message string) {
	responseError := Error{
		Message: &message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(responseError)
}
// Decide that each method should implement it's own version of serializing the successful

func SendJsonResponse(w http.ResponseWriter, code int, responseBody interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(responseBody)
}

func GrpcToHttpStatus(err error) int {
    st, ok := status.FromError(err)
    if !ok {
        return http.StatusInternalServerError
    }
    
    switch st.Code() {
    case codes.InvalidArgument:
        return http.StatusBadRequest
    case codes.NotFound:
        return http.StatusNotFound
    case codes.AlreadyExists:
        return http.StatusConflict
    case codes.PermissionDenied:
        return http.StatusForbidden
    case codes.Unauthenticated:
        return http.StatusUnauthorized
    case codes.ResourceExhausted:
        return http.StatusTooManyRequests
    case codes.Unimplemented:
        return http.StatusNotImplemented
    case codes.Unavailable:
        return http.StatusServiceUnavailable
    case codes.DeadlineExceeded:
        return http.StatusGatewayTimeout
    default:
        return http.StatusInternalServerError
    }
}