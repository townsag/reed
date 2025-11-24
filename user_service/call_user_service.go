package main

import (
	"context"
	"log"
	"fmt"

	"github.com/google/uuid"
	"github.com/townsag/reed/user_service/pkg/client"
)

func main() {
	ctx := context.Background()
	// create an instance of the client
	userClient, err := client.NewUserServiceClient("localhost:50051")
	if err != nil {
		log.Fatalf("failed to create a client with error: %v", err)
	}
	// create a user
	reply, err := userClient.CreateUser(
		ctx,
		"testUser",
		"test@example.com",
		"password",
		nil,
	)
	fmt.Println("reply: ", reply)
	if err != nil {
		log.Fatalf("failed to create a user with error: %v", err)
	}
	userId := reply.UserId
	// get that user
	user, err := userClient.GetUser(ctx, uuid.MustParse(userId))
	if err != nil {
		log.Fatalf("failed to get user with error: %v", err)
	} else {
		log.Println("found user: ", user)
	}
	// delete that user
	err = userClient.DeactivateUser(ctx, uuid.MustParse(userId))
	if err != nil {
		log.Fatalf("failed to deactivate user with error: %v", err)
	}
}