---
name: go-systems
description: Go systems programming — CLI tools with cobra, configuration with viper/envconfig, structured logging with slog/zerolog, OpenTelemetry tracing and metrics, Prometheus instrumentation, error handling patterns, file I/O, process management and signal handling, Go modules and workspaces, build tags, cross-compilation, Makefile/Taskfile patterns, Dockerfile multi-stage builds, CI/CD with golangci-lint/govulncheck, Cobra CLI argument parsing
---

# Go Systems Programming

Comprehensive reference for building production-grade Go systems software targeting Go 1.22+. Covers CLI tool development with Cobra, configuration management with Viper and envconfig, structured logging with log/slog and zerolog, distributed tracing and metrics with OpenTelemetry, Prometheus instrumentation for monitoring, error handling patterns (sentinel errors, wrapping, errors.Is/As), file I/O with os, io, and bufio, process management and signal handling with os/signal, Go modules and workspace mode, build tags and cross-compilation, Makefile and Taskfile patterns, Dockerfile multi-stage builds, and CI/CD pipelines with golangci-lint, go vet, and govulncheck.

## Table of Contents

1. [Cobra CLI Tool Development](#1-cobra-cli-tool-development)
2. [Configuration with Viper & envconfig](#2-configuration-with-viper--envconfig)
3. [Structured Logging with slog](#3-structured-logging-with-slog)
4. [Structured Logging with zerolog](#4-structured-logging-with-zerolog)
5. [OpenTelemetry Tracing & Metrics](#5-opentelemetry-tracing--metrics)
6. [Prometheus Instrumentation](#6-prometheus-instrumentation)
7. [Error Handling Patterns](#7-error-handling-patterns)
8. [File I/O with os, io, and bufio](#8-file-io-with-os-io-and-bufio)
9. [Process Management & Signal Handling](#9-process-management--signal-handling)
10. [Go Modules & Workspace Mode](#10-go-modules--workspace-mode)
11. [Build Tags & Cross-Compilation](#11-build-tags--cross-compilation)
12. [Makefile & Taskfile Patterns](#12-makefile--taskfile-patterns)
13. [Dockerfile Multi-Stage Builds](#13-dockerfile-multi-stage-builds)
14. [CI/CD with golangci-lint, go vet, govulncheck](#14-cicd-with-golangci-lint-go-vet-govulncheck)
15. [Best Practices](#15-best-practices)
16. [Anti-Patterns](#16-anti-patterns)
17. [Sources & References](#17-sources--references)

---

## 1. Cobra CLI Tool Development

Cobra is the standard Go library for building CLI applications. It provides subcommand hierarchies, flag parsing, shell completions, and auto-generated help.

### Root Command Setup

```go
// cmd/root.go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "mytool",
	Short: "A production-grade CLI tool",
	Long: `mytool is a CLI application for managing infrastructure
and deployments. It supports configuration via files, environment
variables, and command-line flags.`,
	Version:       "1.0.0",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initConfig()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
		"config file (default is $HOME/.mytool.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
		"enable verbose output")
	rootCmd.PersistentFlags().String("log-level", "info",
		"log level (debug, info, warn, error)")

	_ = viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
}

func initConfig() error {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("finding home directory: %w", err)
		}
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".mytool")
	}

	viper.SetEnvPrefix("MYTOOL")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("reading config: %w", err)
		}
	}
	return nil
}
```

### Subcommand with Flags and Validation

```go
// cmd/deploy.go
package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var deployCmd = &cobra.Command{
	Use:   "deploy [environment]",
	Short: "Deploy the application to an environment",
	Long:  `Deploy builds and deploys the application to the specified environment.`,
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) (
		[]string, cobra.ShellCompDirective,
	) {
		if len(args) == 0 {
			return []string{"staging", "production", "development"}, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		env := args[0]
		timeout, _ := cmd.Flags().GetDuration("timeout")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		tags, _ := cmd.Flags().GetStringSlice("tag")

		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()

		if dryRun {
			fmt.Printf("DRY RUN: would deploy to %s with tags %v\n", env, tags)
			return nil
		}

		return runDeploy(ctx, env, tags)
	},
}

func init() {
	deployCmd.Flags().Duration("timeout", 5*time.Minute,
		"deployment timeout")
	deployCmd.Flags().Bool("dry-run", false,
		"simulate the deployment without making changes")
	deployCmd.Flags().StringSlice("tag", nil,
		"image tags to deploy (can be specified multiple times)")

	_ = deployCmd.MarkFlagRequired("tag")
	_ = viper.BindPFlag("deploy.timeout", deployCmd.Flags().Lookup("timeout"))

	rootCmd.AddCommand(deployCmd)
}

func runDeploy(ctx context.Context, env string, tags []string) error {
	fmt.Printf("Deploying to %s with tags: %v\n", env, tags)
	// deployment logic here
	return nil
}
```

### Generating Shell Completions

```go
// cmd/completion.go
package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Args:  cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return fmt.Errorf("unsupported shell: %s", args[0])
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
```

---

## 2. Configuration with Viper & envconfig

### Layered Configuration with Viper

Viper supports layered configuration from files, environment variables, flags, and remote key/value stores. Priority order: explicit Set > flags > env > config file > defaults.

```go
package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

type ServerConfig struct {
	Host           string        `mapstructure:"host"`
	Port           int           `mapstructure:"port"`
	ReadTimeout    time.Duration `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration `mapstructure:"write_timeout"`
	MaxRequestSize int64         `mapstructure:"max_request_size"`
}

type DatabaseConfig struct {
	DSN             string        `mapstructure:"dsn"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

func Load() (*Config, error) {
	// Defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.read_timeout", 30*time.Second)
	viper.SetDefault("server.write_timeout", 30*time.Second)
	viper.SetDefault("server.max_request_size", 10<<20) // 10 MiB
	viper.SetDefault("database.max_open_conns", 25)
	viper.SetDefault("database.max_idle_conns", 5)
	viper.SetDefault("database.conn_max_lifetime", 5*time.Minute)
	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")

	// Config file search
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/mytool/")

	// Environment variable binding
	viper.SetEnvPrefix("MYTOOL")
	viper.AutomaticEnv()

	// Read config file (optional)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", c.Server.Port)
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("database.dsn is required")
	}
	if c.Database.MaxOpenConns < c.Database.MaxIdleConns {
		return fmt.Errorf("database.max_open_conns must be >= max_idle_conns")
	}
	return nil
}
```

### Lightweight Config with envconfig

For simpler applications that only need environment variables, `envconfig` provides a struct-tag-driven approach.

```go
package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type EnvConfig struct {
	Port            int           `envconfig:"PORT" default:"8080"`
	Host            string        `envconfig:"HOST" default:"0.0.0.0"`
	DatabaseURL     string        `envconfig:"DATABASE_URL" required:"true"`
	RedisURL        string        `envconfig:"REDIS_URL" default:"redis://localhost:6379"`
	LogLevel        string        `envconfig:"LOG_LEVEL" default:"info"`
	ShutdownTimeout time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"30s"`
	Debug           bool          `envconfig:"DEBUG" default:"false"`
}

func LoadEnv() (*EnvConfig, error) {
	var cfg EnvConfig
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("processing environment config: %w", err)
	}
	return &cfg, nil
}
```

---

## 3. Structured Logging with slog

Go 1.21+ includes `log/slog` in the standard library, providing structured, leveled logging without third-party dependencies.

### Basic slog Setup

```go
package main

import (
	"context"
	"log/slog"
	"os"
)

func main() {
	// JSON handler for production
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Rename "msg" to "message" for compatibility with log aggregators
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			return a
		},
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Basic structured logging
	slog.Info("server starting",
		slog.String("host", "0.0.0.0"),
		slog.Int("port", 8080),
		slog.String("version", "1.2.3"),
	)

	// With context for request-scoped values
	ctx := context.Background()
	slog.InfoContext(ctx, "request received",
		slog.String("method", "GET"),
		slog.String("path", "/api/v1/users"),
		slog.String("request_id", "abc-123"),
	)

	// Logger groups for nested structure
	dbLogger := logger.WithGroup("database")
	dbLogger.Info("connection pool initialized",
		slog.Int("max_open", 25),
		slog.Int("max_idle", 5),
	)

	// Error logging with error attribute
	slog.Error("failed to connect",
		slog.String("service", "payment-gateway"),
		slog.Any("error", fmt.Errorf("connection refused")),
		slog.Duration("retry_after", 5*time.Second),
	)
}
```

### Custom slog Handler with Context Extraction

```go
package logging

import (
	"context"
	"log/slog"
)

type contextKey string

const (
	requestIDKey contextKey = "request_id"
	userIDKey    contextKey = "user_id"
)

// ContextHandler wraps an slog.Handler to extract values from context.
type ContextHandler struct {
	inner slog.Handler
}

func NewContextHandler(inner slog.Handler) *ContextHandler {
	return &ContextHandler{inner: inner}
}

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if reqID, ok := ctx.Value(requestIDKey).(string); ok {
		r.AddAttrs(slog.String("request_id", reqID))
	}
	if userID, ok := ctx.Value(userIDKey).(string); ok {
		r.AddAttrs(slog.String("user_id", userID))
	}
	return h.inner.Handle(ctx, r)
}

func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{inner: h.inner.WithGroup(name)}
}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}
```

---

## 4. Structured Logging with zerolog

zerolog provides high-performance, zero-allocation JSON logging. It is a popular choice when you need maximum logging throughput.

### zerolog Setup

```go
package main

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	// Pretty console output for development
	if os.Getenv("ENV") == "development" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		})
	} else {
		// JSON output for production
		zerolog.TimeFieldFormat = time.RFC3339Nano
		log.Logger = zerolog.New(os.Stdout).
			With().
			Timestamp().
			Str("service", "mytool").
			Str("version", "1.2.3").
			Caller().
			Logger()
	}

	// Set global level
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	// Structured logging
	log.Info().
		Str("host", "0.0.0.0").
		Int("port", 8080).
		Msg("server starting")

	// Sub-logger for a component
	dbLog := log.With().Str("component", "database").Logger()
	dbLog.Info().
		Int("max_open_conns", 25).
		Dur("conn_max_lifetime", 5*time.Minute).
		Msg("connection pool ready")

	// Error logging with stack trace
	err := fmt.Errorf("connection refused")
	log.Error().
		Err(err).
		Str("service", "payment-gateway").
		Dur("retry_after", 5*time.Second).
		Msg("failed to connect")
}
```

---

## 5. OpenTelemetry Tracing & Metrics

OpenTelemetry (OTel) provides vendor-neutral distributed tracing and metrics collection. Go has first-class OTel SDK support.

### OTel Tracer Provider Setup

```go
package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

func InitTracer(ctx context.Context, serviceName, version string) (func(context.Context) error, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint("localhost:4317"),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(version),
			attribute.String("environment", "production"),
		),
		resource.WithHost(),
		resource.WithOS(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(0.1), // sample 10% of root spans
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// Usage: creating spans in application code
func ProcessOrder(ctx context.Context, orderID string) error {
	tracer := otel.Tracer("mytool/orders")

	ctx, span := tracer.Start(ctx, "ProcessOrder",
		trace.WithAttributes(
			attribute.String("order.id", orderID),
		),
	)
	defer span.End()

	// Nested span for a sub-operation
	ctx, validateSpan := tracer.Start(ctx, "ValidateOrder")
	if err := validateOrder(ctx, orderID); err != nil {
		validateSpan.RecordError(err)
		validateSpan.SetStatus(codes.Error, err.Error())
		validateSpan.End()
		return fmt.Errorf("validating order %s: %w", orderID, err)
	}
	validateSpan.End()

	span.AddEvent("order validated", trace.WithAttributes(
		attribute.String("order.id", orderID),
	))

	return nil
}
```

### OTel Meter Provider for Metrics

```go
package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func InitMeter(ctx context.Context, res *resource.Resource) (func(context.Context) error, error) {
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint("localhost:4317"),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP metric exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exporter,
				sdkmetric.WithInterval(15*time.Second),
			),
		),
	)

	otel.SetMeterProvider(mp)
	return mp.Shutdown, nil
}

// Application-level metrics instrumentation
type OrderMetrics struct {
	ordersProcessed metric.Int64Counter
	orderDuration   metric.Float64Histogram
	activeOrders    metric.Int64UpDownCounter
}

func NewOrderMetrics() (*OrderMetrics, error) {
	meter := otel.Meter("mytool/orders")

	processed, err := meter.Int64Counter("orders.processed",
		metric.WithDescription("Total number of orders processed"),
		metric.WithUnit("{order}"),
	)
	if err != nil {
		return nil, err
	}

	duration, err := meter.Float64Histogram("orders.duration",
		metric.WithDescription("Time to process an order"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.5, 1, 5, 10),
	)
	if err != nil {
		return nil, err
	}

	active, err := meter.Int64UpDownCounter("orders.active",
		metric.WithDescription("Number of orders currently being processed"),
		metric.WithUnit("{order}"),
	)
	if err != nil {
		return nil, err
	}

	return &OrderMetrics{
		ordersProcessed: processed,
		orderDuration:   duration,
		activeOrders:    active,
	}, nil
}
```

---

## 6. Prometheus Instrumentation

For services that expose a `/metrics` endpoint for Prometheus scraping, the `prometheus/client_golang` library is the standard choice.

### Prometheus Setup

```go
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "mytool",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests by method, path, and status.",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "mytool",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	dbConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "mytool",
			Subsystem: "db",
			Name:      "connections_active",
			Help:      "Number of active database connections.",
		},
	)

	taskQueueSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "mytool",
			Subsystem: "queue",
			Name:      "size",
			Help:      "Current number of items in the task queue.",
		},
		[]string{"priority"},
	)
)

