package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/townsag/reed/api_gateway/internal/config"
)

type contextKey string
const (
	claimsKey contextKey = "x-reed-jwt-claims"
)

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
		token, err := jwt.ParseWithClaims(
			tokenString, 
			CustomClaims{}, 
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