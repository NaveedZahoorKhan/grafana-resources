package main

import (
	// Import necessary packages
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
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

func initTracer() *trace.TracerProvider {
	ctx := context.Background()

	// Configure the exporter to send traces to Tempo
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint("grafana-tempo:4318"), otlptracehttp.WithInsecure())
	if err != nil {
		log.Fatalf("failed to create exporter: %v", err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(resource.NewSchemaless(
			semconv.ServiceNameKey.String("ServiceB"),
		)),
	)
	otel.SetTracerProvider(tp)

	return tp

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

var (
	totalRequests = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "total_requests",
			Help: "Total number of requests made",
		},
	)
	requestLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "request_latency_seconds",
			Help: "Request latency in seconds",
		},
	)
)

func init() {
	prometheus.MustRegister(totalRequests)
	prometheus.MustRegister(requestLatency)
}

func main() {
	tp := initTracer()
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	logger, file := setupLogger()
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing log file: %v", err)
		}
		if err := logger.Sync(); err != nil {
			log.Printf("Error syncing logger: %v", err)
		}
	}()

	// The rest of your main function remains largely unchanged
	http.HandleFunc("/process", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("ServiceB").Start(r.Context(), "process")
		defer span.End()

		span.SetAttributes(
			attribute.String("service.name", "ServiceB"), // Corrected to ServiceB
			attribute.String("operation", "data processing"),
			attribute.String("client.ip", r.RemoteAddr),
		)

		logger.Info("Processing request", zap.String("service", "ServiceB"))
		// Ensure context propagation to Service C
		client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
		req, err := http.NewRequestWithContext(ctx, "GET", "http://service-c:8082/finalize", nil) // Added error check
		if err != nil {
			logger.Error("Error creating request to service C", zap.Error(err))
			span.RecordError(err)
			http.Error(w, fmt.Sprintf("Error creating request to service C: %v", err), http.StatusInternalServerError)
			return
		}
		otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

		if _, err := client.Do(req); err != nil {
			logger.Error("Error calling service C", zap.Error(err))
			span.RecordError(err)
			http.Error(w, fmt.Sprintf("Error calling service C: %v", err), http.StatusInternalServerError)
			return
		}

		// The rest of your handler remains unchanged
	})

	logger.Info("Service B is running on port 8081", zap.String("service", "ServiceB"))
	log.Fatal(http.ListenAndServe(":8081", nil))
}
