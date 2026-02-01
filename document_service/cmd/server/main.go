package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"net"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	pb "github.com/townsag/reed/document_service/api/v1"
	"github.com/townsag/reed/document_service/internal/config"
	"github.com/townsag/reed/document_service/internal/repository"
	"github.com/townsag/reed/document_service/internal/server"
	"github.com/townsag/reed/document_service/internal/service"

	"github.com/townsag/reed/user_service/pkg/middleware"
)

func main() {
	// initialize the otel sdk
	otelShutdown, err := config.SetupOTelSDK(context.Background())
	if err != nil {
		slog.Error("failed to bootstrap the otel sdk: ", "error", err)
		os.Exit(1)
	}
	defer otelShutdown(context.Background())
	// create a connection to the postgres database
	cfg, err := config.GetConfiguration()
	if err != nil {
		slog.Error("failed to get database connection configuration", "error", err)
		os.Exit(1)
	}
	cfg.AfterConnect = config.RegisterTypes
	pool, err := config.CreateDBConnectionPool(context.Background(), cfg)
	if err != nil {
		slog.Error("failed to create database connection pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	// create a document repo object
	documentRepo := repository.NewDocumentRepository(pool)
	// create a document service object
	documentService := service.NewDocumentService(documentRepo)
	// create a document server object
	documentServer := server.NewDocumentServiceImpl(documentService)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 50051))
	if err != nil {
		slog.Error("failed to listen", "error", err)
		os.Exit(1)
	}
	s := grpc.NewServer(
		grpc.UnaryInterceptor(middleware.LoggingInterceptor()),
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterDocumentServiceServer(s, documentServer)
	slog.Info(fmt.Sprintf("server listening at %v", lis.Addr()))
	if err := s.Serve(lis); err != nil {
		slog.Error("failed to serve", "error", err)
		os.Exit(1)
	}
}