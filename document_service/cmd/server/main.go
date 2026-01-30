package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	pb "github.com/townsag/reed/document_service/api/v1"
	"github.com/townsag/reed/document_service/internal/config"
	"github.com/townsag/reed/document_service/internal/repository"
	"github.com/townsag/reed/document_service/internal/server"
	"github.com/townsag/reed/document_service/internal/service"
)

func main() {
	// initialize the otel sdk
	otelShutdown, err := config.SetupOTelSDK(context.Background())
	if err != nil {
		log.Fatalf("failed to bootstrap the otel sdk: %v", err)
	}
	defer otelShutdown(context.Background())
	// create a connection to the postgres database
	cfg, err := config.GetConfiguration()
	if err != nil {
		log.Fatalf("failed to get database connection configuration: %v", err)
	}
	cfg.AfterConnect = config.RegisterTypes
	pool, err := config.CreateDBConnectionPool(context.Background(), cfg)
	if err != nil {
		log.Fatalf("failed to create database connection pool: %v", err)
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
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterDocumentServiceServer(s, documentServer)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}