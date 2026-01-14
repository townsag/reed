package server

import (
	"net/http"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/townsag/reed/api_gateway/internal/config"
)


// TODO: at some point may want to factor authentication code out into its own package

// the differentiation between a user type claims and a guest type claims is implicit for now
// one can tell that they have a user type token if the token has a UserName
// this may not be the best way to do this but I will look into other approaches when I understand
// the encoding json package a bit better
type CustomClaims struct {
	UserName string `json:"userName"`
	jwt.RegisteredClaims
	// ^this is called struct embedding, it adds all the fields from the jwt registered claims
    // struct to the custom claims struct. They can be accessed as if they were elements of 
    // the CustomClaims struct
}

func (c CustomClaims) ParsePrincipalId() (uuid.UUID, error) {
	parsedId, err := uuid.Parse(c.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to parse the principal id from the JWT claims with error: %w", err)
	}
	return parsedId, nil
}

func (c CustomClaims) GetTokenType() PrincipalType {
	if c.UserName != "" {
		return PrincipalTypeUser
	}
	return PrincipalTypeGuest
}

var SubjectNotFoundError error = fmt.Errorf("Subject not found in JWT claims")

// get a token
func (s *Service) PostAuthLogin(w http.ResponseWriter, r *http.Request) {
	// deserialize the request body from a json string, use the request body struct that is generated
	// the the oapi-gen tool, validate that the username and password are not empty at the openapi spec level
	var reqBody PostAuthLoginJSONRequestBody
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		SendError(w, http.StatusBadRequest, fmt.Sprintf("error decoding request body: %s", err.Error()))
		return
	}
	// use the users service client to validate the credentials
	userId, isValid, err := s.userServiceClient.ValidatePassword(
		r.Context(), reqBody.UserName, reqBody.Password,
	)
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.NotFound {
			// if the user is missing, send a 400 error
			SendError(w, http.StatusNotFound, fmt.Sprintf("no user found with username: %v", reqBody.UserName))
			return
		} else {
			SendError(w, GrpcToHttpStatus(err), err.Error())
			return
		}
	}
	// if the credentials are invalid, send a 401 error
	if !isValid {
		SendError(w, http.StatusUnauthorized, "the provided username and password did not match")
		return
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
		SendError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// return a 200 response with the validated token
	SendJsonResponse(
		w, http.StatusOK, &LoginResponse{
			ExpiresIn: 60 * 60,
			Token: signedToken,
		},
	)
}