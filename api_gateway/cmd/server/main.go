package main

import (
	"net/http"
	"log"

	"github.com/townsag/reed/api_gateway/internal/server"
	"github.com/townsag/reed/api_gateway/internal/service"
	"github.com/townsag/reed/api_gateway/internal/config"

	"github.com/townsag/reed/user_service/pkg/client"
)


func main() {
	// create a client that can be used to access the user service
	userServiceClient, err := client.NewUserServiceClient(config.UserServiceAddr)
	if err != nil {
		log.Fatalf("failed to create a user service client with error: %s", err.Error())
	}
	// create an instance of the struct which implements the server.ServerInterface
	service := service.NewService(userServiceClient)
	// create a request validation middleware
	validationMiddleware := server.RequestValidationMiddleware()
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