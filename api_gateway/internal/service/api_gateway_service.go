package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/townsag/reed/api_gateway/internal/config"
	"github.com/townsag/reed/api_gateway/internal/server"
	"github.com/townsag/reed/api_gateway/internal/util"
	userService "github.com/townsag/reed/user_service/pkg/client"
)

// should this generic function should also be able to take any of the returned service level
// errors as an input?


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

type CustomClaims struct {
	UserName string `json:"userName"`
	jwt.RegisteredClaims
	// ^this is called struct embedding, it adds all the fields from the jwt registered claims
    // struct to the custom claims struct. They can be accessed as if they were elements of 
    // the CustomClaims struct
}

// get a token
func (s *Service) PostAuthLogin(w http.ResponseWriter, r *http.Request) {
	// deserialize the request body from a json string, use the request body struct that is generated
	// the the oapi-gen tool, validate that the username and password are not empty at the openapi spec level
	var reqBody server.PostAuthLoginJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		util.SendError(w, http.StatusBadRequest, fmt.Sprintf("error decoding request body: %s", err.Error()))
		return
	}
	// use the users service client to validate the credentials
	userId, isValid, err := s.userServiceClient.ValidatePassword(
		r.Context(), reqBody.UserName, reqBody.Password,
	)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			// if the user is missing, send a 400 error
			util.SendError(w, http.StatusNotFound, fmt.Sprintf("no user found with username: %v", reqBody.UserName))
			return
		} else {
			util.SendError(w, util.GrpcToHttpStatus(err), err.Error())
			return
		}
	}
	// if the credentials are invalid, send a 401 error
	if !isValid {
		util.SendError(w, http.StatusUnauthorized, "the provided username and password did not match")
	}
	// if the credentials are valid, construct a token that includes the username and a generic scope
	// us the golang-jwt library to make a token, maybe put this part in a package 
	token := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		CustomClaims{
			UserName: reqBody.UserName,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer: "reed",
				Subject: userId.String(),
				IssuedAt: jwt.NewNumericDate(time.Now()),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute * 60)),
			},
		},
	)
	signedToken, err := token.SignedString([]byte(config.JWTSecretKey))
	if err != nil {
		util.SendError(w, http.StatusInternalServerError, err.Error())
	}
	// return a 200 response with the validated token
	util.SendJsonResponse(
		w, http.StatusOK, &server.LoginResponse{
			ExpiresIn: 1234,
			Token: signedToken,
		},
	)
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
		util.SendError(w, http.StatusBadRequest, fmt.Sprintf("error decoding request body: %s", err.Error()))
		return
	}
	// perform any application level request validation
	if err := reqBody.Validate(); err != nil {
		util.SendError(w, http.StatusBadRequest, err.Error())
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
		util.SendError(w, util.GrpcToHttpStatus(err), err.Error())
	}
	// return the userId that is returned by the gRPC client
	// only the UserId field of the create user reply struct is exported so we 
	// can directly encode the service reply
	util.SendJsonResponse(w, http.StatusCreated, serviceReply)
}

// deactivate a user
func (s *Service) DeleteUserUserId(w http.ResponseWriter, r *http.Request, userId server.UserId) {
	// there is no request body to validate
	// call the user microservice to deactivate this user
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	err := s.userServiceClient.DeactivateUser(ctx, userId)
	if err != nil {
		util.SendError(w, util.GrpcToHttpStatus(err), err.Error())
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
		util.SendError(w, util.GrpcToHttpStatus(err), err.Error())
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
	util.SendJsonResponse(w, http.StatusOK, response)
}

// update a user including the users password
func (s *Service) PutUserUserId(w http.ResponseWriter, r *http.Request, userId server.UserId) {
	var reqBody server.PutUserUserIdJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		util.SendError(w, http.StatusBadRequest, fmt.Sprintf("error when decoding the request body: %s", err.Error()))
	}
	// now that we have successfully decoded the json body we need to call the user service 
	ctx, cancel := context.WithTimeout(r.Context(), config.TIMEOUT_MILLISECONDS)
	defer cancel()
	err = s.userServiceClient.ChangeUserPassword(ctx, userId, reqBody.OldPassword, reqBody.NewPassword)
	if err != nil {
		util.SendError(w, util.GrpcToHttpStatus(err), err.Error())
	}
	w.WriteHeader(http.StatusNoContent)
}