---
name: go-web
description: Go web development with Gin, Echo, Fiber, and stdlib net/http+chi — REST API design, middleware, request binding/validation, JSON responses, error handling, GORM, Ent, sqlc, sqlx, database pooling, graceful shutdown, health checks, OpenAPI generation
---

# Go Web Development

Production-ready patterns for building web APIs in Go 1.22+ using Gin 1.10+, Echo v4/v5, Fiber v3, and stdlib `net/http` with chi v5 router. Covers REST API design, route groups, middleware composition (auth, CORS, logging, recovery), request binding and validation with `go-playground/validator`, JSON responses, error handling with custom error types, GORM 2.x (models, migrations, CRUD, scopes, hooks, transactions), Ent (schema, edges, queries), sqlc (type-safe SQL from queries), sqlx (named queries, scanning), database connection pooling, graceful shutdown, health checks, and OpenAPI generation with swaggo.

## Table of Contents

1. [Project Structure & Dependencies](#1-project-structure--dependencies)
2. [stdlib net/http with chi Router](#2-stdlib-nethttp-with-chi-router)
3. [Gin Framework Patterns](#3-gin-framework-patterns)
4. [Echo Framework Patterns](#4-echo-framework-patterns)
5. [Fiber Framework Patterns](#5-fiber-framework-patterns)
6. [Middleware — Auth, CORS, Logging, Recovery](#6-middleware--auth-cors-logging-recovery)
7. [Request Binding, Validation & JSON Responses](#7-request-binding-validation--json-responses)
8. [Error Handling with Custom Error Types](#8-error-handling-with-custom-error-types)
9. [GORM — Models, Migrations, CRUD, Scopes, Hooks, Transactions](#9-gorm--models-migrations-crud-scopes-hooks-transactions)
10. [Ent — Schema, Edges & Queries](#10-ent--schema-edges--queries)
11. [sqlc — Type-Safe SQL from Queries](#11-sqlc--type-safe-sql-from-queries)
12. [sqlx — Named Queries & Scanning](#12-sqlx--named-queries--scanning)
13. [Database Connection Pooling](#13-database-connection-pooling)
14. [Graceful Shutdown & Health Checks](#14-graceful-shutdown--health-checks)
15. [OpenAPI Generation](#15-openapi-generation)
16. [Best Practices](#16-best-practices)
17. [Anti-Patterns](#17-anti-patterns)
18. [Sources & References](#18-sources--references)

---

## 1. Project Structure & Dependencies

### Recommended layout

```
myapp/
  cmd/
    api/
      main.go           # entrypoint, wiring
  internal/
    handler/            # HTTP handlers per domain
      user.go
      order.go
    middleware/          # custom middleware
      auth.go
      cors.go
    model/              # domain types, GORM models
      user.go
    repository/         # data access layer
      user_repo.go
    service/            # business logic
      user_svc.go
    dto/                # request/response DTOs
      user_dto.go
    apperror/           # custom error types
      errors.go
  db/
    migrations/         # GORM AutoMigrate or golang-migrate files
    queries/            # sqlc .sql files
  ent/
    schema/             # Ent schema definitions
  docs/                 # swaggo generated OpenAPI
  go.mod
  go.sum
```

### Key dependencies (go.mod)

```
module myapp

go 1.22

require (
    github.com/gin-gonic/gin          v1.10.0
    github.com/labstack/echo/v4        v4.13.3
    github.com/gofiber/fiber/v3        v3.0.0
    github.com/go-chi/chi/v5           v5.2.1
    gorm.io/gorm                       v1.25.12
    gorm.io/driver/postgres            v1.5.11
    entgo.io/ent                       v0.14.3
    github.com/sqlc-dev/sqlc           v1.28.0
    github.com/jmoiron/sqlx            v1.4.0
    github.com/go-playground/validator/v10  v10.23.0
    github.com/golang-jwt/jwt/v5       v5.2.1
    github.com/swaggo/swag             v1.16.4
    github.com/rs/cors                 v1.11.1
)
```

---

## 2. stdlib net/http with chi Router

Go 1.22 introduced enhanced routing patterns in the standard library `net/http` mux, supporting method-based routing and path parameters. chi v5 builds on `net/http` handlers and is fully compatible with the stdlib.

### net/http 1.22 enhanced mux

```go
package main

import (
    "encoding/json"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "context"
    "time"
)

func main() {
    mux := http.NewServeMux()

    // Go 1.22+: method and path parameter syntax
    mux.HandleFunc("GET /api/users/{id}", getUser)
    mux.HandleFunc("POST /api/users", createUser)
    mux.HandleFunc("PUT /api/users/{id}", updateUser)
    mux.HandleFunc("DELETE /api/users/{id}", deleteUser)

    // Exact match vs subtree: trailing slash means subtree
    mux.HandleFunc("GET /api/health", healthCheck)

    srv := &http.Server{
        Addr:         ":8080",
        Handler:      mux,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    go func() {
        slog.Info("server starting", "addr", srv.Addr)
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            slog.Error("server failed", "error", err)
            os.Exit(1)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, os.Interrupt)
    <-quit

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    srv.Shutdown(ctx)
}

func getUser(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id") // Go 1.22+ path parameter
    json.NewEncoder(w).Encode(map[string]string{"id": id})
}
```

### chi v5 router

```go
package main

import (
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(userHandler *handler.UserHandler) http.Handler {
    r := chi.NewRouter()

    // Built-in middleware
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Timeout(30 * time.Second))

    // Health check outside auth
    r.Get("/health", healthCheck)

    // API route group with auth
    r.Route("/api/v1", func(r chi.Router) {
        r.Use(AuthMiddleware)

        r.Route("/users", func(r chi.Router) {
            r.Get("/", userHandler.List)
            r.Post("/", userHandler.Create)

            r.Route("/{userID}", func(r chi.Router) {
                r.Get("/", userHandler.Get)
                r.Put("/", userHandler.Update)
                r.Delete("/", userHandler.Delete)
            })
        })
    })

    return r
}

// chi extracts path params via chi.URLParam
func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
    userID := chi.URLParam(r, "userID")
    // ...
}
```

---

## 3. Gin Framework Patterns

Gin 1.10+ is the most popular Go web framework. It uses httprouter under the hood and provides a rich middleware ecosystem.

### Route groups and handler setup

```go
package main

import (
    "net/http"

    "github.com/gin-gonic/gin"
)

func SetupRouter(userHandler *handler.UserHandler) *gin.Engine {
    r := gin.New()

    // Global middleware
    r.Use(gin.Logger())
    r.Use(gin.Recovery())

    // Health check
    r.GET("/health", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"status": "ok"})
    })

    // API v1 group
    v1 := r.Group("/api/v1")
    v1.Use(AuthMiddleware())
    {
        users := v1.Group("/users")
        {
            users.GET("", userHandler.List)
            users.POST("", userHandler.Create)
            users.GET("/:id", userHandler.Get)
            users.PUT("/:id", userHandler.Update)
            users.DELETE("/:id", userHandler.Delete)
        }
    }

    return r
}

// Handler using Gin context
func (h *UserHandler) Get(c *gin.Context) {
    id := c.Param("id")

    user, err := h.service.GetByID(c.Request.Context(), id)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
        return
    }

    c.JSON(http.StatusOK, user)
}

// Binding JSON body with validation
func (h *UserHandler) Create(c *gin.Context) {
    var req dto.CreateUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    user, err := h.service.Create(c.Request.Context(), req)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
        return
    }

    c.JSON(http.StatusCreated, user)
}

// Query parameter binding
func (h *UserHandler) List(c *gin.Context) {
    var query dto.ListUsersQuery
    if err := c.ShouldBindQuery(&query); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    users, total, err := h.service.List(c.Request.Context(), query)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "data":  users,
        "total": total,
        "page":  query.Page,
        "limit": query.Limit,
    })
}
```

---

## 4. Echo Framework Patterns

Echo v4 (stable) provides a fast HTTP router with built-in middleware. Echo v5 has been released but may still have breaking changes through early 2026.

### Route groups and handlers

```go
package main

import (
    "net/http"

    "github.com/labstack/echo/v4"
    echomw "github.com/labstack/echo/v4/middleware"
)

func SetupRouter(userHandler *handler.UserHandler) *echo.Echo {
    e := echo.New()

    // Global middleware
    e.Use(echomw.Logger())
    e.Use(echomw.Recover())
    e.Use(echomw.RequestID())

    // CORS
    e.Use(echomw.CORSWithConfig(echomw.CORSConfig{
        AllowOrigins: []string{"https://example.com"},
        AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
        AllowHeaders: []string{echo.HeaderContentType, echo.HeaderAuthorization},
        MaxAge:       86400,
    }))

    // Health check
    e.GET("/health", func(c echo.Context) error {
        return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
    })

    // API v1 group with JWT middleware
    v1 := e.Group("/api/v1")
    v1.Use(echomw.JWTWithConfig(echomw.JWTConfig{
        SigningKey: []byte("secret"),
        Skipper: func(c echo.Context) bool {
            return c.Path() == "/api/v1/auth/login"
        },
    }))

    users := v1.Group("/users")
    users.GET("", userHandler.List)
    users.POST("", userHandler.Create)
    users.GET("/:id", userHandler.Get)
    users.PUT("/:id", userHandler.Update)
    users.DELETE("/:id", userHandler.Delete)

    return e
}

// Echo handler returns error
func (h *UserHandler) Get(c echo.Context) error {
    id := c.Param("id")

    user, err := h.service.GetByID(c.Request().Context(), id)
    if err != nil {
        return echo.NewHTTPError(http.StatusNotFound, "user not found")
    }

    return c.JSON(http.StatusOK, user)
}

// Binding with validation
func (h *UserHandler) Create(c echo.Context) error {
    var req dto.CreateUserRequest
    if err := c.Bind(&req); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, err.Error())
    }
    if err := c.Validate(&req); err != nil {
        return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
    }

    user, err := h.service.Create(c.Request().Context(), req)
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "failed to create user")
    }

    return c.JSON(http.StatusCreated, user)
}
```

---

## 5. Fiber Framework Patterns

Fiber v3 is built on fasthttp and offers an Express-like API. It requires Go 1.23+.

### Route setup

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/gofiber/fiber/v3/middleware/cors"
    "github.com/gofiber/fiber/v3/middleware/logger"
    "github.com/gofiber/fiber/v3/middleware/recover"
)

func SetupApp(userHandler *handler.UserHandler) *fiber.App {
    app := fiber.New(fiber.Config{
        ErrorHandler:  customErrorHandler,
        ReadTimeout:   10 * time.Second,
        WriteTimeout:  15 * time.Second,
        IdleTimeout:   60 * time.Second,
    })

    app.Use(logger.New())
    app.Use(recover.New())
    app.Use(cors.New(cors.Config{
        AllowOrigins: []string{"https://example.com"},
        AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
        AllowHeaders: []string{"Content-Type", "Authorization"},
        MaxAge:       86400,
    }))

    // Health check
    app.Get("/health", func(c fiber.Ctx) error {
        return c.JSON(fiber.Map{"status": "ok"})
    })

    // API v1 group
    v1 := app.Group("/api/v1", AuthMiddleware)

    users := v1.Group("/users")
    users.Get("/", userHandler.List)
    users.Post("/", userHandler.Create)
    users.Get("/:id", userHandler.Get)
    users.Put("/:id", userHandler.Update)
    users.Delete("/:id", userHandler.Delete)

    return app
}

// Fiber v3 handler
func (h *UserHandler) Get(c fiber.Ctx) error {
    id := c.Params("id")

    user, err := h.service.GetByID(c.Context(), id)
    if err != nil {
        return fiber.NewError(fiber.StatusNotFound, "user not found")
    }

    return c.JSON(user)
}

// Fiber body parsing
func (h *UserHandler) Create(c fiber.Ctx) error {
    var req dto.CreateUserRequest
    if err := c.Bind().JSON(&req); err != nil {
        return fiber.NewError(fiber.StatusBadRequest, err.Error())
    }

    user, err := h.service.Create(c.Context(), req)
    if err != nil {
        return fiber.NewError(fiber.StatusInternalServerError, "failed to create user")
    }

    return c.Status(fiber.StatusCreated).JSON(user)
}

// Custom error handler
func customErrorHandler(c fiber.Ctx, err error) error {
    code := fiber.StatusInternalServerError
    msg := "internal server error"

    var e *fiber.Error
    if errors.As(err, &e) {
        code = e.Code
        msg = e.Message
    }

    return c.Status(code).JSON(fiber.Map{
        "error":   msg,
        "code":    code,
    })
}
```

---

## 6. Middleware -- Auth, CORS, Logging, Recovery

### JWT authentication middleware (framework-agnostic pattern)

```go
package middleware

import (
    "context"
    "net/http"
    "strings"

    "github.com/golang-jwt/jwt/v5"
)

type contextKey string

const UserIDKey contextKey = "userID"

type Claims struct {
    UserID string `json:"user_id"`
    Role   string `json:"role"`
    jwt.RegisteredClaims
}

// For chi / stdlib net/http
func JWTAuth(secret string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" {
                http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
                return
            }

            tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
            if tokenStr == authHeader {
                http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
                return
            }

            claims := &Claims{}
            token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
                if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
                    return nil, jwt.ErrSignatureInvalid
                }
                return []byte(secret), nil
            })

            if err != nil || !token.Valid {
                http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
                return
            }

            ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// For Gin
func GinJWTAuth(secret string) gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

        claims := &Claims{}
        token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
            return []byte(secret), nil
        })

        if err != nil || !token.Valid {
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
            return
        }

        c.Set("userID", claims.UserID)
        c.Set("role", claims.Role)
        c.Next()
    }
}

// Role-based authorization (chi middleware)
func RequireRole(roles ...string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            role, ok := r.Context().Value("role").(string)
            if !ok {
                http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
                return
            }
            for _, allowed := range roles {
                if role == allowed {
                    next.ServeHTTP(w, r)
                    return
                }
            }
            http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
        })
    }
}
```

### CORS middleware with rs/cors (stdlib/chi)

```go
import "github.com/rs/cors"

func CORSMiddleware() func(http.Handler) http.Handler {
    c := cors.New(cors.Options{
        AllowedOrigins:   []string{"https://example.com", "https://app.example.com"},
        AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
        AllowedHeaders:   []string{"Content-Type", "Authorization"},
        ExposedHeaders:   []string{"X-Request-ID"},
        AllowCredentials: true,
        MaxAge:           86400,
    })
    return c.Handler
}
```

### Structured logging middleware with slog

```go
func LoggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

        next.ServeHTTP(ww, r)

        slog.Info("request",
            "method", r.Method,
            "path", r.URL.Path,
            "status", ww.statusCode,
            "duration_ms", time.Since(start).Milliseconds(),
            "remote", r.RemoteAddr,
            "request_id", r.Header.Get("X-Request-ID"),
        )
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

## 7. Request Binding, Validation & JSON Responses

### DTOs with go-playground/validator tags

```go
package dto

import "time"

type CreateUserRequest struct {
    Name     string `json:"name"     binding:"required,min=2,max=100"   validate:"required,min=2,max=100"`
    Email    string `json:"email"    binding:"required,email"           validate:"required,email"`
    Password string `json:"password" binding:"required,min=8"           validate:"required,min=8"`
    Role     string `json:"role"     binding:"omitempty,oneof=admin user" validate:"omitempty,oneof=admin user"`
}

type UpdateUserRequest struct {
    Name  *string `json:"name,omitempty"  binding:"omitempty,min=2,max=100" validate:"omitempty,min=2,max=100"`
    Email *string `json:"email,omitempty" binding:"omitempty,email"         validate:"omitempty,email"`
}

type ListUsersQuery struct {
    Page   int    `form:"page"   binding:"omitempty,min=1"           validate:"omitempty,min=1"`
    Limit  int    `form:"limit"  binding:"omitempty,min=1,max=100"   validate:"omitempty,min=1,max=100"`
    Search string `form:"search" binding:"omitempty,max=200"         validate:"omitempty,max=200"`
    SortBy string `form:"sort_by" binding:"omitempty,oneof=name email created_at"`
}

type UserResponse struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Email     string    `json:"email"`
    Role      string    `json:"role"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type PaginatedResponse[T any] struct {
    Data       []T   `json:"data"`
    Total      int64 `json:"total"`
    Page       int   `json:"page"`
    Limit      int   `json:"limit"`
    TotalPages int   `json:"total_pages"`
}
```

### Custom validator setup (for Echo / stdlib)

```go
package middleware

import (
    "github.com/go-playground/validator/v10"
)

type CustomValidator struct {
    validate *validator.Validate
}

func NewValidator() *CustomValidator {
    v := validator.New()

    // Register custom validations
    v.RegisterValidation("slug", func(fl validator.FieldLevel) bool {
        return regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`).MatchString(fl.Field().String())
    })

    return &CustomValidator{validate: v}
}

// For Echo
func (cv *CustomValidator) Validate(i interface{}) error {
    return cv.validate.Struct(i)
}

// For stdlib/chi - manual validation helper
func ValidateStruct(v *validator.Validate, s interface{}) map[string]string {
    err := v.Struct(s)
    if err == nil {
        return nil
    }
    errs := make(map[string]string)
    for _, e := range err.(validator.ValidationErrors) {
        errs[e.Field()] = formatValidationError(e)
    }
    return errs
}

func formatValidationError(e validator.FieldError) string {
    switch e.Tag() {
    case "required":
        return "this field is required"
    case "email":
        return "must be a valid email"
    case "min":
        return "must be at least " + e.Param() + " characters"
    case "max":
        return "must be at most " + e.Param() + " characters"
    default:
        return "invalid value"
    }
}
```

### Standard JSON response helpers

```go
package handler

import (
    "encoding/json"
    "net/http"
)

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
    respondJSON(w, status, map[string]interface{}{
        "error":   message,
        "code":    status,
    })
}

func respondPaginated[T any](w http.ResponseWriter, data []T, total int64, page, limit int) {
    totalPages := int(total) / limit
    if int(total)%limit != 0 {
        totalPages++
    }
    respondJSON(w, http.StatusOK, dto.PaginatedResponse[T]{
        Data:       data,
        Total:      total,
        Page:       page,
        Limit:      limit,
        TotalPages: totalPages,
    })
}
```

---

## 8. Error Handling with Custom Error Types

### Application error type

```go
package apperror

import (
    "errors"
    "fmt"
    "net/http"
)

type AppError struct {
    Code    int    `json:"-"`
    Message string `json:"error"`
    Detail  string `json:"detail,omitempty"`
    Err     error  `json:"-"`
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

// Constructor helpers
func NotFound(resource string) *AppError {
    return &AppError{
        Code:    http.StatusNotFound,
        Message: fmt.Sprintf("%s not found", resource),
    }
}

func BadRequest(msg string) *AppError {
    return &AppError{Code: http.StatusBadRequest, Message: msg}
}

func Unauthorized(msg string) *AppError {
    return &AppError{Code: http.StatusUnauthorized, Message: msg}
}

func Forbidden(msg string) *AppError {
    return &AppError{Code: http.StatusForbidden, Message: msg}
}

func Conflict(msg string) *AppError {
    return &AppError{Code: http.StatusConflict, Message: msg}
}

func Internal(err error) *AppError {
    return &AppError{
        Code:    http.StatusInternalServerError,
        Message: "internal server error",
        Err:     err,
    }
}

func Wrap(err error, msg string) *AppError {
    return &AppError{
        Code:    http.StatusInternalServerError,
        Message: msg,
        Err:     err,
    }
}

// Resolve error to AppError, defaulting to 500 for unknown types
func Resolve(err error) *AppError {
    var appErr *AppError
    if errors.As(err, &appErr) {
        return appErr
    }
    return Internal(err)
}
```

### Error middleware for chi / stdlib

```go
// Wrap handlers that return (T, error) into http.HandlerFunc
type AppHandler func(w http.ResponseWriter, r *http.Request) error

func HandleError(h AppHandler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if err := h(w, r); err != nil {
            appErr := apperror.Resolve(err)

            // Log internal errors, not client errors
            if appErr.Code >= 500 {
                slog.Error("internal error",
                    "error", appErr.Err,
                    "path", r.URL.Path,
                    "method", r.Method,
                )
            }

            respondJSON(w, appErr.Code, appErr)
        }
    }
}
```

### Using with Gin

```go
// Gin error middleware — add as the first middleware
func GinErrorHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()

        if len(c.Errors) > 0 {
            err := c.Errors.Last().Err
            appErr := apperror.Resolve(err)

            if appErr.Code >= 500 {
                slog.Error("internal error", "error", appErr.Err, "path", c.Request.URL.Path)
            }

            c.JSON(appErr.Code, appErr)
        }
    }
}

// Handler uses c.Error() to propagate
func (h *UserHandler) Get(c *gin.Context) {
    user, err := h.service.GetByID(c.Request.Context(), c.Param("id"))
    if err != nil {
        c.Error(err) // propagated to error middleware
        return
    }
    c.JSON(http.StatusOK, user)
}
```

---

## 9. GORM -- Models, Migrations, CRUD, Scopes, Hooks, Transactions

### Model definitions

```go
package model

import (
    "time"

    "gorm.io/gorm"
)

type User struct {
    ID        string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    Name      string         `gorm:"type:varchar(100);not null"                     json:"name"`
    Email     string         `gorm:"type:varchar(255);uniqueIndex;not null"         json:"email"`
    Password  string         `gorm:"type:varchar(255);not null"                     json:"-"`
    Role      string         `gorm:"type:varchar(20);default:'user'"                json:"role"`
    IsActive  bool           `gorm:"default:true"                                   json:"is_active"`
    Posts     []Post         `gorm:"foreignKey:AuthorID"                            json:"posts,omitempty"`
    CreatedAt time.Time      `                                                      json:"created_at"`
    UpdatedAt time.Time      `                                                      json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index"                                          json:"-"`
}

type Post struct {
    ID        string         `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    Title     string         `gorm:"type:varchar(255);not null"                     json:"title"`
    Body      string         `gorm:"type:text"                                      json:"body"`
    Status    string         `gorm:"type:varchar(20);default:'draft';index"         json:"status"`
    AuthorID  string         `gorm:"type:uuid;not null;index"                       json:"author_id"`
    Author    User           `gorm:"foreignKey:AuthorID"                            json:"author,omitempty"`
    Tags      []Tag          `gorm:"many2many:post_tags"                            json:"tags,omitempty"`
    CreatedAt time.Time      `                                                      json:"created_at"`
    UpdatedAt time.Time      `                                                      json:"updated_at"`
    DeletedAt gorm.DeletedAt `gorm:"index"                                          json:"-"`
}

type Tag struct {
    ID    string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
    Name  string `gorm:"type:varchar(50);uniqueIndex;not null"          json:"name"`
    Posts []Post `gorm:"many2many:post_tags"                            json:"posts,omitempty"`
}
```

### Migrations

```go
func AutoMigrate(db *gorm.DB) error {
    return db.AutoMigrate(
        &model.User{},
        &model.Post{},
        &model.Tag{},
    )
}
```

### CRUD operations

```go
package repository

import (
    "context"

    "gorm.io/gorm"
)

type UserRepo struct {
    db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo {
    return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, user *model.User) error {
    return r.db.WithContext(ctx).Create(user).Error
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
    var user model.User
    err := r.db.WithContext(ctx).
        Preload("Posts", func(db *gorm.DB) *gorm.DB {
            return db.Order("created_at DESC").Limit(10)
        }).
        First(&user, "id = ?", id).Error
    if err != nil {
        return nil, err
    }
    return &user, nil
}

func (r *UserRepo) List(ctx context.Context, page, limit int, search string) ([]model.User, int64, error) {
    var users []model.User
    var total int64

    query := r.db.WithContext(ctx).Model(&model.User{})
    if search != "" {
        query = query.Where("name ILIKE ? OR email ILIKE ?", "%"+search+"%", "%"+search+"%")
    }

    err := query.Count(&total).Error
    if err != nil {
        return nil, 0, err
    }

    err = query.
        Offset((page - 1) * limit).
        Limit(limit).
        Order("created_at DESC").
        Find(&users).Error

    return users, total, err
}

func (r *UserRepo) Update(ctx context.Context, id string, updates map[string]interface{}) error {
    return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).Updates(updates).Error
}

func (r *UserRepo) Delete(ctx context.Context, id string) error {
    return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.User{}).Error
}
```

### Scopes

```go
// Reusable query scopes
func ActiveUsers(db *gorm.DB) *gorm.DB {
    return db.Where("is_active = ?", true)
}

func ByRole(role string) func(db *gorm.DB) *gorm.DB {
    return func(db *gorm.DB) *gorm.DB {
        return db.Where("role = ?", role)
    }
}

func Paginate(page, limit int) func(db *gorm.DB) *gorm.DB {
    return func(db *gorm.DB) *gorm.DB {
        offset := (page - 1) * limit
        return db.Offset(offset).Limit(limit)
    }
}

// Usage:
// db.Scopes(ActiveUsers, ByRole("admin"), Paginate(1, 20)).Find(&users)
```

### Hooks

```go
import "golang.org/x/crypto/bcrypt"

func (u *User) BeforeCreate(tx *gorm.DB) error {
    hashed, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
    if err != nil {
        return err
    }
    u.Password = string(hashed)
    return nil
}

func (u *User) AfterCreate(tx *gorm.DB) error {
    slog.Info("user created", "id", u.ID, "email", u.Email)
    return nil
}
```

### Transactions

```go
func (r *UserRepo) CreateWithProfile(ctx context.Context, user *model.User, profile *model.Profile) error {
    return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := tx.Create(user).Error; err != nil {
            return err
        }
        profile.UserID = user.ID
        if err := tx.Create(profile).Error; err != nil {
            return err // automatic rollback
        }
        return nil // commit
    })
}

// Nested transactions with SavePoint
func (r *OrderRepo) ProcessOrder(ctx context.Context, order *model.Order) error {
    return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        if err := tx.Create(order).Error; err != nil {
            return err
        }

        // Nested savepoint — failure rolls back only this block
        err := tx.Transaction(func(tx2 *gorm.DB) error {
            return tx2.Create(&model.AuditLog{
                Action:   "order_created",
                EntityID: order.ID,
            }).Error
        })
        if err != nil {
            slog.Warn("audit log failed, continuing", "error", err)
            // swallow error — order still commits
        }

        return nil
    })
}
```

---

## 10. Ent -- Schema, Edges & Queries

Ent is a code-generation-based ORM. Define schemas in Go, then run `go generate` to produce type-safe query builders.

### Schema definition

```go
// ent/schema/user.go
package schema

import (
    "entgo.io/ent"
    "entgo.io/ent/schema/edge"
    "entgo.io/ent/schema/field"
    "entgo.io/ent/schema/index"
)

type User struct {
    ent.Schema
}

func (User) Fields() []ent.Field {
    return []ent.Field{
        field.String("name").NotEmpty().MaxLen(100),
        field.String("email").Unique().NotEmpty(),
        field.String("password").Sensitive(),
        field.Enum("role").Values("admin", "user").Default("user"),
        field.Bool("is_active").Default(true),
        field.Time("created_at").Default(time.Now).Immutable(),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}

func (User) Edges() []ent.Edge {
    return []ent.Edge{
        edge.To("posts", Post.Type),        // User has many Posts
        edge.To("profile", Profile.Type).   // User has one Profile
            Unique(),
    }
}

func (User) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("email").Unique(),
        index.Fields("role", "is_active"),
    }
}
```

### Generated queries

```go
// After running: go generate ./ent
func (r *UserRepo) GetByID(ctx context.Context, id int) (*ent.User, error) {
    return r.client.User.
        Query().
        Where(user.ID(id)).
        WithPosts(func(q *ent.PostQuery) {
            q.Order(ent.Desc(post.FieldCreatedAt)).
                Limit(10)
        }).
        Only(ctx)
}

func (r *UserRepo) ListActive(ctx context.Context, offset, limit int) ([]*ent.User, error) {
    return r.client.User.
        Query().
        Where(
            user.IsActive(true),
            user.RoleEQ(user.RoleUser),
        ).
        Offset(offset).
        Limit(limit).
        Order(ent.Desc(user.FieldCreatedAt)).
        All(ctx)
}

// Transaction
func (r *UserRepo) CreateWithProfile(ctx context.Context, name, email string) (*ent.User, error) {
    tx, err := r.client.Tx(ctx)
    if err != nil {
        return nil, err
    }
    defer func() {
        if v := recover(); v != nil {
            tx.Rollback()
            panic(v)
        }
    }()

    u, err := tx.User.Create().
        SetName(name).
        SetEmail(email).
        SetPassword(hashedPassword).
        Save(ctx)
    if err != nil {
        return nil, rollback(tx, err)
    }

    _, err = tx.Profile.Create().
        SetUser(u).
        SetBio("").
        Save(ctx)
    if err != nil {
        return nil, rollback(tx, err)
    }

    if err := tx.Commit(); err != nil {
        return nil, err
    }
    return u, nil
}

func rollback(tx *ent.Tx, err error) error {
    if rerr := tx.Rollback(); rerr != nil {
        return fmt.Errorf("rollback failed: %w: %v", err, rerr)
    }
    return err
}
```

---

## 11. sqlc -- Type-Safe SQL from Queries

sqlc generates type-safe Go code from SQL queries. Write SQL, get Go functions.

### sqlc.yaml configuration

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "db/queries/"
    schema: "db/migrations/"
    gen:
      go:
        package: "sqlcdb"
        out: "internal/sqlcdb"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_prepared_queries: true
        emit_interface: true
        overrides:
          - db_type: "uuid"
            go_type: "github.com/google/uuid.UUID"
          - db_type: "timestamptz"
            go_type: "time.Time"
```

### SQL query files

```sql
-- db/queries/users.sql

-- name: GetUser :one
SELECT id, name, email, role, is_active, created_at, updated_at
FROM users
WHERE id = $1;

-- name: ListUsers :many
SELECT id, name, email, role, is_active, created_at, updated_at
FROM users
WHERE is_active = true
  AND (name ILIKE '%' || @search::text || '%' OR email ILIKE '%' || @search::text || '%')
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountUsers :one
SELECT count(*) FROM users WHERE is_active = true;

-- name: CreateUser :one
INSERT INTO users (name, email, password, role)
VALUES ($1, $2, $3, $4)
RETURNING id, name, email, role, is_active, created_at, updated_at;

-- name: UpdateUser :one
UPDATE users
SET name = COALESCE(sqlc.narg('name'), name),
    email = COALESCE(sqlc.narg('email'), email),
    updated_at = now()
WHERE id = $1
RETURNING id, name, email, role, is_active, created_at, updated_at;

-- name: SoftDeleteUser :exec
UPDATE users SET is_active = false, updated_at = now() WHERE id = $1;
```

### Generated code usage

```go
// Generated code produces typed functions:
func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*sqlcdb.User, error) {
    return r.queries.GetUser(ctx, id)
}

func (r *UserRepo) List(ctx context.Context, search string, limit, offset int32) ([]sqlcdb.User, error) {
    return r.queries.ListUsers(ctx, sqlcdb.ListUsersParams{
        Search: search,
        Limit:  limit,
        Offset: offset,
    })
}

func (r *UserRepo) Create(ctx context.Context, params sqlcdb.CreateUserParams) (*sqlcdb.User, error) {
    user, err := r.queries.CreateUser(ctx, params)
    if err != nil {
        return nil, fmt.Errorf("create user: %w", err)
    }
    return &user, nil
}
```

---

## 12. sqlx -- Named Queries & Scanning

sqlx extends `database/sql` with struct scanning, named queries, and `IN` clause expansion.

### Setup and usage

```go
package repository

import (
    "context"

    "github.com/jmoiron/sqlx"
    _ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
)

type UserRepo struct {
    db *sqlx.DB
}

func NewDB(dsn string) (*sqlx.DB, error) {
    db, err := sqlx.Connect("pgx", dsn)
    if err != nil {
        return nil, fmt.Errorf("connect db: %w", err)
    }

    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(10)
    db.SetConnMaxLifetime(5 * time.Minute)
    db.SetConnMaxIdleTime(1 * time.Minute)

    return db, nil
}

// Struct scanning (fields matched by db tag)
type User struct {
    ID        string    `db:"id"         json:"id"`
    Name      string    `db:"name"       json:"name"`
    Email     string    `db:"email"      json:"email"`
    Role      string    `db:"role"       json:"role"`
    IsActive  bool      `db:"is_active"  json:"is_active"`
    CreatedAt time.Time `db:"created_at" json:"created_at"`
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*User, error) {
    var user User
    err := r.db.GetContext(ctx, &user,
        `SELECT id, name, email, role, is_active, created_at FROM users WHERE id = $1`, id)
    if err != nil {
        return nil, err
    }
    return &user, nil
}

func (r *UserRepo) List(ctx context.Context, page, limit int) ([]User, error) {
    var users []User
    err := r.db.SelectContext(ctx, &users,
        `SELECT id, name, email, role, is_active, created_at
         FROM users WHERE is_active = true
         ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
        limit, (page-1)*limit)
    return users, err
}

// Named queries
func (r *UserRepo) Create(ctx context.Context, user *User) error {
    query := `INSERT INTO users (name, email, role)
              VALUES (:name, :email, :role)
              RETURNING id, created_at`
    rows, err := r.db.NamedQueryContext(ctx, query, user)
    if err != nil {
        return err
    }
    defer rows.Close()
    if rows.Next() {
        return rows.StructScan(user)
    }
    return fmt.Errorf("no rows returned")
}

// IN clause expansion
func (r *UserRepo) GetByIDs(ctx context.Context, ids []string) ([]User, error) {
    query, args, err := sqlx.In(
        `SELECT id, name, email FROM users WHERE id IN (?)`, ids)
    if err != nil {
        return nil, err
    }
    query = r.db.Rebind(query) // convert ? to $1, $2, ...
    var users []User
    err = r.db.SelectContext(ctx, &users, query, args...)
    return users, err
}
```

---

## 13. Database Connection Pooling

### Production pool configuration

```go
package database

import (
    "context"
    "fmt"
    "log/slog"
    "time"

    "gorm.io/driver/postgres"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

type Config struct {
    Host            string
    Port            int
    User            string
    Password        string
    DBName          string
    SSLMode         string
    MaxOpenConns    int
    MaxIdleConns    int
    ConnMaxLifetime time.Duration
    ConnMaxIdleTime time.Duration
}

func NewGormDB(cfg Config) (*gorm.DB, error) {
    dsn := fmt.Sprintf(
        "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
        cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
    )

    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
        Logger:                 logger.Default.LogMode(logger.Warn),
        SkipDefaultTransaction: true,  // disable default per-query transaction for reads
        PrepareStmt:            true,  // cache prepared statements
    })
    if err != nil {
        return nil, fmt.Errorf("open db: %w", err)
    }

    sqlDB, err := db.DB()
    if err != nil {
        return nil, fmt.Errorf("get underlying sql.DB: %w", err)
    }

    // Pool tuning
    sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)       // default: 25
    sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)       // default: 10
    sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)  // default: 5m — prevents stale conns
    sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)  // default: 1m — reclaims idle conns

    // Verify connectivity
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := sqlDB.PingContext(ctx); err != nil {
        return nil, fmt.Errorf("ping db: %w", err)
    }

    slog.Info("database connected",
        "host", cfg.Host,
        "db", cfg.DBName,
        "max_open", cfg.MaxOpenConns,
        "max_idle", cfg.MaxIdleConns,
    )

    return db, nil
}
```

### Pool sizing guidelines

- **MaxOpenConns**: Match your database max_connections divided by the number of app instances. A good starting point is 25 per instance.
- **MaxIdleConns**: Set to roughly half of MaxOpenConns. Too low causes frequent reconnections; too high wastes server memory.
- **ConnMaxLifetime**: 5 minutes prevents holding connections that the database or load balancer has already closed.
- **ConnMaxIdleTime**: 1 minute reclaims idle connections during low-traffic periods.

---

## 14. Graceful Shutdown & Health Checks

### Full graceful shutdown pattern

```go
package main

import (
    "context"
    "errors"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "gorm.io/gorm"
)

func main() {
    db, err := database.NewGormDB(loadConfig())
    if err != nil {
        slog.Error("failed to connect db", "error", err)
        os.Exit(1)
    }

    router := SetupRouter(db)

    srv := &http.Server{
        Addr:         ":8080",
        Handler:      router,
        ReadTimeout:  10 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    // Start server in goroutine
    go func() {
        slog.Info("server starting", "addr", srv.Addr)
        if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            slog.Error("server error", "error", err)
            os.Exit(1)
        }
    }()

    // Wait for interrupt signal
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    sig := <-quit
    slog.Info("shutting down", "signal", sig.String())

    // Give in-flight requests time to complete
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := srv.Shutdown(ctx); err != nil {
        slog.Error("server shutdown error", "error", err)
    }

    // Close database
    sqlDB, _ := db.DB()
    if err := sqlDB.Close(); err != nil {
        slog.Error("db close error", "error", err)
    }

    slog.Info("server stopped")
}
```

### Health check endpoints

```go
package handler

import (
    "context"
    "net/http"
    "time"

    "gorm.io/gorm"
)

type HealthHandler struct {
    db      *gorm.DB
    version string
}

func NewHealthHandler(db *gorm.DB, version string) *HealthHandler {
    return &HealthHandler{db: db, version: version}
}

// Liveness probe — is the process alive?
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]string{
        "status":  "ok",
        "version": h.version,
    })
}

// Readiness probe — can the service accept traffic?
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    sqlDB, err := h.db.DB()
    if err != nil {
        respondJSON(w, http.StatusServiceUnavailable, map[string]string{
            "status": "error",
            "db":     "unavailable",
        })
        return
    }

    if err := sqlDB.PingContext(ctx); err != nil {
        respondJSON(w, http.StatusServiceUnavailable, map[string]string{
            "status": "error",
            "db":     "unreachable",
        })
        return
    }

    respondJSON(w, http.StatusOK, map[string]string{
        "status": "ready",
        "db":     "connected",
    })
}

// Register with chi
func RegisterHealthRoutes(r chi.Router, h *HealthHandler) {
    r.Get("/healthz", h.Liveness)
    r.Get("/readyz", h.Readiness)
}
```

---

## 15. OpenAPI Generation

### swaggo for Gin

Install: `go install github.com/swaggo/swag/cmd/swag@latest`

Add annotations to handlers:

```go
// @Summary      Get user by ID
// @Description  Fetch a single user by their UUID
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "User ID (UUID)"
// @Success      200  {object}  dto.UserResponse
// @Failure      404  {object}  apperror.AppError
// @Failure      500  {object}  apperror.AppError
// @Security     BearerAuth
// @Router       /api/v1/users/{id} [get]
func (h *UserHandler) Get(c *gin.Context) {
    // ...
}

// @Summary      Create a user
// @Description  Create a new user account
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        body  body      dto.CreateUserRequest  true  "User data"
// @Success      201   {object}  dto.UserResponse
// @Failure      400   {object}  apperror.AppError
// @Failure      409   {object}  apperror.AppError
// @Security     BearerAuth
// @Router       /api/v1/users [post]
func (h *UserHandler) Create(c *gin.Context) {
    // ...
}

// @Summary      List users
// @Tags         users
// @Produce      json
// @Param        page    query     int     false  "Page number"     default(1)
// @Param        limit   query     int     false  "Items per page"  default(20)
// @Param        search  query     string  false  "Search by name or email"
// @Success      200     {object}  dto.PaginatedResponse[dto.UserResponse]
// @Security     BearerAuth
// @Router       /api/v1/users [get]
func (h *UserHandler) List(c *gin.Context) {
    // ...
}
```

### Main file annotations and setup

```go
// @title           My API
// @version         1.0
// @description     REST API server
// @host            localhost:8080
// @BasePath        /api/v1
// @securityDefinitions.apikey  BearerAuth
// @in                          header
// @name                        Authorization
package main

import (
    "github.com/gin-gonic/gin"
    swaggerFiles "github.com/swaggo/files"
    ginSwagger "github.com/swaggo/gin-swagger"
    _ "myapp/docs" // generated by swag init
)

func SetupRouter() *gin.Engine {
    r := gin.New()

    // Swagger UI at /swagger/index.html
    r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

    // ...routes...
    return r
}
```

Generate docs: `swag init -g cmd/api/main.go -o docs/`

### huma for chi (alternative)

huma provides OpenAPI 3.1 generation built into the router with automatic request/response validation.

```go
import (
    "github.com/danielgtaylor/huma/v2"
    "github.com/danielgtaylor/huma/v2/adapters/humachi"
    "github.com/go-chi/chi/v5"
)

type CreateUserInput struct {
    Body struct {
        Name  string `json:"name"  minLength:"2" maxLength:"100" doc:"User name"`
        Email string `json:"email" format:"email"                doc:"User email"`
    }
}

type UserOutput struct {
    Body dto.UserResponse
}

func RegisterRoutes(r chi.Router) {
    api := humachi.New(r, huma.DefaultConfig("My API", "1.0.0"))

    huma.Register(api, huma.Operation{
        OperationID: "create-user",
        Method:      http.MethodPost,
        Path:        "/api/v1/users",
        Summary:     "Create a user",
        Tags:        []string{"users"},
    }, func(ctx context.Context, input *CreateUserInput) (*UserOutput, error) {
        // input is already validated by huma
        user, err := svc.Create(ctx, input.Body.Name, input.Body.Email)
        if err != nil {
            return nil, huma.Error404NotFound("user not found")
        }
        return &UserOutput{Body: *user}, nil
    })
}
```

---

## 16. Best Practices

1. **Use `context.Context` everywhere** -- Pass request context from handlers through service and repository layers. This enables proper cancellation and timeout propagation.

2. **Prefer chi or stdlib 1.22+ for new projects** -- chi is fully compatible with `net/http` middleware. The Go 1.22 enhanced mux reduces the need for a third-party router for simple APIs.

3. **Separate handler, service, repository layers** -- Handlers bind/validate requests and produce responses. Services contain business logic. Repositories own data access. This enables unit testing each layer independently.

4. **Use structured logging with `log/slog`** -- stdlib slog (Go 1.21+) provides structured, leveled logging without external dependencies. Use JSON handler in production.

5. **Always set HTTP server timeouts** -- ReadTimeout, WriteTimeout, and IdleTimeout prevent slowloris attacks and resource exhaustion.

6. **GORM: Set `SkipDefaultTransaction: true`** -- GORM wraps every query in a transaction by default. Disabling this for read-heavy workloads improves throughput by 30%+.

7. **GORM: Use `PrepareStmt: true`** -- Caches prepared statements, reducing database round-trips for repeated queries.

8. **Prefer sqlc for performance-critical paths** -- sqlc generates zero-reflection code from raw SQL. For complex queries or performance-sensitive endpoints, it avoids ORM overhead entirely.

9. **Connection pool tuning** -- Set MaxOpenConns, MaxIdleConns, ConnMaxLifetime, and ConnMaxIdleTime based on your database capacity and traffic patterns. Monitor pool stats via `sql.DB.Stats()`.

10. **Implement both liveness and readiness probes** -- Liveness checks if the process is running. Readiness checks if the service can handle requests (database connected, caches warm). Kubernetes and load balancers rely on these.

11. **Generate OpenAPI specs from code** -- Use swaggo annotations or huma to keep documentation in sync with implementation. Serve Swagger UI in non-production environments.

12. **Graceful shutdown with signal handling** -- Catch SIGINT/SIGTERM, stop accepting new connections, drain in-flight requests, then close database connections.

---

## 17. Anti-Patterns

- **String concatenation in SQL queries** -- Always use parameterized queries (`$1`, named params) to prevent SQL injection. This applies to GORM `.Where()`, sqlx, and sqlc alike.

- **Ignoring `context.Context`** -- Not passing context through the call chain breaks cancellation, timeouts, and tracing. Every database call should use `WithContext(ctx)`.

- **Returning raw database errors to clients** -- Database errors leak schema information. Wrap errors in `AppError` types and return sanitized messages.

- **Using GORM `AutoMigrate` in production** -- AutoMigrate is useful for development but does not handle column drops, renames, or data migrations. Use golang-migrate or Atlas for production schema changes.

- **Unbounded queries without pagination** -- Listing endpoints without LIMIT/OFFSET cause memory exhaustion and slow responses. Always enforce maximum page sizes.

- **Not setting connection pool limits** -- The default `database/sql` pool has no limit on open connections. Under load, this can overwhelm the database with hundreds of connections.

- **Global database variables** -- Passing `*gorm.DB` or `*sqlx.DB` through global variables prevents testing and makes dependency graphs invisible. Use dependency injection.

- **Ignoring `gorm.ErrRecordNotFound`** -- Not checking for this sentinel error causes 500 responses when a 404 is appropriate. Use `errors.Is(err, gorm.ErrRecordNotFound)`.

- **Using Fiber's `*fiber.Ctx` after handler returns** -- Fiber reuses context objects from a pool. Storing or passing `fiber.Ctx` to goroutines causes data races. Copy needed values before spawning goroutines.

- **Skipping request validation** -- Binding without validation allows malformed data into the service layer. Always pair `ShouldBindJSON` with validator tags or a separate validation step.

- **Monolithic handler functions** -- Handlers that contain business logic, database queries, and response formatting are untestable. Extract logic into service and repository layers.

---

## 18. Sources & References

- [Go net/http ServeMux Routing Enhancements (Go 1.22)](https://go.dev/blog/routing-enhancements)
- [chi -- Lightweight, Idiomatic Go HTTP Router](https://github.com/go-chi/chi)
- [Gin Web Framework Documentation](https://gin-gonic.com/en/docs/)
- [Echo -- High Performance Go Web Framework](https://echo.labstack.com/docs)
- [Fiber -- Express-Inspired Go Web Framework](https://docs.gofiber.io/)
- [GORM Guides -- The Fantastic ORM Library for Go](https://gorm.io/docs/)
- [Ent -- An Entity Framework for Go](https://entgo.io/docs/getting-started/)
- [sqlc Documentation -- Generate Type-Safe Go from SQL](https://docs.sqlc.dev/)
- [sqlx -- Extensions to database/sql](https://github.com/jmoiron/sqlx)
- [go-playground/validator -- Struct and Field Validation](https://github.com/go-playground/validator)
- [golang-jwt/jwt -- Go JWT Implementation](https://github.com/golang-jwt/jwt)
- [swaggo/swag -- Go Swagger Generator](https://github.com/swaggo/swag)
- [huma -- Modern Go REST/HTTP API Framework with OpenAPI 3.1](https://github.com/danielgtaylor/huma)
