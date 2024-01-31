package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

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
			semconv.ServiceNameKey.String("ServiceA"),
		)),
	)
	otel.SetTracerProvider(tp)

	return tp
}

func setupLogger() (*zap.Logger, *os.File) {
	logDir := "/app/logs"
	logFilePath := filepath.Join(logDir, "service-a.log")

	// Ensure log directory exists
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatalf("failed to create log directory: %v", err)
	}
	file, err := os.Create(logFilePath)
	if err != nil {
		log.Fatalf("failed to create log file: %v", err)
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(file),
		zapcore.InfoLevel,
	)

	return zap.New(core), file
}

func main() {
	tp := initTracer()
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	logger, logFile := setupLogger()
	defer func() {
		if err := logFile.Close(); err != nil {
			logger.Error("Error closing log file", zap.Error(err))
		}
		if err := logger.Sync(); err != nil {
			log.Printf("Error syncing logger: %v", err)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())

	http.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		ctx, span := otel.Tracer("ServiceA").Start(r.Context(), "start")
		logger.Info("Trace started", zap.String("traceID", span.SpanContext().TraceID().String()))
		defer span.End()

		span.SetAttributes(
			attribute.String("service.name", "ServiceA"),
			attribute.String("operation", "data processing"),
			attribute.String("client.ip", r.RemoteAddr),
		)

		start := time.Now()
		logger.Info("Request received", zap.String("service", "ServiceA"))

		// Call Service B
		client := http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
		req, err := http.NewRequestWithContext(ctx, "GET", "http://service-b:8081/process", nil)
		if err != nil {
			logger.Error("Error creating request to service B", zap.Error(err))
			span.RecordError(err)
			http.Error(w, fmt.Sprintf("Error creating request to service B: %v", err), http.StatusInternalServerError)
			return
		}
		if _, err := client.Do(req); err != nil {
			logger.Error("Error calling service B", zap.Error(err))
			span.RecordError(err)
			span.SetAttributes(attribute.String("error", err.Error()))
			http.Error(w, fmt.Sprintf("Error calling service B: %v", err), http.StatusInternalServerError)
			return
		}

		w.Write([]byte("Request Processed"))

		requestLatency.Observe(time.Since(start).Seconds())
		totalRequests.Inc()

		logger.Info("Request processed", zap.String("service", "ServiceA"))
	})

	logger.Info("Service A is running on port 8080", zap.String("service", "ServiceA"))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
