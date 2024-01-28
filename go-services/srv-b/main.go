package main

import (
	// Import necessary packages
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func initTracer() {
	ctx := context.Background()

	// Configure the exporter to send traces to Tempo
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint("http://localhost:4318"))
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("ServiceB"),
		),
	)
	if err != nil {
		log.Fatalf("Failed to create resource: %v", err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
}

func setupLogger() (*zap.Logger, *os.File) {
	logDir := "/app/logs"
	logFilePath := filepath.Join(logDir, "service-b.log")

	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		panic(err) // or handle it more gracefully
	}

	file, err := os.Create(logFilePath)
	if err != nil {
		panic(err) // or handle it more gracefully
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(file),
		zapcore.InfoLevel,
	)

	logger := zap.New(core)
	defer logger.Sync() // Flushes buffer, if any

	return logger, file
}

func main() {
	initTracer()
	logger, file := setupLogger()

	defer file.Close()

	http.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("ServiceB").Start(r.Context(), "process")
		defer span.End()
		logger.Info("Processing request", zap.String("service", "ServiceB"))
		// Call Service C
		client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost:8082/finalize", nil)
		if _, err := client.Do(req); err != nil {
			log.Printf("Error calling Service C: %v", err)
			span.RecordError(err)
			span.SetAttributes(attribute.String("error", err.Error()))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("Request processed", zap.String("service", "ServiceB"))
	})

	logger.Info("Service B is running on port 8081", zap.String("service", "ServiceB"))
	log.Fatal(http.ListenAndServe(":8081", nil))
}
