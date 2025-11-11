package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	
	"github.com/townsag/reed/user_service/pkg/middleware"
	"github.com/townsag/reed/user_service/internal/config"
	"github.com/townsag/reed/user_service/internal/repository"
	"github.com/townsag/reed/user_service/internal/service"
	"github.com/townsag/reed/user_service/internal/server"
	pb "github.com/townsag/reed/user_service/api"
)


func main() {
	// initialize the otel sdk
	otelShutdown, err := config.SetupOTelSDK(context.Background())
	if err != nil {
		log.Fatalf("failed to bootstrap OTEL SDK: %v", err)
	}
	defer otelShutdown(context.Background())
	// create a connection to the database
	cfg, err := config.GetConfiguration()
	if err != nil {
		log.Fatalf("failed to get database connection configuration: %v", err)
	}
	pool, err := config.CreateDBConnectionPool(context.Background(), cfg)
	if err != nil {
		log.Fatalf("failed to create database connection pool: %v", err)
	}
	defer pool.Close()
	// create a repo
	userRepo := repository.NewUserRepository(pool)
	// create a service
	userService := service.NewUserService(userRepo)
	// create a server
	userServer := server.NewUserServiceImpl(userService)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 50051))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(
		grpc.UnaryInterceptor(middleware.RequestIdInterceptor()),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterUserServiceServer(s, userServer)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}