// MetricsHandler returns an http.Handler that serves Prometheus metrics.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// InstrumentHTTP wraps an http.Handler with Prometheus request metrics.
func InstrumentHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timer := prometheus.NewTimer(httpRequestDuration.WithLabelValues(r.Method, r.URL.Path))
		defer timer.ObserveDuration()

		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		httpRequestsTotal.WithLabelValues(
			r.Method, r.URL.Path, http.StatusText(rw.statusCode),
		).Inc()
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
```

---

## 7. Error Handling Patterns

Go's error handling uses explicit return values. Go 1.13+ introduced error wrapping, and Go 1.22+ continues to build on these primitives.

### Sentinel Errors

```go
package storage

import "errors"

// Sentinel errors for well-known, expected failure conditions.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrRateLimited   = errors.New("rate limited")
)
```

### Wrapping Errors with Context

```go
package storage

import (
	"fmt"
	"os"
)

func ReadConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file %s: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	return data, nil
}
```

### Inspecting Errors with errors.Is and errors.As

```go
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"myapp/storage"
)

func handleRequest() {
	data, err := storage.ReadConfig("/etc/myapp/config.yaml")
	if err != nil {
		// Check for a specific sentinel error anywhere in the chain
		if errors.Is(err, storage.ErrNotFound) {
			slog.Warn("config not found, using defaults", slog.Any("error", err))
			return
		}

		// Extract a specific error type from the chain
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			slog.Error("filesystem error",
				slog.String("op", pathErr.Op),
				slog.String("path", pathErr.Path),
				slog.Any("error", pathErr.Err),
			)
			return
		}

		slog.Error("unexpected error", slog.Any("error", err))
		return
	}

	fmt.Printf("config loaded: %d bytes\n", len(data))
}
```

### Custom Error Types

```go
package api

