package service

import (
	"net/http"
	"encoding/json"

	"github.com/townsag/reed/api_gateway/internal/server"
	userService "github.com/townsag/reed/user_service/pkg/client"
)

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

func sendError(w http.ResponseWriter, code int, message string) {
	responseError := server.Error{
		Message: &message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(responseError)
}

/*
CHECKPOINT:
- you were updating the post user method to include proper deserialization 
  and error handling code
- you were referencing the pet store example from the oapi-codegen repo
- you were also adding the grpc client as a dependency of the api gateway
  server struct
	- this includes adding it to the struct, creating it in the main.go file
	- passing it down to the service struct

*/

// Create a User
func (s *Service) PostUser(w http.ResponseWriter, r *http.Request) {
	// assume that the request body is well formed with regard to api spec because of the 
	// request validation middleware
	// deserialize the request body to json using the encoding/json decoder
	// use some generic function to send an error on failing to unmarshal the json
	// the generic function should also be able to take any of the returned service level
	// errors as an input
	// perform any application level request validation

	// call the user microservice with the gRPC client 
	// return the userId that is returned by the gRPC client
	// ensure that we are using proper error handling the entire time
}