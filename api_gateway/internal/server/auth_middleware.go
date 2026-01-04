package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/townsag/reed/api_gateway/internal/config"
)

type contextKey string
const (
	claimsKey contextKey = "x-reed-jwt-claims"
)

var ErrorClaimsNotFound error = fmt.Errorf("no claims found in this request context")

func GetClaims(ctx context.Context) (*CustomClaims, error) {
	claims := ctx.Value(claimsKey)
	if claims == nil {
		return nil, ErrorClaimsNotFound
	}
	customClaims, ok := claims.(*CustomClaims)
	if !ok {
		return nil, ErrorClaimsNotFound
	}
	return customClaims, nil
}

/*
Some notes:
- based on the implementation of parse with claims and the below stack overflow thread
  I am convinced that I can pass a pointer to a custom claims struct to the parse with
  claims function and the returned token will have a pointer to a custom claims struct
	- https://stackoverflow.com/questions/35604356/json-unmarshal-accepts-a-pointer-to-a-pointer
- also look at this jwt documentation example
	- https://pkg.go.dev/github.com/golang-jwt/jwt/v5#example-ParseWithClaims-CustomClaimsType
*/
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// check if the path is /auth/login
		if r.URL.Path == "/auth/login" {
			// if so, then continue without validating that there is a token
			next.ServeHTTP(w, r)
		}
		// read the token from the Authentication header
		headerValue := r.Header.Get("Authentication")
		if headerValue == "" {
			SendError(w, http.StatusUnauthorized, "Authentication header with JWT bearer token is required")
			return
		}
		// split validate the token 
		tokenString := strings.TrimPrefix(headerValue, "Bearer ")
		if tokenString == headerValue {
			SendError(w, http.StatusUnauthorized, "poorly formatted header value for Authentication header")
			return
		}
		// validate the token body
		// attempt to validate the token body first as a user type token then as a guest type token
		token, err := jwt.ParseWithClaims(
			tokenString, 
			&CustomClaims{}, 
			func (token *jwt.Token) (any, error) {
				return []byte(config.JWTSecretKey), nil
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}),
		)
		if err != nil {
			SendError(w, http.StatusForbidden, err.Error())
			return
		}
		customClaims, ok := token.Claims.(*CustomClaims)
		if !ok {
			SendError(w, http.StatusForbidden, "poorly formatted jwt claims")
			return
		}
		// add the custom claims to the request context
		ctx := context.WithValue(r.Context(), claimsKey, customClaims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}