import (
	"fmt"
	"net/http"
)

// AppError carries an HTTP status code and a user-facing message alongside
// the internal error. It implements the error interface and supports unwrapping.
type AppError struct {
	Code    int    // HTTP status code
	Message string // user-facing message
	Err     error  // internal error (may be nil)
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewNotFoundError(resource string, err error) *AppError {
	return &AppError{
		Code:    http.StatusNotFound,
		Message: fmt.Sprintf("%s not found", resource),
		Err:     err,
	}
}

func NewBadRequestError(msg string) *AppError {
	return &AppError{
		Code:    http.StatusBadRequest,
		Message: msg,
	}
}

func NewInternalError(err error) *AppError {
	return &AppError{
		Code:    http.StatusInternalServerError,
		Message: "internal server error",
		Err:     err,
	}
}
```

---

## 8. File I/O with os, io, and bufio

### Reading Files

```go
package fileutil

import (
	"bufio"
	"fmt"
	"io"
	"os"
)

// ReadAll reads an entire file into memory. Suitable for small files.
func ReadAll(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

// ReadLines reads a file line-by-line using a scanner. Memory-efficient
// for large files because only one line is in memory at a time.
func ReadLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)

	// Increase buffer for files with very long lines (default is 64 KiB)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning %s: %w", path, err)
	}
	return lines, nil
}

