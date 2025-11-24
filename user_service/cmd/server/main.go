package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	pb "github.com/townsag/reed/user_service/api"
	"github.com/townsag/reed/user_service/internal/config"
	"github.com/townsag/reed/user_service/internal/repository"
	"github.com/townsag/reed/user_service/internal/server"
	"github.com/townsag/reed/user_service/internal/service"
	"github.com/townsag/reed/user_service/pkg/middleware"
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
		slog.Error("failed to get database connection configuration", "error", err.Error())
		os.Exit(1)
	}
	pool, err := config.CreateDBConnectionPool(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to create database connection pool", "error", err.Error())
		os.Exit(1)
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
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpc.UnaryServerInterceptor(middleware.TraceIdInterceptor()),
			grpc.UnaryServerInterceptor(middleware.LoggingInterceptor()),
		),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterUserServiceServer(s, userServer)
	slog.Warn(fmt.Sprintf("server listening at %v", lis.Addr()))
	if err := s.Serve(lis); err != nil {
		slog.Error("failed to serve", "error", err.Error())
		os.Exit(1)
	}
}