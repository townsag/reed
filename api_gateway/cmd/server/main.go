package main

import (
	"net/http"

	"github.com/townsag/reed/api_gateway/internal/server"
	"github.com/townsag/reed/api_gateway/internal/service"
)


func main() {
	// create a request validation middleware
	validationMiddleware := server.RequestValidationMiddleware()
	// create an instance of the struct which implements the server.ServerInterface
	service := service.NewService()
	// create an instance of the handler 
	h := server.HandlerWithOptions(
		service, server.StdHTTPServerOptions{
			BaseURL: "todo",
			Middlewares: []server.MiddlewareFunc{validationMiddleware},
			ErrorHandlerFunc: server.ErrorHandlerFunc,
		},
	)
	// create a net/http server from this handler
	s := &http.Server{
		Handler: h,
		Addr: "0.0.0.0:8000",
	}
	s.ListenAndServe()
}