// StreamCopy copies from src to dst using io.Copy with a buffered writer.
func StreamCopy(dst, src string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("opening source %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("creating destination %s: %w", dst, err)
	}
	defer out.Close()

	bw := bufio.NewWriter(out)
	n, err := io.Copy(bw, in)
	if err != nil {
		return n, fmt.Errorf("copying data: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return n, fmt.Errorf("flushing buffer: %w", err)
	}
	return n, nil
}
```

### Writing Files Safely

```go
package fileutil

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to a file atomically by first writing to a temporary
// file in the same directory, then renaming. This prevents partial writes.
func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Clean up temp file on any error path
	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming temp file to %s: %w", path, err)
	}
	return nil
}

// WriteLines writes a slice of strings as lines to a file with buffered I/O.
func WriteLines(path string, lines []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("writing line: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flushing writer: %w", err)
	}
	return nil
}
```

### Walking Directories

```go
package fileutil

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WalkGoFiles returns all .go files under root, skipping vendor and testdata.
func WalkGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip common directories
		if d.IsDir() {
			switch d.Name() {
			case "vendor", "testdata", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
```

---

## 9. Process Management & Signal Handling

### Graceful Shutdown with os/signal

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Create a context that is cancelled on SIGINT or SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      http.DefaultServeMux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		slog.Info("server starting", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Block until signal received
	<-ctx.Done()
	slog.Info("shutdown signal received, draining connections")

	// Give active requests a grace period to finish
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("server stopped cleanly")
}
```

### Running External Processes

```go
package process

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RunCommand executes an external command with a timeout and returns
// combined stdout/stderr output.
func RunCommand(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("running %s: %w\nstderr: %s", name, err, stderr.String())
	}

	return stdout.String(), nil
}

// RunPipeline runs a sequence of commands piping stdout to stdin.
func RunPipeline(ctx context.Context, commands [][]string) (string, error) {
	if len(commands) == 0 {
		return "", fmt.Errorf("no commands provided")
	}

	var cmds []*exec.Cmd
	for _, cmdArgs := range commands {
		cmds = append(cmds, exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...))
	}

	// Wire pipes between commands
	for i := 0; i < len(cmds)-1; i++ {
		pipe, err := cmds[i].StdoutPipe()
		if err != nil {
			return "", fmt.Errorf("creating pipe: %w", err)
		}
		cmds[i+1].Stdin = pipe
	}

	var output bytes.Buffer
	cmds[len(cmds)-1].Stdout = &output

	// Start all commands
	for _, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("starting %s: %w", cmd.Path, err)
		}
	}

	// Wait for all commands
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			return "", fmt.Errorf("waiting for %s: %w", cmd.Path, err)
		}
	}

	return output.String(), nil
}
```

---

## 10. Go Modules & Workspace Mode

### Module Basics (go.mod)

```
module github.com/myorg/mytool

go 1.22

require (
    github.com/spf13/cobra v1.8.0
    github.com/spf13/viper v1.18.2
    github.com/rs/zerolog v1.32.0
    go.opentelemetry.io/otel v1.24.0
    go.opentelemetry.io/otel/sdk v1.24.0
    github.com/prometheus/client_golang v1.19.0
)

// Replace directive for local development
// replace github.com/myorg/mylib => ../mylib
```

### Common Module Commands

```bash
# Initialize a new module
go mod init github.com/myorg/mytool

# Add a dependency (automatically updates go.mod and go.sum)
go get github.com/spf13/cobra@latest

# Tidy: remove unused deps, add missing ones
go mod tidy

# Vendor dependencies into ./vendor
go mod vendor

# Verify checksums
go mod verify

# Show the dependency graph
go mod graph

# Upgrade all dependencies
go get -u ./...

# Upgrade only patch versions
go get -u=patch ./...
```

### Workspace Mode (go.work)

Go workspaces let you develop multiple modules simultaneously without `replace` directives. Introduced in Go 1.18, refined through Go 1.22+.

```
// go.work
go 1.22

use (
    ./cmd/mytool
    ./pkg/core
    ./pkg/storage
    ./internal/platform
)
```

```bash
# Initialize a workspace
go work init ./cmd/mytool ./pkg/core

# Add another module to the workspace
go work use ./pkg/storage

# Sync workspace modules
go work sync

# Build all modules in the workspace
go build ./...

# Run tests across the workspace
go test ./...
```

### Recommended Multi-Module Layout

```
project/
  go.work              # workspace root
  cmd/
    mytool/
      go.mod           # module: github.com/myorg/mytool
      main.go
  pkg/
    core/
      go.mod           # module: github.com/myorg/mytool/pkg/core
      types.go
    storage/
      go.mod           # module: github.com/myorg/mytool/pkg/storage
      store.go
  internal/
    platform/
      go.mod           # module: github.com/myorg/mytool/internal/platform
      platform.go
```

---

## 11. Build Tags & Cross-Compilation

### Build Tags (Go 1.17+ syntax)

```go
//go:build linux && amd64

package platform

import "fmt"

func PlatformInfo() string {
	return fmt.Sprintf("Linux AMD64 build")
}
```

```go
//go:build darwin

package platform

func PlatformInfo() string {
	return "macOS build"
}
```

```go
//go:build !linux && !darwin

package platform

func PlatformInfo() string {
	return "unsupported platform"
}
```

### Custom Build Tags

```go
//go:build integration

package storage_test

import (
	"os"
	"testing"
)

func TestDatabaseIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	// run integration tests against a real database
}
```

Run with: `go test -tags=integration ./...`

### Cross-Compilation

```bash
# Build for Linux AMD64
GOOS=linux GOARCH=amd64 go build -o mytool-linux-amd64 ./cmd/mytool

# Build for Linux ARM64 (e.g., AWS Graviton)
GOOS=linux GOARCH=arm64 go build -o mytool-linux-arm64 ./cmd/mytool

# Build for macOS ARM (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o mytool-darwin-arm64 ./cmd/mytool

# Build for Windows
GOOS=windows GOARCH=amd64 go build -o mytool-windows-amd64.exe ./cmd/mytool

# Build with version info embedded via ldflags
go build -ldflags="-s -w \
  -X main.version=1.2.3 \
  -X main.commit=$(git rev-parse --short HEAD) \
  -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o mytool ./cmd/mytool

# List all supported OS/ARCH pairs
go tool dist list
```

### Embedding Version Information

```go
package main

import "fmt"

// Set via ldflags at build time
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func printVersion() {
	fmt.Printf("mytool %s (commit: %s, built: %s)\n", version, commit, buildDate)
}
```

---

## 12. Makefile & Taskfile Patterns

### Makefile

```makefile
# Project variables
BINARY_NAME := mytool
MODULE := github.com/myorg/mytool
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse --short HEAD)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

# Go settings
GOBIN ?= $(shell go env GOPATH)/bin
GOLANGCI_LINT_VERSION := v1.57.2

.PHONY: all build test lint fmt vet clean install tools docker

all: lint test build

## Build

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/mytool

build-all:
	GOOS=linux   GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-amd64   ./cmd/mytool
	GOOS=linux   GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-linux-arm64   ./cmd/mytool
	GOOS=darwin  GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-darwin-arm64  ./cmd/mytool
	GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o bin/$(BINARY_NAME)-windows.exe   ./cmd/mytool

install:
	go install -ldflags="$(LDFLAGS)" ./cmd/mytool

## Quality

test:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

test-integration:
	go test -race -tags=integration -count=1 ./...

lint: tools
	golangci-lint run ./...

fmt:
	gofumpt -l -w .

vet:
	go vet ./...

vuln: tools
	govulncheck ./...

## Tools

tools:
	@which golangci-lint > /dev/null 2>&1 || \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@which govulncheck > /dev/null 2>&1 || \
		go install golang.org/x/vuln/cmd/govulncheck@latest
	@which gofumpt > /dev/null 2>&1 || \
		go install mvdan.cc/gofumpt@latest

## Docker

docker:
	docker build -t $(BINARY_NAME):$(VERSION) .

## Clean

clean:
	rm -rf bin/ coverage.out

## Help

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
```

### Taskfile (Taskfile.yml)

```yaml
# Taskfile.yml — a modern alternative to Makefile
version: "3"

vars:
  BINARY_NAME: mytool
  VERSION:
    sh: git describe --tags --always --dirty
  COMMIT:
    sh: git rev-parse --short HEAD
  BUILD_DATE:
    sh: date -u +%Y-%m-%dT%H:%M:%SZ
  LDFLAGS: >-
    -s -w
    -X main.version={{.VERSION}}
    -X main.commit={{.COMMIT}}
    -X main.buildDate={{.BUILD_DATE}}

tasks:
  default:
    deps: [lint, test, build]

  build:
    desc: Build the binary
    cmds:
      - CGO_ENABLED=0 go build -ldflags="{{.LDFLAGS}}" -o bin/{{.BINARY_NAME}} ./cmd/mytool
    sources:
      - "**/*.go"
      - go.mod
      - go.sum
    generates:
      - bin/{{.BINARY_NAME}}

  test:
    desc: Run all tests with race detection
    cmds:
      - go test -race -coverprofile=coverage.out ./...
      - go tool cover -func=coverage.out

  test:integration:
    desc: Run integration tests
    cmds:
      - go test -race -tags=integration -count=1 ./...

  lint:
    desc: Run golangci-lint
    cmds:
      - golangci-lint run ./...

  fmt:
    desc: Format code with gofumpt
    cmds:
      - gofumpt -l -w .

  vet:
    desc: Run go vet
    cmds:
      - go vet ./...

  vuln:
    desc: Check for known vulnerabilities
    cmds:
      - govulncheck ./...

  docker:
    desc: Build Docker image
    cmds:
      - docker build -t {{.BINARY_NAME}}:{{.VERSION}} .

  clean:
    desc: Remove build artifacts
    cmds:
      - rm -rf bin/ coverage.out
```

---

## 13. Dockerfile Multi-Stage Builds

### Production Dockerfile

```dockerfile
# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM golang:1.22-bookworm AS builder

WORKDIR /src

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /bin/mytool \
    ./cmd/mytool

# ---- Final stage ----
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/mytool /usr/local/bin/mytool

# Copy config files if needed
# COPY --from=builder /src/config/default.yaml /etc/mytool/config.yaml

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["mytool"]
CMD ["serve"]
```

### Dockerfile with Testing Stage

```dockerfile
# syntax=docker/dockerfile:1

# ---- Base dependencies ----
FROM golang:1.22-bookworm AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# ---- Test stage ----
FROM deps AS test
COPY . .
RUN go vet ./...
RUN go test -race -count=1 ./...

# ---- Lint stage (optional, for CI) ----
FROM deps AS lint
RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.57.2
COPY . .
RUN golangci-lint run ./...

# ---- Build stage ----
FROM deps AS builder
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /bin/mytool \
    ./cmd/mytool

# ---- Final stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /bin/mytool /usr/local/bin/mytool
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["mytool"]
CMD ["serve"]
```

Build commands:

```bash
# Build production image
docker build --build-arg VERSION=1.2.3 --build-arg COMMIT=$(git rev-parse --short HEAD) \
  -t mytool:1.2.3 .

# Run test stage only (exits after tests)
docker build --target test -t mytool:test .

# Run lint stage only
docker build --target lint -t mytool:lint .
```

---

## 14. CI/CD with golangci-lint, go vet, govulncheck

### golangci-lint Configuration

```yaml
# .golangci.yml
run:
  timeout: 5m
  go: "1.22"
  tests: true

linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - gosimple
    - ineffassign
    - typecheck
    - gocritic
    - gofumpt
    - misspell
    - nolintlint
    - revive
    - bodyclose
    - exhaustive
    - gosec
    - prealloc
    - unconvert
    - unparam
    - errorlint
    - wrapcheck

linters-settings:
  errcheck:
    check-type-assertions: true
    check-blank: true
  govet:
    enable-all: true
  gocritic:
    enabled-tags:
      - diagnostic
      - style
      - performance
  revive:
    rules:
      - name: blank-imports
      - name: context-as-argument
      - name: dot-imports
      - name: error-return
      - name: error-strings
      - name: exported
      - name: increment-decrement
      - name: var-naming
      - name: package-comments
  errorlint:
    errorf: true
    asserts: true
    comparison: true
  wrapcheck:
    ignoreSigs:
      - .Errorf(
      - errors.New(
      - errors.Unwrap(

issues:
  max-issues-per-linter: 50
  max-same-issues: 5
  exclude-rules:
    - path: _test\.go
      linters:
        - wrapcheck
        - gosec
```

### GitHub Actions CI Workflow

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

env:
  GO_VERSION: "1.22"

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v4
        with:
          version: v1.57.2

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}

      - name: Run go vet
        run: go vet ./...

      - name: Run tests
        run: go test -race -coverprofile=coverage.out -count=1 ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v4
        with:
          files: coverage.out

  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}

      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest

      - name: Run govulncheck
        run: govulncheck ./...

  build:
    needs: [lint, test, security]
    runs-on: ubuntu-latest
    strategy:
      matrix:
        goos: [linux, darwin, windows]
        goarch: [amd64, arm64]
        exclude:
          - goos: windows
            goarch: arm64
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}

      - name: Build
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: |
          BINARY=mytool-${{ matrix.goos }}-${{ matrix.goarch }}
          if [ "${{ matrix.goos }}" = "windows" ]; then BINARY="${BINARY}.exe"; fi
          CGO_ENABLED=0 go build \
            -ldflags="-s -w -X main.version=${{ github.ref_name }} -X main.commit=${{ github.sha }}" \
            -o "bin/${BINARY}" \
            ./cmd/mytool

      - name: Upload artifact
        uses: actions/upload-artifact@v4
        with:
          name: mytool-${{ matrix.goos }}-${{ matrix.goarch }}
          path: bin/
