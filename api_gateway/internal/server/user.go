package server

import (
	"net/http"
	"context"
	"encoding/json"
	"fmt"

	"github.com/townsag/reed/api_gateway/internal/config"
)

// Create a User
func (s *Service) PostUser(w http.ResponseWriter, r *http.Request) {
	// assume that the request body is well formed with regard to api spec because of the 
	// request validation middleware
	// deserialize the request body to json using the encoding/json decoder, use the 
	// request body that is generated for this route by oapi codegen
	var reqBody PostUserJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		// use a generic function to send an error on failing to unmarshal the json
		SendError(w, http.StatusBadRequest, fmt.Sprintf("error decoding request body: %s", err.Error()))
		return
	}
	// perform any application level request validation
	if err := reqBody.Validate(); err != nil {
		SendError(w, http.StatusBadRequest, err.Error())
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
		SendError(w, GrpcToHttpStatus(err), err.Error())
	}
	// return the userId that is returned by the gRPC client
	// only the UserId field of the create user reply struct is exported so we 
	// can directly encode the service reply
	SendJsonResponse(w, http.StatusCreated, serviceReply)
}

// deactivate a user
func (s *Service) DeleteUserUserId(w http.ResponseWriter, r *http.Request, userId UserId) {
	// there is no request body to validate
	// call the user microservice to deactivate this user
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	err := s.userServiceClient.DeactivateUser(ctx, userId)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}

// get a user
func (s *Service) GetUserUserId(w http.ResponseWriter, r *http.Request, userId UserId) {
	// call the user microservice to get this user
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	serviceReply, err := s.userServiceClient.GetUser(ctx, userId)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
	}
	// ignore the returned user id, we don't have to parse it because it 
	// will be the same as the calling user id 
	// format the response into a user struct
	response := &User{
		Email: serviceReply.User.Email,
		MaxDocuments: serviceReply.User.MaxDocuments,
		UserId: userId,
		UserName: serviceReply.User.UserName,
	}
	// return the User object to the client
	SendJsonResponse(w, http.StatusOK, response)
}

// update a user including the users password
func (s *Service) PutUserUserId(w http.ResponseWriter, r *http.Request, userId UserId) {
	var reqBody PutUserUserIdJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		SendError(w, http.StatusBadRequest, fmt.Sprintf("error when decoding the request body: %s", err.Error()))
	}
	// now that we have successfully decoded the json body we need to call the user service 
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	err = s.userServiceClient.ChangeUserPassword(ctx, userId, reqBody.OldPassword, reqBody.NewPassword)
	if err != nil {
		SendError(w, GrpcToHttpStatus(err), err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}