package server

import (
	userService "github.com/townsag/reed/user_service/pkg/client"
	documentService "github.com/townsag/reed/document_service/pkg/client"
)

// should this generic function should also be able to take any of the returned service level
// errors as an input?


var _ ServerInterface = (*Service)(nil)

type Service struct {
	userServiceClient *userService.UserServiceClient
	documentServiceClient *documentService.DocumentServiceClient
	// probably also add a client for accessing some external state like a cache or a 
	// way to record request counts 
}

func NewService(
	usClient *userService.UserServiceClient,
	dsClient *documentService.DocumentServiceClient,
) Service {
	return Service{
		userServiceClient: usClient,
		documentServiceClient: dsClient,
	}
}