```

### Essential CI Commands

```bash
# Format check (will fail if code is not formatted)
gofumpt -l -d . | grep . && exit 1 || true

# Vet: detect suspicious constructs
go vet ./...

# Lint with golangci-lint
golangci-lint run ./...

# Test with race detector and coverage
go test -race -coverprofile=coverage.out -count=1 ./...

# Show coverage summary
go tool cover -func=coverage.out

# Vulnerability scan against the Go vulnerability database
govulncheck ./...

# Check for outdated dependencies
go list -m -u all

# Verify module checksums
go mod verify
```

---

## 15. Best Practices

**Error Handling**
- Always wrap errors with context using `fmt.Errorf("doing X: %w", err)` so that error chains are meaningful.
- Define sentinel errors with `errors.New` for well-known failure conditions that callers inspect with `errors.Is`.
- Use `errors.As` when you need to extract fields from a specific error type in the chain.
- Never discard errors silently. If you intentionally ignore one, assign to a blank identifier with a comment: `_ = f.Close() // best-effort cleanup`.

**CLI Design**
- Use Cobra's `RunE` (not `Run`) so that errors propagate to the root command and control exit codes.
- Set `SilenceUsage: true` and `SilenceErrors: true` on the root command to avoid printing usage on every error.
- Provide `ValidArgsFunction` on subcommands that accept positional arguments to enable shell completion.
- Use `PersistentPreRunE` on the root command for cross-cutting concerns like config loading and logger initialization.

