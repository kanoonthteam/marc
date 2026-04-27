---
name: go-testing
description: Go testing patterns — go test basics, table-driven tests, testify assertions, httptest for handlers, gomock and manual test doubles, testcontainers-go for integration tests, golden files, benchmarks, fuzz testing, coverage, parallel tests, and TestMain
---

# Go Testing Patterns

Production-ready testing patterns for Go 1.22+ applications. Covers `go test` fundamentals, table-driven tests with `t.Run()`, testify (assert, require, suite), `net/http/httptest` for HTTP handler testing, gomock for interface mocking, manual test doubles (fakes, stubs, spies), testcontainers-go for integration tests against real databases, golden file testing, benchmark tests, fuzz testing, test coverage with `go test -cover`, test fixtures and helpers, testing with context and timeouts, parallel tests with `t.Parallel()`, `TestMain` for global setup/teardown, testing private functions, `testing/fstest` for filesystem testing, and `testing/slogtest` for structured log verification.

## Table of Contents

1. [Go Test Basics](#go-test-basics)
2. [Table-Driven Tests with t.Run()](#table-driven-tests-with-trun)
3. [Testify: Assert, Require, and Suite](#testify-assert-require-and-suite)
4. [HTTP Handler Testing with httptest](#http-handler-testing-with-httptest)
5. [Gomock for Interface Mocking](#gomock-for-interface-mocking)
6. [Manual Test Doubles: Fakes, Stubs, Spies](#manual-test-doubles-fakes-stubs-spies)
7. [Testcontainers-Go for Integration Tests](#testcontainers-go-for-integration-tests)
8. [Golden File Testing](#golden-file-testing)
9. [Benchmark Tests](#benchmark-tests)
10. [Fuzz Testing](#fuzz-testing)
11. [Test Coverage](#test-coverage)
12. [Test Fixtures and Helpers](#test-fixtures-and-helpers)
13. [Testing with Context and Timeouts](#testing-with-context-and-timeouts)
14. [Parallel Tests with t.Parallel()](#parallel-tests-with-tparallel)
15. [TestMain for Setup and Teardown](#testmain-for-setup-and-teardown)
16. [Testing Private Functions](#testing-private-functions)
17. [Filesystem Testing with testing/fstest](#filesystem-testing-with-testingfstest)
18. [Structured Log Testing with testing/slogtest](#structured-log-testing-with-testingslogtest)
19. [Best Practices](#best-practices)
20. [Anti-Patterns](#anti-patterns)
21. [Sources & References](#sources--references)

---

## Go Test Basics

Go ships with a built-in test runner. Test files live alongside source files and end in `_test.go`. Every test function starts with `Test` and receives `*testing.T`.

```go
// user_test.go
package user

import "testing"

func TestFullName(t *testing.T) {
    u := User{First: "Ada", Last: "Lovelace"}

    got := u.FullName()
    want := "Ada Lovelace"

    if got != want {
        t.Errorf("FullName() = %q, want %q", got, want)
    }
}
```

Run tests:

```bash
go test ./...                    # all packages recursively
go test -v ./user/...            # verbose, one package tree
go test -run TestFullName ./user # specific test by regex
go test -count=1 ./...           # disable test caching
go test -short ./...             # skip long-running tests
go test -timeout 30s ./...       # set global timeout
go test -shuffle=on ./...        # randomize test order (Go 1.17+)
```

Use `t.Helper()` in any function that calls `t.Errorf` or `t.Fatalf` indirectly so the failure reports the correct caller line:

```go
func assertEqual(t *testing.T, got, want string) {
    t.Helper()
    if got != want {
        t.Errorf("got %q, want %q", got, want)
    }
}
```

Use `t.Cleanup()` to register teardown functions that run after the test (and its subtests) finish:

```go
func TestWithTempDir(t *testing.T) {
    dir := t.TempDir() // auto-cleaned after test
    // ... use dir
}
```

Use `testing.Short()` to skip slow tests when running with `-short`:

```go
func TestIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }
    // ... expensive test
}
```

---

## Table-Driven Tests with t.Run()

Table-driven tests are the idiomatic Go pattern for testing multiple input/output combinations. Combined with `t.Run()`, each case gets its own subtest with a descriptive name.

```go
// calc_test.go
package calc

import "testing"

func TestAdd(t *testing.T) {
    tests := []struct {
        name string
        a, b int
        want int
    }{
        {name: "positive numbers", a: 2, b: 3, want: 5},
        {name: "negative numbers", a: -1, b: -2, want: -3},
        {name: "zero", a: 0, b: 0, want: 0},
        {name: "mixed signs", a: -5, b: 10, want: 5},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := Add(tt.a, tt.b)
            if got != tt.want {
                t.Errorf("Add(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
            }
        })
    }
}
```

Run a single subtest:

```bash
go test -run TestAdd/positive_numbers ./calc
```

For tests that verify errors:

```go
func TestDivide(t *testing.T) {
    tests := []struct {
        name    string
        a, b    float64
        want    float64
        wantErr bool
    }{
        {name: "valid division", a: 10, b: 2, want: 5},
        {name: "divide by zero", a: 10, b: 0, wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Divide(tt.a, tt.b)
            if tt.wantErr {
                if err == nil {
                    t.Fatal("expected error, got nil")
                }
                return
            }
            if err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if got != tt.want {
                t.Errorf("Divide(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
            }
        })
    }
}
```

---

## Testify: Assert, Require, and Suite

The `testify` library provides fluent assertions (`assert`), fatal assertions (`require`), and test suites.

```go
// order_test.go
package order

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestOrder_Total(t *testing.T) {
    o := NewOrder()
    o.AddItem(Item{Name: "Widget", Price: 9.99, Qty: 3})
    o.AddItem(Item{Name: "Gadget", Price: 24.50, Qty: 1})

    total := o.Total()

    assert.InDelta(t, 54.47, total, 0.01, "total should be sum of item prices * qty")
    assert.Len(t, o.Items, 2)
    assert.Equal(t, "Widget", o.Items[0].Name)
}

func TestOrder_Checkout(t *testing.T) {
    o := NewOrder()
    o.AddItem(Item{Name: "Book", Price: 12.00, Qty: 1})

    // require stops the test immediately on failure
    receipt, err := o.Checkout()
    require.NoError(t, err, "checkout should not fail")
    require.NotNil(t, receipt)

    assert.Equal(t, "confirmed", receipt.Status)
    assert.Greater(t, receipt.Total, 0.0)
}
```

Use `testify/suite` for shared setup/teardown across related tests:

```go
// repo_test.go
package repo

import (
    "database/sql"
    "testing"

    "github.com/stretchr/testify/suite"
)

type UserRepoSuite struct {
    suite.Suite
    db   *sql.DB
    repo *UserRepo
}

func (s *UserRepoSuite) SetupSuite() {
    db, err := sql.Open("postgres", testDSN)
    s.Require().NoError(err)
    s.db = db
    s.repo = NewUserRepo(db)
}

func (s *UserRepoSuite) TearDownSuite() {
    s.db.Close()
}

func (s *UserRepoSuite) SetupTest() {
    _, err := s.db.Exec("DELETE FROM users")
    s.Require().NoError(err)
}

func (s *UserRepoSuite) TestCreateUser() {
    user, err := s.repo.Create("alice@example.com", "Alice")
    s.Require().NoError(err)
    s.Assert().NotZero(user.ID)
    s.Assert().Equal("alice@example.com", user.Email)
}

func (s *UserRepoSuite) TestFindByEmail() {
    _, err := s.repo.Create("bob@example.com", "Bob")
    s.Require().NoError(err)

    found, err := s.repo.FindByEmail("bob@example.com")
    s.Require().NoError(err)
    s.Assert().Equal("Bob", found.Name)
}

func TestUserRepoSuite(t *testing.T) {
    suite.Run(t, new(UserRepoSuite))
}
```

Key differences between `assert` and `require`:
- `assert` records the failure and continues the test
- `require` records the failure and stops the test immediately (`t.FailNow()`)
- Use `require` for preconditions that, if failed, make subsequent assertions meaningless

---

## HTTP Handler Testing with httptest

The `net/http/httptest` package provides `httptest.NewRequest` and `httptest.NewRecorder` for testing handlers without starting a real server.

```go
// handler_test.go
package api

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestGetUsersHandler(t *testing.T) {
    repo := &StubUserRepo{
        users: []User{
            {ID: 1, Name: "Alice"},
            {ID: 2, Name: "Bob"},
        },
    }
    handler := NewGetUsersHandler(repo)

    req := httptest.NewRequest(http.MethodGet, "/users?limit=10", nil)
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusOK, rec.Code)
    assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

    var body []User
    err := json.NewDecoder(rec.Body).Decode(&body)
    require.NoError(t, err)
    assert.Len(t, body, 2)
    assert.Equal(t, "Alice", body[0].Name)
}

func TestCreateUserHandler(t *testing.T) {
    repo := &StubUserRepo{}
    handler := NewCreateUserHandler(repo)

    payload := `{"name":"Charlie","email":"charlie@example.com"}`
    req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(payload))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusCreated, rec.Code)

    var created User
    err := json.NewDecoder(rec.Body).Decode(&created)
    require.NoError(t, err)
    assert.Equal(t, "Charlie", created.Name)
}

func TestCreateUserHandler_ValidationError(t *testing.T) {
    repo := &StubUserRepo{}
    handler := NewCreateUserHandler(repo)

    payload := `{"name":""}` // missing required fields
    req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(payload))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()

    handler.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusBadRequest, rec.Code)
}
```

For full end-to-end HTTP testing with a running server:

```go
func TestAPIIntegration(t *testing.T) {
    mux := SetupRoutes()
    srv := httptest.NewServer(mux)
    defer srv.Close()

    resp, err := http.Get(srv.URL + "/health")
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusOK, resp.StatusCode)
}
```

For testing middleware:

```go
func TestAuthMiddleware(t *testing.T) {
    inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    tests := []struct {
        name       string
        token      string
        wantStatus int
    }{
        {name: "valid token", token: "Bearer valid-jwt", wantStatus: 200},
        {name: "missing token", token: "", wantStatus: 401},
        {name: "invalid token", token: "Bearer expired", wantStatus: 401},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := httptest.NewRequest(http.MethodGet, "/protected", nil)
            if tt.token != "" {
                req.Header.Set("Authorization", tt.token)
            }
            rec := httptest.NewRecorder()

            AuthMiddleware(inner).ServeHTTP(rec, req)

            assert.Equal(t, tt.wantStatus, rec.Code)
        })
    }
}
```

---

## Gomock for Interface Mocking

`gomock` generates type-safe mocks from Go interfaces. Use `go generate` or `mockgen` directly.

```go
// store.go
package store

//go:generate mockgen -source=store.go -destination=mock_store.go -package=store

type UserStore interface {
    GetByID(id int64) (*User, error)
    Create(user *User) error
    Delete(id int64) error
}
```

```go
// service_test.go
package store

import (
    "errors"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.uber.org/mock/gomock"
)

func TestUserService_GetUser(t *testing.T) {
    ctrl := gomock.NewController(t)

    mockStore := NewMockUserStore(ctrl)
    svc := NewUserService(mockStore)

    mockStore.EXPECT().
        GetByID(int64(42)).
        Return(&User{ID: 42, Name: "Alice"}, nil).
        Times(1)

    user, err := svc.GetUser(42)
    require.NoError(t, err)
    assert.Equal(t, "Alice", user.Name)
}

func TestUserService_GetUser_NotFound(t *testing.T) {
    ctrl := gomock.NewController(t)

    mockStore := NewMockUserStore(ctrl)
    svc := NewUserService(mockStore)

    mockStore.EXPECT().
        GetByID(int64(999)).
        Return(nil, ErrNotFound).
        Times(1)

    _, err := svc.GetUser(999)
    assert.True(t, errors.Is(err, ErrNotFound))
}

func TestUserService_Delete_CallsStore(t *testing.T) {
    ctrl := gomock.NewController(t)

    mockStore := NewMockUserStore(ctrl)
    svc := NewUserService(mockStore)

    gomock.InOrder(
        mockStore.EXPECT().GetByID(int64(1)).Return(&User{ID: 1}, nil),
        mockStore.EXPECT().Delete(int64(1)).Return(nil),
    )

    err := svc.DeleteUser(1)
    require.NoError(t, err)
}
```

Install mockgen (Go 1.22+):

```bash
go install go.uber.org/mock/mockgen@latest
```

---

## Manual Test Doubles: Fakes, Stubs, Spies

Manual test doubles give you full control without code generation.

```go
// doubles_test.go
package notification

import "sync"

// Stub — returns canned responses
type StubEmailSender struct {
    Err error
}

func (s *StubEmailSender) Send(to, subject, body string) error {
    return s.Err
}

// Spy — records calls for later inspection
type SpyEmailSender struct {
    mu    sync.Mutex
    Calls []EmailCall
}

type EmailCall struct {
    To, Subject, Body string
}

func (s *SpyEmailSender) Send(to, subject, body string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.Calls = append(s.Calls, EmailCall{To: to, Subject: subject, Body: body})
    return nil
}

// Fake — working in-memory implementation
type FakeUserRepo struct {
    mu    sync.Mutex
    users map[int64]*User
    seq   int64
}

func NewFakeUserRepo() *FakeUserRepo {
    return &FakeUserRepo{users: make(map[int64]*User)}
}

func (f *FakeUserRepo) Create(u *User) error {
    f.mu.Lock()
    defer f.mu.Unlock()
    f.seq++
    u.ID = f.seq
    f.users[u.ID] = u
    return nil
}

func (f *FakeUserRepo) GetByID(id int64) (*User, error) {
    f.mu.Lock()
    defer f.mu.Unlock()
    u, ok := f.users[id]
    if !ok {
        return nil, ErrNotFound
    }
    return u, nil
}

// Test using the spy
func TestNotificationService_SendsWelcomeEmail(t *testing.T) {
    spy := &SpyEmailSender{}
    svc := NewNotificationService(spy)

    err := svc.WelcomeUser("alice@example.com", "Alice")
    require.NoError(t, err)

    require.Len(t, spy.Calls, 1)
    assert.Equal(t, "alice@example.com", spy.Calls[0].To)
    assert.Contains(t, spy.Calls[0].Subject, "Welcome")
}
```

---

## Testcontainers-Go for Integration Tests

`testcontainers-go` spins up real Docker containers for integration tests against actual databases and services.

```go
// repo_integration_test.go
package repo

import (
    "context"
    "database/sql"
    "testing"

    _ "github.com/lib/pq"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgres(t *testing.T) *sql.DB {
    t.Helper()
    ctx := context.Background()

    pgContainer, err := postgres.Run(ctx,
        "postgres:16-alpine",
        postgres.WithDatabase("testdb"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2),
        ),
    )
    require.NoError(t, err)

    t.Cleanup(func() {
        require.NoError(t, pgContainer.Terminate(ctx))
    })

    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)

    db, err := sql.Open("postgres", connStr)
    require.NoError(t, err)

    t.Cleanup(func() { db.Close() })

    // Run migrations
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS users (
            id    SERIAL PRIMARY KEY,
            email TEXT UNIQUE NOT NULL,
            name  TEXT NOT NULL
        )
    `)
    require.NoError(t, err)

    return db
}

func TestUserRepo_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    db := setupPostgres(t)
    repo := NewUserRepo(db)
    ctx := context.Background()

    // Create
    user, err := repo.Create(ctx, "alice@example.com", "Alice")
    require.NoError(t, err)
    assert.NotZero(t, user.ID)

    // Read
    found, err := repo.FindByEmail(ctx, "alice@example.com")
    require.NoError(t, err)
    assert.Equal(t, "Alice", found.Name)

    // Duplicate detection
    _, err = repo.Create(ctx, "alice@example.com", "Alice2")
    assert.Error(t, err)
}
```

For Redis:

```go
import "github.com/testcontainers/testcontainers-go/modules/redis"

func setupRedis(t *testing.T) *redis.RedisContainer {
    t.Helper()
    ctx := context.Background()

    container, err := redis.Run(ctx, "redis:7-alpine")
    require.NoError(t, err)
    t.Cleanup(func() { container.Terminate(ctx) })

    return container
}
```

---

## Golden File Testing

Golden files store expected output and automatically update when you pass `-update`. This is ideal for testing complex text output, serialization, or code generation.

```go
// golden_test.go
package render

import (
    "flag"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update golden files")

func TestRenderTemplate(t *testing.T) {
    tests := []struct {
        name string
        data TemplateData
    }{
        {
            name: "simple_user",
            data: TemplateData{Name: "Alice", Items: []string{"a", "b"}},
        },
        {
            name: "empty_items",
            data: TemplateData{Name: "Bob", Items: nil},
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := RenderTemplate(tt.data)
            require.NoError(t, err)

            goldenPath := filepath.Join("testdata", tt.name+".golden")

            if *update {
                err := os.MkdirAll("testdata", 0o755)
                require.NoError(t, err)
                err = os.WriteFile(goldenPath, []byte(got), 0o644)
                require.NoError(t, err)
            }

            want, err := os.ReadFile(goldenPath)
            require.NoError(t, err)

            assert.Equal(t, string(want), got)
        })
    }
}
```

Update golden files:

```bash
go test -run TestRenderTemplate -update ./render/...
```

Store test data files in `testdata/` directories. Go tooling ignores `testdata/` by convention.

---

## Benchmark Tests

Benchmark functions start with `Benchmark` and use `*testing.B`. The `b.N` loop runs enough iterations for stable measurements.

```go
// sort_bench_test.go
package sort

import (
    "math/rand"
    "testing"
)

func BenchmarkBubbleSort(b *testing.B) {
    for b.Loop() {
        data := generateRandomSlice(1000)
        BubbleSort(data)
    }
}

func BenchmarkQuickSort(b *testing.B) {
    for b.Loop() {
        data := generateRandomSlice(1000)
        QuickSort(data)
    }
}

// Sub-benchmarks for different input sizes
func BenchmarkSort(b *testing.B) {
    sizes := []int{100, 1_000, 10_000}

    for _, size := range sizes {
        b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
            for b.Loop() {
                data := generateRandomSlice(size)
                QuickSort(data)
            }
        })
    }
}

// Benchmark with memory allocation tracking
func BenchmarkConcat(b *testing.B) {
    b.ReportAllocs()
    for b.Loop() {
        var s string
        for i := 0; i < 100; i++ {
            s += "x"
        }
    }
}

func generateRandomSlice(n int) []int {
    s := make([]int, n)
    for i := range s {
        s[i] = rand.Intn(n * 10)
    }
    return s
}
```

Note: Go 1.24+ introduces `b.Loop()` which replaces the traditional `for i := 0; i < b.N; i++` pattern. The `b.Loop()` method automatically handles iteration counting and prevents the compiler from optimizing away benchmark code.

Run benchmarks:

```bash
go test -bench=. ./sort/...                  # run all benchmarks
go test -bench=BenchmarkQuickSort ./sort/... # specific benchmark
go test -bench=. -benchmem ./sort/...        # include memory stats
go test -bench=. -count=5 ./sort/...         # multiple runs for stability
go test -bench=. -benchtime=5s ./sort/...    # longer benchmark duration
go test -bench=. -cpuprofile=cpu.out ./sort  # CPU profiling
go test -bench=. -memprofile=mem.out ./sort  # memory profiling
```

Compare benchmarks with `benchstat`:

```bash
go install golang.org/x/perf/cmd/benchstat@latest
go test -bench=. -count=10 ./sort/... > old.txt
# make changes
go test -bench=. -count=10 ./sort/... > new.txt
benchstat old.txt new.txt
```

---

## Fuzz Testing

Fuzz testing (Go 1.18+) generates random inputs to discover edge cases. Fuzz functions start with `Fuzz` and use `*testing.F`.

```go
// parser_fuzz_test.go
package parser

import (
    "testing"
    "unicode/utf8"
)

func FuzzParseJSON(f *testing.F) {
    // Seed corpus — these guide the fuzzer
    f.Add(`{"name":"Alice","age":30}`)
    f.Add(`{"items":[1,2,3]}`)
    f.Add(`{}`)
    f.Add(`{"nested":{"key":"val"}}`)
    f.Add(`invalid json`)

    f.Fuzz(func(t *testing.T, input string) {
        if !utf8.ValidString(input) {
            t.Skip("skipping invalid UTF-8")
        }

        result, err := ParseJSON(input)
        if err != nil {
            // Parser returned an error: this is fine
            return
        }

        // If parsing succeeded, verify round-trip
        output, err := result.Marshal()
        if err != nil {
            t.Fatalf("failed to marshal parsed result: %v", err)
        }

        reparsed, err := ParseJSON(output)
        if err != nil {
            t.Fatalf("round-trip failed: parsed successfully but re-parse failed: %v", err)
        }

        if !result.Equal(reparsed) {
            t.Errorf("round-trip mismatch:\n  original: %s\n  reparsed: %s", output, reparsed)
        }
    })
}
```

Run fuzz tests:

```bash
go test -fuzz=FuzzParseJSON ./parser/...              # fuzz until stopped
go test -fuzz=FuzzParseJSON -fuzztime=30s ./parser/... # fuzz for 30 seconds
go test -fuzz=FuzzParseJSON -fuzztime=1000x ./parser/  # fuzz for 1000 iterations
go test ./parser/...                                    # run seed corpus as regular tests
```

Failing inputs are saved to `testdata/fuzz/<FuncName>/` and automatically replayed in future test runs.

---

## Test Coverage

```bash
# Basic coverage percentage
go test -cover ./...

# Generate coverage profile
go test -coverprofile=coverage.out ./...

# View in browser as annotated HTML
go tool cover -html=coverage.out -o coverage.html

# Coverage by function
go tool cover -func=coverage.out

# Coverage for specific packages
go test -coverpkg=./internal/... -coverprofile=coverage.out ./...

# Combine coverage from integration + unit
go test -coverprofile=unit.out ./...
go test -tags=integration -coverprofile=int.out ./...
```

Set coverage thresholds in CI:

```bash
# Fail if coverage drops below 80%
COVERAGE=$(go test -cover ./... | grep -oP '\d+\.\d+(?=%)' | awk '{sum+=$1; n++} END {print sum/n}')
if (( $(echo "$COVERAGE < 80" | bc -l) )); then
    echo "Coverage ${COVERAGE}% is below 80% threshold"
    exit 1
fi
```

---

## Test Fixtures and Helpers

Use `testdata/` directories for fixture files and `t.Helper()` for shared setup logic.

```go
// helpers_test.go
package invoice

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/require"
)

// loadFixture reads a JSON fixture file from testdata/
func loadFixture[T any](t *testing.T, name string) T {
    t.Helper()

    data, err := os.ReadFile(filepath.Join("testdata", name))
    require.NoError(t, err)

    var result T
    err = json.Unmarshal(data, &result)
    require.NoError(t, err)
    return result
}

// newTestInvoice creates an invoice with sensible defaults.
// Override fields after creation for specific test scenarios.
func newTestInvoice(t *testing.T) *Invoice {
    t.Helper()
    return &Invoice{
        Number:   "INV-001",
        Customer: "Acme Corp",
        Items: []LineItem{
            {Description: "Widget", Qty: 10, UnitPrice: 9.99},
        },
    }
}

func TestInvoiceTotal(t *testing.T) {
    inv := newTestInvoice(t)
    assert.InDelta(t, 99.90, inv.Total(), 0.01)
}

func TestInvoiceFromFixture(t *testing.T) {
    inv := loadFixture[Invoice](t, "invoice_basic.json")
    assert.Equal(t, "INV-042", inv.Number)
    assert.Len(t, inv.Items, 3)
}
```

Use `t.TempDir()` for tests that need temporary filesystem state:

```go
func TestWriteReport(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "report.csv")

    err := WriteReport(path, reportData)
    require.NoError(t, err)

    content, err := os.ReadFile(path)
    require.NoError(t, err)
    assert.Contains(t, string(content), "Total,150.00")
}
```

---

## Testing with Context and Timeouts

Test cancellation, deadlines, and context propagation to verify graceful handling.

```go
// worker_test.go
package worker

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestWorker_RespectsContextCancellation(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    w := NewWorker()

    // Start worker in background
    errCh := make(chan error, 1)
    go func() {
        errCh <- w.Run(ctx)
    }()

    // Cancel after brief period
    time.Sleep(50 * time.Millisecond)
    cancel()

    select {
    case err := <-errCh:
        assert.ErrorIs(t, err, context.Canceled)
    case <-time.After(2 * time.Second):
        t.Fatal("worker did not stop after context cancellation")
    }
}

func TestFetchData_Timeout(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    _, err := FetchData(ctx, "https://httpbin.org/delay/10")
    require.Error(t, err)
    assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestService_PropagatesContext(t *testing.T) {
    ctx := context.WithValue(context.Background(), traceIDKey, "abc-123")

    spy := &SpyStore{}
    svc := NewService(spy)

    _ = svc.ProcessOrder(ctx, Order{ID: 1})

    // Verify the store received a context with the trace ID
    require.NotNil(t, spy.LastCtx)
    assert.Equal(t, "abc-123", spy.LastCtx.Value(traceIDKey))
}
```

---

## Parallel Tests with t.Parallel()

Call `t.Parallel()` to run subtests concurrently. This finds race conditions and speeds up I/O-bound tests.

```go
// parallel_test.go
package cache

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestCache(t *testing.T) {
    t.Parallel() // top-level test runs in parallel with other top-level tests

    tests := []struct {
        name  string
        key   string
        value string
    }{
        {name: "simple key", key: "foo", value: "bar"},
        {name: "empty value", key: "empty", value: ""},
        {name: "unicode key", key: "klucz", value: "wartosc"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel() // subtests also run in parallel

            c := NewCache() // each subtest gets its own cache
            c.Set(tt.key, tt.value)

            got, ok := c.Get(tt.key)
            assert.True(t, ok)
            assert.Equal(t, tt.value, got)
        })
    }
}
```

Run tests with the race detector to catch data races:

```bash
go test -race -parallel=8 ./...
```

Important considerations for `t.Parallel()`:
- Each parallel subtest must use its own copy of test data (the loop variable is safe in Go 1.22+ due to the loop variable change)
- Shared resources (databases, files) need synchronization or isolation
- `t.Parallel()` subtests run after the parent test function returns
- The `-parallel` flag controls the maximum number of parallel tests (default: GOMAXPROCS)

---

## TestMain for Setup and Teardown

`TestMain` lets you run global setup before any tests and teardown after all tests in a package.

```go
// main_test.go
package integration

import (
    "database/sql"
    "fmt"
    "log"
    "os"
    "testing"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
    // Global setup
    var err error
    dsn := os.Getenv("TEST_DATABASE_URL")
    if dsn == "" {
        dsn = "postgres://test:test@localhost:5432/testdb?sslmode=disable"
    }

    testDB, err = sql.Open("postgres", dsn)
    if err != nil {
        log.Fatalf("failed to connect to test database: %v", err)
    }

    if err := runMigrations(testDB); err != nil {
        log.Fatalf("failed to run migrations: %v", err)
    }

    // Run all tests
    code := m.Run()

    // Global teardown
    if err := testDB.Close(); err != nil {
        fmt.Fprintf(os.Stderr, "error closing db: %v\n", err)
    }

    os.Exit(code)
}

func TestUserCRUD(t *testing.T) {
    // testDB is available here
    repo := NewUserRepo(testDB)
    // ...
}
```

A common pattern is using `TestMain` for Docker container lifecycle:

```go
func TestMain(m *testing.M) {
    pool, err := dockertest.NewPool("")
    if err != nil {
        log.Fatalf("could not construct pool: %v", err)
    }

    resource, err := pool.Run("postgres", "16", []string{
        "POSTGRES_PASSWORD=secret",
        "POSTGRES_DB=testdb",
    })
    if err != nil {
        log.Fatalf("could not start resource: %v", err)
    }

    // Wait for container
    if err := pool.Retry(func() error {
        var err error
        testDB, err = sql.Open("postgres",
            fmt.Sprintf("postgres://postgres:secret@localhost:%s/testdb?sslmode=disable",
                resource.GetPort("5432/tcp")))
        if err != nil {
            return err
        }
        return testDB.Ping()
    }); err != nil {
        log.Fatalf("could not connect to database: %v", err)
    }

    code := m.Run()

    pool.Purge(resource)
    os.Exit(code)
}
```

---

## Testing Private Functions

In Go, test files in the same package can access unexported (private) identifiers directly. This is the standard approach for unit testing internal logic.

```go
// parser.go
package parser

func tokenize(input string) []token {
    // unexported function
    // ...
}

type token struct {
    kind  tokenKind
    value string
}
```

```go
// parser_test.go
package parser

import "testing"

func TestTokenize(t *testing.T) {
    // Can access unexported tokenize() because we are in package parser
    tokens := tokenize("hello world")

    if len(tokens) != 2 {
        t.Fatalf("expected 2 tokens, got %d", len(tokens))
    }
    if tokens[0].value != "hello" {
        t.Errorf("first token = %q, want %q", tokens[0].value, "hello")
    }
}
```

For black-box testing of the public API from an external perspective, use `_test` suffix on the package name:

```go
// parser_external_test.go
package parser_test

import (
    "testing"

    "mymodule/parser"
)

func TestParse(t *testing.T) {
    // Can only access exported identifiers
    result, err := parser.Parse("hello world")
    // ...
}
```

Use `export_test.go` to selectively expose internals for external tests:

```go
// export_test.go
package parser

// Tokenize exports the private tokenize function for external tests only.
// This file is only compiled during testing.
var Tokenize = tokenize
```

---

## Filesystem Testing with testing/fstest

The `testing/fstest` package provides an in-memory filesystem (`MapFS`) that implements `fs.FS`. Use it to test code that reads from filesystems without touching real files.

```go
// config_test.go
package config

import (
    "testing"
    "testing/fstest"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
    fsys := fstest.MapFS{
        "config.yaml": &fstest.MapFile{
            Data: []byte(`
database:
  host: localhost
  port: 5432
  name: myapp
`),
        },
        "config.d/overrides.yaml": &fstest.MapFile{
            Data: []byte(`
database:
  port: 5433
`),
        },
    }

    cfg, err := LoadConfig(fsys, "config.yaml")
    require.NoError(t, err)
    assert.Equal(t, "localhost", cfg.Database.Host)
    assert.Equal(t, 5432, cfg.Database.Port)
}

func TestLoadTemplates(t *testing.T) {
    fsys := fstest.MapFS{
        "templates/index.html":  {Data: []byte(`<h1>{{.Title}}</h1>`)},
        "templates/layout.html": {Data: []byte(`<html>{{.Body}}</html>`)},
    }

    templates, err := LoadTemplates(fsys, "templates")
    require.NoError(t, err)
    assert.Len(t, templates, 2)
}
```

Validate that a filesystem implementation conforms to the `fs.FS` contract:

```go
func TestCustomFS(t *testing.T) {
    myFS := NewCustomFS("/some/root")

    err := fstest.TestFS(myFS, "expected/file.txt", "expected/dir/other.txt")
    if err != nil {
        t.Fatal(err)
    }
}
```

---

## Structured Log Testing with testing/slogtest

The `testing/slogtest` package (Go 1.22+) verifies that custom `slog.Handler` implementations produce correct structured log output.

```go
// handler_test.go
package logging

import (
    "bytes"
    "encoding/json"
    "log/slog"
    "testing"
    "testing/slogtest"
)

func TestJSONHandler(t *testing.T) {
    var buf bytes.Buffer

    results := func(t *testing.T) map[string]any {
        line, _, _ := bytes.Cut(buf.Bytes(), []byte("\n"))
        if len(line) == 0 {
            return nil
        }

        var m map[string]any
        if err := json.Unmarshal(line, &m); err != nil {
            t.Fatal(err)
        }
        buf.Reset()
        return m
    }

    handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })

    err := slogtest.Run(t, func(t *testing.T) slog.Handler {
        buf.Reset()
        return handler
    }, results)
    if err != nil {
        t.Fatal(err)
    }
}
```

For custom handlers, `slogtest` validates:
- All standard fields (time, level, msg) are present
- Attribute groups are handled correctly
- `WithAttrs` and `WithGroup` work as expected
- Source information is recorded when enabled

---

## Best Practices

1. **Use table-driven tests** for any function with multiple input/output scenarios. Subtests via `t.Run()` provide clear failure messages and allow running individual cases.

2. **Prefer `require` for preconditions, `assert` for verifications.** If a nil check fails with `assert`, subsequent field access will panic. Use `require.NotNil` or `require.NoError` for guards.

3. **Always call `t.Helper()`** in test helper functions so failures report the actual test line, not the helper internals.

4. **Use `t.Cleanup()` over `defer`** when setup functions create resources. Cleanup functions run after the test and all its subtests complete.

5. **Run tests with `-race`** routinely. Data races cause flaky tests and production bugs. Add `-race` to your CI pipeline.

6. **Keep unit tests fast.** Guard slow integration tests with `if testing.Short() { t.Skip(...) }` so developers can run `go test -short` for rapid feedback.

7. **Put test fixtures in `testdata/` directories.** The Go toolchain ignores these directories during builds. Use golden files for complex output validation.

8. **Design for testability.** Accept interfaces, return structs. Inject dependencies (database connections, HTTP clients, clocks) instead of using package-level globals.

9. **Use `t.Parallel()`** for independent tests to find concurrency bugs and speed up test suites. Ensure each parallel test has its own state.

10. **Test error paths, not just happy paths.** Verify that functions return correct error types, wrap errors properly, and handle edge cases like empty input, nil pointers, and context cancellation.

11. **Use build tags for integration tests** when `testing.Short()` is insufficient:
    ```go
    //go:build integration
    package repo
    ```
    Run with: `go test -tags=integration ./...`

12. **Benchmark before optimizing.** Use `b.ReportAllocs()` and `benchstat` to make data-driven performance decisions.

---

## Anti-Patterns

- **Testing implementation details instead of behavior.** Tests that break on every refactor provide little value. Test the public interface.

- **Sharing mutable state between tests.** Each test should set up its own data. Shared state causes ordering dependencies and flaky tests.

- **Ignoring test errors with `_ =`** instead of asserting on them. Every error return should be checked in tests.

- **Overusing gomock for everything.** Manual stubs and fakes are often clearer and easier to maintain for simple interfaces. Reserve gomock for interfaces with many methods.

- **Not running `go test -race` in CI.** The race detector catches real bugs. The small performance cost is worth it.

- **Using `time.Sleep()` in tests** instead of channels, condition variables, or `require.Eventually`. Sleep-based tests are slow and flaky.

- **Testing unexported functions excessively.** If you need extensive tests for private functions, consider whether they should be extracted into their own package with a public API.

- **Huge test functions without subtests.** A 200-line test function is hard to debug. Use `t.Run()` to break it into named subtests.

- **Not using `t.TempDir()` or `t.Cleanup()`.** Manual temp directory creation and cleanup is error-prone. Let the testing framework handle it.

- **Skipping fuzz testing entirely.** Even a short fuzz run (`-fuzztime=10s`) often finds edge cases that unit tests miss, especially in parsers, validators, and serialization code.

---

## Sources & References

- [Go Testing Package Documentation](https://pkg.go.dev/testing)
- [Go Wiki: Table-Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [testify - Toolkit with assertions and mocking](https://github.com/stretchr/testify)
- [gomock - Mock framework for Go (uber fork)](https://github.com/uber-go/mock)
- [testcontainers-go - Integration tests with real containers](https://golang.testcontainers.org/)
- [Go Blog: Fuzz Testing](https://go.dev/doc/fuzz/)
- [Go Blog: Using Subtests and Sub-benchmarks](https://go.dev/blog/subtests)
- [testing/fstest Package Documentation](https://pkg.go.dev/testing/fstest)
- [testing/slogtest Package Documentation](https://pkg.go.dev/testing/slogtest)
- [Go Code Review Comments - Test Conventions](https://go.dev/wiki/CodeReviewComments)
