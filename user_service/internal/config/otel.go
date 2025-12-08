package config

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"

	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.37.0"
)

const version = "0.1.0"

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func SetupOTelSDK(ctx context.Context) (func(context.Context) error, error) {
	var shutdownFuncs []func(context.Context) error
	var err error

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	// set up a resource
	resource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("user-service"),
			semconv.ServiceVersion(version),
		),
	)

	if err != nil {
		return shutdown, err
	}

	// Set up propagator.
	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	// Set up trace provider.
	tracerProvider, err := newTracerProvider(ctx, resource)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	//Set up meter provider.
	meterProvider, err := newMeterProvider(ctx, resource)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	// Set up logger provider.
	loggerProvider, err := newLoggerProvider(ctx, resource)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	// create a new slog logger that is backed by a handler that will send all logs that
	// it receives to otel
	defaultLogger := otelslog.NewLogger(
		"user_service",
		otelslog.WithLoggerProvider(loggerProvider),
	)
	slog.SetDefault(defaultLogger)

	return shutdown, err
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTracerProvider(ctx context.Context, res *resource.Resource) (*trace.TracerProvider, error) {
	traceExporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}

	tracerProvider := trace.NewTracerProvider(
		trace.WithResource(res),
		trace.WithBatcher(traceExporter,
			// Default is 5s. Set to 1s for demonstrative purposes.
			trace.WithBatchTimeout(time.Second)),
	)
	return tracerProvider, nil
}

func newMeterProvider(ctx context.Context, res *resource.Resource) (*metric.MeterProvider, error) {
	metricExporter, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(
			metric.NewPeriodicReader(
				metricExporter,
				// Default is 1m. Set to 3s for demonstrative purposes.
				metric.WithInterval(3*time.Second),
			),
		),
	)
	return meterProvider, nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*log.LoggerProvider, error) {
	// create a otlp grpc log exporter
	logExporter, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, err
	}

	// create a stdout log exporter
	stdOutLogExporter, err := stdoutlog.New()
	if err != nil {
		return nil, err
	}

	loggerProvider := log.NewLoggerProvider(
		log.WithResource(res),
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
		log.WithProcessor(log.NewBatchProcessor(stdOutLogExporter)),
	)
	return loggerProvider, nil
}