**Configuration**
- Layer configuration in this priority: CLI flags > environment variables > config file > defaults.
- Validate all configuration values immediately after loading, before passing them to the rest of the application.
- Use `MYTOOL_` prefixes for environment variables to avoid collisions with other programs.

**Logging**
- Use `log/slog` for new projects targeting Go 1.22+ to avoid third-party dependencies.
- Prefer structured fields (`slog.String("user_id", id)`) over string interpolation for machine-parseable logs.
- Use `slog.InfoContext(ctx, ...)` to propagate request-scoped values like trace IDs through your logger.
- Use JSON output in production and text/console output in development.

**Observability**
- Instrument all HTTP handlers with Prometheus counters (request count, duration, status code) at minimum.
- Use OpenTelemetry for distributed tracing when your service communicates with other services.
- Set a reasonable sampling rate (e.g., 10%) on the trace provider in production to control costs.
- Record errors on spans with `span.RecordError(err)` and set status with `span.SetStatus(codes.Error, msg)`.

**Modules & Builds**
- Use `go.work` for multi-module development instead of `replace` directives in `go.mod`.
- Pin tool versions in CI to prevent flaky builds from upstream updates.
- Use `-ldflags="-s -w"` in release builds to strip debug information and reduce binary size.
- Run `go mod tidy` before every commit to keep `go.mod` and `go.sum` clean.

