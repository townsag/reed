package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

	"github.com/townsag/reed/api_gateway/internal/config"
	"github.com/townsag/reed/api_gateway/internal/server"
	userService "github.com/townsag/reed/user_service/pkg/client"
)

// should this generic function should also be able to take any of the returned service level
// errors as an input?
func sendError(w http.ResponseWriter, code int, message string) {
	responseError := server.Error{
		Message: &message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(responseError)
}
// Decide that each method should implement it's own version of serializing the successful

func sendJsonResponse(w http.ResponseWriter, code int, responseBody interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(responseBody)
}

func grpcToHttpStatus(err error) int {
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

var _ server.ServerInterface = (*Service)(nil)

type Service struct {
	userServiceClient *userService.UserServiceClient
	// probably also add a client for accessing some external state like a cache or a 
	// way to record request counts 
}

func NewService(usClient *userService.UserServiceClient) Service {
	return Service{
		userServiceClient: usClient,
	}
}

// Create a User
func (s *Service) PostUser(w http.ResponseWriter, r *http.Request) {
	// assume that the request body is well formed with regard to api spec because of the 
	// request validation middleware
	// deserialize the request body to json using the encoding/json decoder, use the 
	// request body that is generated for this route by oapi codegen
	var reqBody server.PostUserJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		// use a generic function to send an error on failing to unmarshal the json
		sendError(w, http.StatusBadRequest, fmt.Sprintf("error decoding request body: %s", err.Error()))
		return
	}
	// perform any application level request validation
	if err := reqBody.Validate(); err != nil {
		sendError(w, http.StatusBadRequest, err.Error())
	}
	// call the user microservice with the gRPC client
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	serviceReply, err := s.userServiceClient.CreateUser(
		ctx,
		reqBody.UserName,
		string(reqBody.UserEmail),
		reqBody.Password,
		reqBody.MaxDocuments,
	)
	if err != nil {
		sendError(w, grpcToHttpStatus(err), err.Error())
	}
	// return the userId that is returned by the gRPC client
	// only the UserId field of the create user reply struct is exported so we 
	// can directly encode the service reply
	sendJsonResponse(w, http.StatusCreated, serviceReply)
}

// deactivate a user
func (s *Service) DeleteUserUserId(w http.ResponseWriter, r *http.Request, userId server.UserId) {
	// there is no request body to validate
	// call the user microservice to deactivate this user
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	err := s.userServiceClient.DeactivateUser(ctx, userId)
	if err != nil {
		sendError(w, grpcToHttpStatus(err), err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}

// get a user
func (s *Service) GetUserUserId(w http.ResponseWriter, r *http.Request, userId server.UserId) {
	// call the user microservice to get this user
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	serviceReply, err := s.userServiceClient.GetUser(ctx, userId)
	if err != nil {
		sendError(w, grpcToHttpStatus(err), err.Error())
	}
	// ignore the returned user id, we don't have to parse it because it 
	// will be the same as the calling user id 
	// format the response into a user struct
	response := &server.User{
		Email: serviceReply.User.Email,
		MaxDocuments: serviceReply.User.MaxDocuments,
		UserId: userId,
		UserName: serviceReply.User.UserName,
	}
	// return the User object to the client
	sendJsonResponse(w, http.StatusOK, response)
}

// update a user including the users password
func (s *Service) PutUserUserId(w http.ResponseWriter, r *http.Request, userId server.UserId) {
	var reqBody server.PutUserUserIdJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		sendError(w, http.StatusBadRequest, fmt.Sprintf("error when decoding the request body: %s", err.Error()))
	}
	// now that we have successfully decoded the json body we need to call the user service 
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	err = s.userServiceClient.ChangeUserPassword(ctx, userId, reqBody.OldPassword, reqBody.NewPassword)
	if err != nil {
		sendError(w, grpcToHttpStatus(err), err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}