package server

import (
	"net/http"
	"github.com/getkin/kin-openapi/openapi3"
	middleware "github.com/oapi-codegen/nethttp-middleware"
)

func RequestValidationMiddleware() func(http.Handler) http.Handler {
	// read the openapi 3 spec from the file system
	spec, err := openapi3.NewLoader().LoadFromFile("../../api/v1/api-gateway.yml")
	if err != nil {
		panic(err)
	}
	// use the oapi request validator to generate a handler function middleware
	mw := middleware.OapiRequestValidator(spec)
	return mw
}