**Docker**
- Use multi-stage builds: a `golang` image to compile, a `distroless` or `scratch` image for the final container.
- Set `CGO_ENABLED=0` for fully static binaries that run in minimal base images.
- Run as a non-root user in the final image.
- Cache `go mod download` in a separate layer before copying source code.

---

## 16. Anti-Patterns

**Using `log.Fatal` or `os.Exit` Deep in Library Code**
Calling `log.Fatal` or `os.Exit` terminates the process immediately without running deferred functions. Only the top-level `main` function should decide when to exit. Library code should return errors.

**Ignoring Context Cancellation**
Accepting a `context.Context` parameter but never checking `ctx.Err()` or passing it to downstream calls defeats the purpose of cancellation. Always propagate context and respect its deadline.

**Bare `panic` for Expected Errors**
Using `panic` instead of returning an error forces callers to use `recover` or crash. Reserve `panic` for truly unrecoverable situations like programmer bugs during initialization. Return errors for anything that can reasonably fail at runtime.

**Global Mutable State for Configuration**
Storing config in global variables that are mutated at runtime leads to data races and makes testing difficult. Pass configuration explicitly as struct values through constructors and function parameters.

**Stringly-Typed Everything**
Using `map[string]interface{}` or raw JSON everywhere instead of typed structs loses compile-time safety and makes refactoring error-prone. Always unmarshal into strongly-typed structs.

**Not Closing Resources**
Forgetting `defer f.Close()` or `defer rows.Close()` leaks file descriptors and database connections. Always close resources in a `defer` immediately after opening them, and check for errors on close when writes are involved.

**Blocking in Signal Handlers**
Performing slow I/O or complex shutdown logic directly in a signal handler goroutine can cause the process to hang. Use `signal.NotifyContext` to cancel a context, and perform shutdown in the main goroutine with a timeout.

**Over-Engineering with Interfaces Too Early**
Defining interfaces before you have multiple implementations adds complexity without benefit. Accept interfaces, return structs. Introduce interfaces when you have a genuine need for polymorphism or when writing tests.

**Committing `go.work` and `go.work.sum`**
The `go.work` file is for local development convenience and should generally be in `.gitignore`. Committing it forces all contributors to use the same module layout and can break CI builds.

**Unbounded Goroutines**
Spawning goroutines in a loop without any concurrency limit can exhaust memory and file descriptors. Use a semaphore pattern (`chan struct{}`), `sync.WaitGroup`, or `golang.org/x/sync/errgroup` with `SetLimit` to bound concurrency.

**Logging Sensitive Data**
Printing passwords, API keys, or tokens in log output is a security risk. Redact sensitive fields before logging. Use custom `slog.LogValuer` implementations to control how sensitive types are serialized.

**Skipping `go vet` and Linters in CI**
Relying solely on compilation and tests misses entire classes of bugs that static analysis catches: shadowed variables, incorrect format strings, unreachable code, and unsafe patterns. Always run `go vet` and `golangci-lint` in CI.

---

## 17. Sources & References

- Cobra CLI library documentation: https://cobra.dev/
- Viper configuration library: https://github.com/spf13/viper
- Go log/slog package (standard library): https://pkg.go.dev/log/slog
- zerolog high-performance logging: https://github.com/rs/zerolog
- OpenTelemetry Go SDK documentation: https://opentelemetry.io/docs/languages/go/
- Prometheus Go client library: https://github.com/prometheus/client_golang
- Go error handling best practices (official blog): https://go.dev/blog/error-handling-and-go
- Go modules reference: https://go.dev/ref/mod
- Go workspace mode documentation: https://go.dev/doc/tutorial/workspaces
- golangci-lint linter aggregator: https://golangci-lint.run/
- govulncheck vulnerability scanner: https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck
- Effective Go (official guide): https://go.dev/doc/effective_go
- Go by Example (practical patterns): https://gobyexample.com/
- kelseyhightower/envconfig: https://github.com/kelseyhightower/envconfig
- Taskfile task runner: https://taskfile.dev/
