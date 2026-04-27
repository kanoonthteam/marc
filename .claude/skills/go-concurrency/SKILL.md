---
name: go-concurrency
description: Go Concurrency Patterns — goroutines, channels, select, context.Context, sync primitives, errgroup, fan-out/fan-in, pipelines, worker pools, rate limiting, graceful shutdown, race detection, and atomic operations for Go 1.22+
---

# Go Concurrency Patterns

Production-ready concurrency patterns for Go 1.22+. Covers goroutine lifecycle management, buffered and unbuffered channels, the select statement, context.Context for cancellation/timeout/deadline propagation, sync.WaitGroup, sync.Mutex and sync.RWMutex, sync.Once, sync.Map, sync.Pool, the errgroup package, semaphore patterns, fan-out/fan-in, pipeline construction, worker pools, rate limiting with time.Ticker, graceful shutdown with OS signal handling, race condition detection via `go test -race`, atomic operations with sync/atomic, and range-over-func iterators introduced in Go 1.22.

## Table of Contents

1. [Goroutines and Lifecycle Management](#1-goroutines-and-lifecycle-management)
2. [Channels: Buffered and Unbuffered](#2-channels-buffered-and-unbuffered)
3. [Select Statement](#3-select-statement)
4. [Context for Cancellation, Timeout, and Deadline](#4-context-for-cancellation-timeout-and-deadline)
5. [sync.WaitGroup](#5-syncwaitgroup)
6. [sync.Mutex and sync.RWMutex](#6-syncmutex-and-syncrwmutex)
7. [sync.Once, sync.Map, and sync.Pool](#7-synconce-syncmap-and-syncpool)
8. [errgroup for Structured Concurrency](#8-errgroup-for-structured-concurrency)
9. [Semaphore Pattern](#9-semaphore-pattern)
10. [Fan-Out / Fan-In Pattern](#10-fan-out--fan-in-pattern)
11. [Pipeline Pattern](#11-pipeline-pattern)
12. [Worker Pool Pattern](#12-worker-pool-pattern)
13. [Rate Limiting with time.Ticker](#13-rate-limiting-with-timeticker)
14. [Graceful Shutdown with Signal Handling](#14-graceful-shutdown-with-signal-handling)
15. [Race Condition Detection and Atomic Operations](#15-race-condition-detection-and-atomic-operations)
16. [Range-Over-Func Iterators (Go 1.22+)](#16-range-over-func-iterators-go-122)
17. [Best Practices](#17-best-practices)
18. [Anti-Patterns](#18-anti-patterns)
19. [Sources & References](#19-sources--references)

---

## 1. Goroutines and Lifecycle Management

Goroutines are lightweight threads managed by the Go runtime. They cost roughly 2-8 KB of stack space initially and grow as needed. The runtime multiplexes goroutines onto OS threads using an M:N scheduling model.

- Launch a goroutine with the `go` keyword followed by a function call.
- A goroutine runs independently; the launching goroutine does not block.
- The program exits when `main` returns, regardless of whether other goroutines are still running.
- Always ensure goroutines have a clear termination path. Leaked goroutines consume memory and CPU indefinitely.

Key rules for goroutine lifecycle:

- **Ownership**: The code that starts a goroutine is responsible for ensuring it can stop.
- **Signaling**: Use channels or `context.Context` to signal goroutines to exit.
- **Joining**: Use `sync.WaitGroup` or `errgroup.Group` to wait for goroutines to finish.

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// poller runs until the context is cancelled, demonstrating
// goroutine lifecycle tied to context.
func poller(ctx context.Context, wg *sync.WaitGroup, id int) {
	defer wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("poller %d: shutting down: %v\n", id, ctx.Err())
			return
		case t := <-ticker.C:
			fmt.Printf("poller %d: tick at %v\n", id, t.Format(time.TimeOnly))
		}
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := range 3 { // Go 1.22+ range-over-int
		wg.Add(1)
		go poller(ctx, &wg, i)
	}
	wg.Wait()
	fmt.Println("all pollers stopped")
}
```

## 2. Channels: Buffered and Unbuffered

Channels are typed conduits for communication between goroutines. They enforce synchronization by design.

**Unbuffered channels** (`make(chan T)`) block the sender until a receiver is ready, and vice versa. They provide a strong synchronization guarantee: every send happens-before the corresponding receive completes.

**Buffered channels** (`make(chan T, cap)`) allow sends to proceed without blocking until the buffer is full. They decouple producers from consumers but do not eliminate the need for coordination.

Channel axioms:

| Operation | nil channel | closed channel | open channel |
|-----------|------------|----------------|--------------|
| Send | blocks forever | **panic** | blocks or succeeds |
| Receive | blocks forever | returns zero value, `ok=false` | blocks or succeeds |
| Close | **panic** | **panic** | succeeds |

Guidelines:

- Only the **sender** should close a channel, never the receiver.
- Closing is not required unless the receiver needs to detect completion (e.g., `range` over a channel).
- Use directional channel types (`chan<-` for send-only, `<-chan` for receive-only) in function signatures to communicate intent and catch misuse at compile time.
- Prefer unbuffered channels as the default; add buffering only when you have a measured performance need or a design reason (e.g., batching).

```go
package main

import "fmt"

// generator returns a receive-only channel that yields values.
func generator(nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, n := range nums {
			out <- n
		}
	}()
	return out
}

// squarer reads from in, squares each value, writes to a buffered output channel.
func squarer(in <-chan int) <-chan int {
	out := make(chan int, 4) // buffered: up to 4 items can queue
	go func() {
		defer close(out)
		for n := range in {
			out <- n * n
		}
	}()
	return out
}

func main() {
	nums := generator(2, 3, 4, 5)
	squares := squarer(nums)
	for sq := range squares {
		fmt.Println(sq) // 4, 9, 16, 25
	}
}
```

## 3. Select Statement

The `select` statement lets a goroutine wait on multiple channel operations simultaneously. It blocks until one of its cases can proceed. If multiple cases are ready, one is chosen at random (uniform pseudo-random selection).

Key patterns:

- **Timeout**: combine with `time.After` or a context deadline.
- **Non-blocking send/receive**: add a `default` case. The `default` case fires immediately if no other case is ready.
- **Priority**: Go does not support priority in `select`; use nested selects or pre-check the high-priority channel before entering the main select.
- **Nil channel trick**: assigning a channel variable to `nil` disables that case in a `select`, which is useful for dynamically enabling/disabling branches.

```go
package main

import (
	"context"
	"fmt"
	"time"
)

func merge(ctx context.Context, ch1, ch2 <-chan string) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		c1, c2 := ch1, ch2
		for c1 != nil || c2 != nil {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-c1:
				if !ok {
					c1 = nil // disable this case
					continue
				}
				out <- v
			case v, ok := <-c2:
				if !ok {
					c2 = nil
					continue
				}
				out <- v
			}
		}
	}()
	return out
}

func produce(ctx context.Context, name string, interval time.Duration, count int) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		for i := range count {
			select {
			case <-ctx.Done():
				return
			case ch <- fmt.Sprintf("%s-%d", name, i):
				time.Sleep(interval)
			}
		}
	}()
	return ch
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch1 := produce(ctx, "alpha", 200*time.Millisecond, 10)
	ch2 := produce(ctx, "beta", 350*time.Millisecond, 10)

	for v := range merge(ctx, ch1, ch2) {
		fmt.Println(v)
	}
}
```

## 4. Context for Cancellation, Timeout, and Deadline

`context.Context` carries deadlines, cancellation signals, and request-scoped values across API boundaries and between goroutines. It is the standard mechanism for controlling goroutine lifetime in Go.

Context hierarchy:

- `context.Background()` -- top-level context, never cancelled. Used in `main`, init, and tests.
- `context.TODO()` -- placeholder when you are unsure which context to use.
- `context.WithCancel(parent)` -- returns a derived context and a `cancel` function. Calling `cancel()` cancels the child and all its descendants.
- `context.WithTimeout(parent, duration)` -- cancels automatically after the duration elapses.
- `context.WithDeadline(parent, time)` -- cancels at a specific wall-clock time.
- `context.WithValue(parent, key, val)` -- attaches a value (use sparingly; prefer explicit parameters).
- `context.WithoutCancel(parent)` (Go 1.21+) -- derives a context that is not cancelled when the parent is, but still propagates values.
- `context.AfterFunc(ctx, f)` (Go 1.21+) -- registers a function to run asynchronously when the context is done.

Rules:

- Always pass `ctx` as the first parameter of a function.
- Never store a context in a struct; pass it explicitly.
- Always call the `cancel` function returned by `WithCancel`/`WithTimeout`/`WithDeadline`, typically via `defer`.
- Check `ctx.Err()` to distinguish between `context.Canceled` and `context.DeadlineExceeded`.

## 5. sync.WaitGroup

`sync.WaitGroup` waits for a collection of goroutines to finish. It maintains an internal counter:

- `Add(delta)` increments (or decrements) the counter.
- `Done()` decrements by 1 (equivalent to `Add(-1)`).
- `Wait()` blocks until the counter reaches zero.

Rules:

- Call `Add` **before** launching the goroutine, not inside it. Otherwise a race exists between `Wait` and the goroutine calling `Add`.
- A `WaitGroup` must not be copied after first use. Pass it by pointer.
- Do not call `Add` with a negative value that would make the counter go below zero; this panics.

Common pattern:

```go
var wg sync.WaitGroup
for i := range 10 {
    wg.Add(1)
    go func() {
        defer wg.Done()
        process(i)
    }()
}
wg.Wait()
```

Note: In Go 1.22+, the loop variable `i` is per-iteration, so capturing it in the closure is safe without an explicit copy. This was a significant change from pre-1.22 behavior where the loop variable was shared across iterations.

## 6. sync.Mutex and sync.RWMutex

`sync.Mutex` provides mutual exclusion. Only one goroutine can hold the lock at a time. `sync.RWMutex` distinguishes between readers and writers: multiple readers can hold the lock concurrently, but a writer requires exclusive access.

Guidelines:

- Keep the critical section as small as possible.
- Always unlock with `defer mu.Unlock()` immediately after locking, unless you have a compelling reason not to (e.g., performance in a tight loop where you have proven the defer overhead matters via benchmarks).
- Never copy a Mutex or RWMutex after first use.
- Be careful with `RWMutex`: a pending writer blocks new readers, which prevents reader starvation of writers but can cause latency spikes for readers.
- Prefer channels for communication between goroutines; use mutexes for protecting shared state that is accessed by multiple goroutines.

Embedding pattern for clean APIs:

```go
type SafeCounter struct {
	mu sync.RWMutex
	v  map[string]int
}

func NewSafeCounter() *SafeCounter {
	return &SafeCounter{v: make(map[string]int)}
}

func (c *SafeCounter) Inc(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.v[key]++
}

func (c *SafeCounter) Value(key string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.v[key]
}

func (c *SafeCounter) Snapshot() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make(map[string]int, len(c.v))
	for k, v := range c.v {
		cp[k] = v
	}
	return cp
}
```

## 7. sync.Once, sync.Map, and sync.Pool

### sync.Once

`sync.Once` ensures a function is executed exactly once, regardless of how many goroutines call it. It is typically used for lazy initialization.

- `once.Do(f)` calls `f` the first time. Subsequent calls return immediately.
- If `f` panics, `Do` considers the function executed; subsequent calls will not retry. Use `sync.OnceFunc` (Go 1.21+) if you want panic-safe semantics, or `sync.OnceValue[T]` / `sync.OnceValues[T1, T2]` for returning values.

```go
var (
	dbOnce sync.Once
	dbConn *sql.DB
)

func GetDB() *sql.DB {
	dbOnce.Do(func() {
		var err error
		dbConn, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
		if err != nil {
			log.Fatal(err)
		}
	})
	return dbConn
}

// Go 1.21+ alternative with sync.OnceValue:
var getDB = sync.OnceValue(func() *sql.DB {
	db, err := sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	return db
})
```

### sync.Map

`sync.Map` is a concurrent map optimized for two use cases: (1) when the entry for a given key is only ever written once but read many times, and (2) when multiple goroutines read, write, and overwrite entries for disjoint sets of keys. For other scenarios a plain `map` protected by `sync.RWMutex` typically performs better.

Methods: `Store`, `Load`, `LoadOrStore`, `LoadAndDelete`, `Delete`, `Range`, `Swap` (Go 1.20+), `CompareAndSwap` (Go 1.20+), `CompareAndDelete` (Go 1.20+).

Limitations:

- Not type-safe (uses `any` for keys and values). Consider wrapping it or using generics.
- No `Len` method. You must count via `Range` if you need the size.
- Cannot be used with the `range` keyword directly; use the `Range` method.

### sync.Pool

`sync.Pool` is a set of temporary objects that may be reused to reduce allocation pressure. Objects in the pool may be removed at any time without notice (the GC can clear the pool between GC cycles).

- `pool.Get()` retrieves an item, removing it from the pool.
- `pool.Put(x)` returns an item to the pool.
- Set the `New` field to a function that allocates a fresh object when the pool is empty.

Common use: buffer pools for encoding/decoding, byte slices for I/O.

```go
var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func process(data []byte) string {
	buf := bufPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		bufPool.Put(buf)
	}()

	// Use buf for temporary work
	buf.Write(data)
	return buf.String()
}
```

## 8. errgroup for Structured Concurrency

`golang.org/x/sync/errgroup` provides synchronization, error propagation, and context cancellation for groups of goroutines working on subtasks of a common task.

- `errgroup.Group` combines a `WaitGroup` with error collection.
- `g.Go(func() error)` launches a goroutine. If any goroutine returns a non-nil error, `g.Wait()` returns that error.
- `errgroup.WithContext(ctx)` returns a group and a derived context that is cancelled when any goroutine returns an error.
- `g.SetLimit(n)` (Go 1.20+) limits the number of goroutines that can run concurrently, providing built-in concurrency control without a separate semaphore.
- `g.TryGo(func() error)` (Go 1.20+) attempts to launch a goroutine but returns `false` if the concurrency limit is reached instead of blocking.

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"golang.org/x/sync/errgroup"
)

func fetchAll(ctx context.Context, urls []string) ([]int, error) {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(5) // max 5 concurrent fetches

	results := make([]int, len(urls))

	for i, url := range urls {
		g.Go(func() error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return fmt.Errorf("creating request for %s: %w", url, err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("fetching %s: %w", url, err)
			}
			defer resp.Body.Close()
			results[i] = resp.StatusCode // safe: each goroutine writes to its own index
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func main() {
	urls := []string{
		"https://go.dev",
		"https://pkg.go.dev",
		"https://play.golang.org",
	}
	codes, err := fetchAll(context.Background(), urls)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	for i, code := range codes {
		fmt.Printf("%s -> %d\n", urls[i], code)
	}
}
```

Note: In Go 1.22+, loop variables `i` and `url` are per-iteration, so the closures above capture the correct values without needing explicit copies.

## 9. Semaphore Pattern

A semaphore limits the number of goroutines that can access a resource concurrently. In Go, this is typically implemented with a buffered channel or with `golang.org/x/sync/semaphore`.

**Buffered channel semaphore:**

```go
sem := make(chan struct{}, maxConcurrency)

for _, task := range tasks {
    sem <- struct{}{} // acquire
    go func() {
        defer func() { <-sem }() // release
        process(task)
    }()
}
// Drain the semaphore to wait for all goroutines
for range maxConcurrency {
    sem <- struct{}{}
}
```

**`semaphore.Weighted` from x/sync:**

```go
import "golang.org/x/sync/semaphore"

sem := semaphore.NewWeighted(int64(maxConcurrency))

for _, task := range tasks {
    if err := sem.Acquire(ctx, 1); err != nil {
        return err // context cancelled
    }
    go func() {
        defer sem.Release(1)
        process(task)
    }()
}
// Wait for all in-flight goroutines
if err := sem.Acquire(ctx, int64(maxConcurrency)); err != nil {
    return err
}
```

The `semaphore.Weighted` variant supports context cancellation and weighted acquisition (e.g., acquire 3 slots for a heavy task), making it more flexible than the channel approach.

## 10. Fan-Out / Fan-In Pattern

**Fan-out** means starting multiple goroutines to handle input from the same channel. **Fan-in** means multiplexing multiple input channels onto a single output channel.

This pattern is useful when a stage in a pipeline is CPU-bound or I/O-bound and benefits from parallelism.

Fan-in implementation:

```go
func fanIn[T any](ctx context.Context, channels ...<-chan T) <-chan T {
	var wg sync.WaitGroup
	merged := make(chan T)

	output := func(ch <-chan T) {
		defer wg.Done()
		for v := range ch {
			select {
			case <-ctx.Done():
				return
			case merged <- v:
			}
		}
	}

	wg.Add(len(channels))
	for _, ch := range channels {
		go output(ch)
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}
```

Fan-out usage:

```go
// Fan-out: start N workers reading from the same source channel
source := generateWork(ctx)
workers := make([]<-chan Result, numWorkers)
for i := range numWorkers {
    workers[i] = processWork(ctx, source) // each reads from source
}
// Fan-in: merge all worker outputs
results := fanIn(ctx, workers...)
```

## 11. Pipeline Pattern

A pipeline is a series of stages connected by channels, where each stage is a group of goroutines running the same function. Each stage:

1. Receives values from upstream via an inbound channel.
2. Performs some computation on that data.
3. Sends values downstream via an outbound channel.

Pipeline stages should accept a context for cancellation and close their output channels when done so downstream stages can detect completion via `range`.

Design considerations:

- Each stage owns its output channel and is responsible for closing it.
- Propagate context cancellation through all stages.
- Consider using errgroup when stages can fail.
- Use buffered channels between stages to smooth out throughput differences.

Example of a three-stage pipeline (generate, transform, collect):

```go
func generate(ctx context.Context, nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, n := range nums {
			select {
			case <-ctx.Done():
				return
			case out <- n:
			}
		}
	}()
	return out
}

func transform(ctx context.Context, in <-chan int, fn func(int) int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for n := range in {
			select {
			case <-ctx.Done():
				return
			case out <- fn(n):
			}
		}
	}()
	return out
}

func collect(ctx context.Context, in <-chan int) []int {
	var results []int
	for n := range in {
		results = append(results, n)
	}
	return results
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nums := generate(ctx, 1, 2, 3, 4, 5)
	doubled := transform(ctx, nums, func(n int) int { return n * 2 })
	squared := transform(ctx, doubled, func(n int) int { return n * n })
	results := collect(ctx, squared)
	fmt.Println(results) // [4 16 36 64 100]
}
```

## 12. Worker Pool Pattern

A worker pool is a fixed set of goroutines that pull tasks from a shared channel. This bounds concurrency and provides backpressure when the task channel is full.

```go
package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Job struct {
	ID      int
	Payload string
}

type Result struct {
	JobID int
	Value string
	Err   error
}

func worker(ctx context.Context, id int, jobs <-chan Job, results chan<- Result) {
	for job := range jobs {
		select {
		case <-ctx.Done():
			results <- Result{JobID: job.ID, Err: ctx.Err()}
			return
		default:
		}

		// Simulate work
		time.Sleep(50 * time.Millisecond)
		results <- Result{
			JobID: job.ID,
			Value: fmt.Sprintf("worker-%d processed %q", id, job.Payload),
		}
	}
}

func runPool(ctx context.Context, numWorkers int, jobs []Job) []Result {
	jobCh := make(chan Job, len(jobs))
	resultCh := make(chan Result, len(jobs))

	// Start workers
	var wg sync.WaitGroup
	for i := range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, i, jobCh, resultCh)
		}()
	}

	// Send jobs
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	// Wait for workers and close results
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results
	var results []Result
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}

func main() {
	ctx := context.Background()
	jobs := make([]Job, 20)
	for i := range jobs {
		jobs[i] = Job{ID: i, Payload: fmt.Sprintf("task-%d", i)}
	}

	results := runPool(ctx, 4, jobs)
	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("job %d error: %v\n", r.JobID, r.Err)
		} else {
			fmt.Printf("job %d: %s\n", r.JobID, r.Value)
		}
	}
}
```

For dynamic worker pools where the number of workers can scale up and down, consider `errgroup.Group` with `SetLimit` instead of manual pool management.

## 13. Rate Limiting with time.Ticker

`time.Ticker` delivers ticks at regular intervals. Combined with a channel read, it enforces a rate limit on operations.

**Fixed rate limiter:**

```go
func rateLimitedProcess(ctx context.Context, items []string, ratePerSec int) error {
	ticker := time.NewTicker(time.Second / time.Duration(ratePerSec))
	defer ticker.Stop()

	for _, item := range items {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := process(item); err != nil {
				return err
			}
		}
	}
	return nil
}
```

**Bursty rate limiter** (allows accumulation of tokens up to a burst size):

```go
func burstyLimiter(ctx context.Context, rate int, burst int) <-chan struct{} {
	tokens := make(chan struct{}, burst)

	// Pre-fill with burst tokens
	for range min(burst, burst) {
		tokens <- struct{}{}
	}

	// Refill at the given rate
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(rate))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case tokens <- struct{}{}:
				default: // bucket full, discard
				}
			}
		}
	}()

	return tokens
}
```

For production rate limiting, consider `golang.org/x/time/rate` which provides a token-bucket rate limiter with `rate.NewLimiter(rate, burst)` supporting `Allow`, `Wait`, and `Reserve` methods.

## 14. Graceful Shutdown with Signal Handling

Graceful shutdown ensures in-flight work completes before the process exits. The standard approach uses `os/signal` to catch termination signals and `context.Context` to propagate the shutdown decision.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Create a context that is cancelled on SIGINT or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	// Start server in a goroutine
	go func() {
		fmt.Println("server listening on :8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for the signal
	<-ctx.Done()
	stop() // stop receiving further signals
	fmt.Println("shutting down gracefully...")

	// Give outstanding requests up to 30 seconds to complete
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "forced shutdown: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("server stopped cleanly")
}
```

Key points:

- `signal.NotifyContext` (Go 1.16+) is the cleanest way to tie OS signals to a context.
- Call `stop()` after receiving the first signal so a second signal can trigger the default behavior (immediate termination).
- Use a separate timeout context for the shutdown phase itself.
- In Kubernetes, set `terminationGracePeriodSeconds` to a value greater than your shutdown timeout.

## 15. Race Condition Detection and Atomic Operations

### Race Detector

Go's race detector is built into the toolchain. Enable it with:

```
go test -race ./...
go run -race main.go
go build -race -o myapp
```

The race detector instruments memory accesses at compile time and reports data races at runtime. It has roughly 2-10x CPU overhead and 5-15x memory overhead, so use it in tests and CI but not in production builds.

The race detector finds data races (two goroutines accessing the same variable concurrently where at least one access is a write) but does not find all concurrency bugs (e.g., deadlocks, livelocks).

### Atomic Operations

`sync/atomic` provides low-level atomic operations on integers, pointers, and the `atomic.Value` type. Use atomics for simple counters and flags where a mutex would be overkill.

Go 1.19+ introduced typed atomic types:

- `atomic.Bool` -- atomic boolean.
- `atomic.Int32`, `atomic.Int64`, `atomic.Uint32`, `atomic.Uint64` -- atomic integers.
- `atomic.Pointer[T]` -- atomic pointer (generic, type-safe).

```go
var (
	requestCount atomic.Int64
	isShutdown   atomic.Bool
)

func handleRequest() {
	if isShutdown.Load() {
		return // reject new requests
	}
	requestCount.Add(1)
	defer requestCount.Add(-1)
	// ... handle request
}

func shutdown() {
	isShutdown.Store(true)
	// Wait for in-flight requests to drain
	for requestCount.Load() > 0 {
		time.Sleep(100 * time.Millisecond)
	}
}
```

`atomic.Pointer[T]` example for lock-free config reload:

```go
type Config struct {
	MaxConns int
	Timeout  time.Duration
}

var currentConfig atomic.Pointer[Config]

func init() {
	currentConfig.Store(&Config{MaxConns: 100, Timeout: 5 * time.Second})
}

func GetConfig() *Config {
	return currentConfig.Load()
}

func ReloadConfig(newCfg *Config) {
	currentConfig.Store(newCfg) // atomic swap, readers see old or new, never partial
}
```

## 16. Range-Over-Func Iterators (Go 1.22+)

Go 1.22 introduced range-over-integer (`for i := range n`) and Go 1.23 stabilized range-over-func iterators. These allow custom iteration functions to be used with the `for-range` statement.

Iterator function signatures:

- `func(yield func() bool)` -- iterator with no values.
- `func(yield func(V) bool)` -- single-value iterator (`iter.Seq[V]`).
- `func(yield func(K, V) bool)` -- two-value iterator (`iter.Seq2[K, V]`).

The `yield` function returns `false` if the loop body executed a `break`, so the iterator should stop producing values and return.

Using iterators with concurrency patterns:

```go
package main

import (
	"fmt"
	"iter"
	"sync"
)

// ConcurrentCollect runs fn on each item from seq concurrently (up to maxWorkers)
// and returns the results in arbitrary order.
func ConcurrentCollect[In, Out any](seq iter.Seq[In], maxWorkers int, fn func(In) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		var (
			wg sync.WaitGroup
			mu sync.Mutex
			stop bool
		)
		sem := make(chan struct{}, maxWorkers)

		for v := range seq {
			mu.Lock()
			if stop {
				mu.Unlock()
				break
			}
			mu.Unlock()

			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				result := fn(v)

				mu.Lock()
				defer mu.Unlock()
				if !stop {
					if !yield(result) {
						stop = true
					}
				}
			}()
		}
		wg.Wait()
	}
}

// Channel returns an iterator over values received from a channel.
func Channel[T any](ch <-chan T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for v := range ch {
			if !yield(v) {
				return
			}
		}
	}
}

func main() {
	// Example: iterate over channel values using range-over-func
	ch := make(chan int, 5)
	go func() {
		defer close(ch)
		for i := range 5 {
			ch <- i * 10
		}
	}()

	for v := range Channel(ch) {
		fmt.Println(v) // 0, 10, 20, 30, 40
	}
}
```

The `iter` package in the standard library provides `Seq[V]` and `Seq2[K, V]` type aliases and helpers like `iter.Pull` which converts a push-style iterator into a pull-style (next/stop) pair -- useful for interleaving iteration with other control flow.

## 17. Best Practices

1. **Start goroutines with a clear owner and shutdown path.** Every goroutine you launch should have a mechanism (context, done channel, or WaitGroup) that guarantees it will terminate. Document the contract.

2. **Prefer context.Context over bare done channels.** Context propagates deadlines and cancellation through call chains and is the idiomatic Go approach. Use `signal.NotifyContext` for OS signal integration.

3. **Use errgroup for groups of related goroutines.** It combines WaitGroup, error propagation, and context cancellation in one package. Use `SetLimit` for built-in concurrency control.

4. **Prefer channels for communication, mutexes for state protection.** If goroutines exchange data, use channels. If multiple goroutines access shared memory, protect it with a mutex. Do not mix both approaches for the same data without careful thought.

5. **Close channels from the sender side only.** The sender knows when there is no more data. The receiver detects closure via the two-value receive or `range`.

6. **Use directional channel types in function signatures.** `<-chan T` and `chan<- T` make intent explicit and catch mistakes at compile time.

7. **Run `go test -race` in CI for every package.** Data races are subtle and often non-deterministic. The race detector catches them with high probability if the code path is exercised.

8. **Prefer `sync/atomic` typed wrappers over raw `atomic.AddInt64`.** The typed wrappers (`atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]`) are safer and more readable.

9. **Use `sync.Pool` for high-allocation hot paths.** Buffer pools for encoding, serialization, and I/O can significantly reduce GC pressure. Always `Reset()` objects before returning them to the pool.

10. **Benchmark before optimizing.** Use `go test -bench` and pprof to identify actual bottlenecks. Do not add concurrency complexity without evidence that it helps.

11. **Bound concurrency.** Unbounded goroutine creation leads to resource exhaustion. Use worker pools, `errgroup.SetLimit`, semaphores, or buffered channels to cap parallelism.

12. **Avoid goroutine leaks.** A goroutine that blocks forever on a channel send/receive is a memory leak. Use `goleak` in tests to detect leaked goroutines.

## 18. Anti-Patterns

1. **Fire-and-forget goroutines without cancellation.** Launching `go doSomething()` without any way to stop it or wait for it leads to goroutine leaks, especially in long-running services.

2. **Calling `wg.Add` inside the goroutine.** This creates a race between `wg.Wait()` and the goroutine starting. Always call `wg.Add` before the `go` statement.

3. **Closing a channel from the receiver.** This can cause panics if the sender writes to the closed channel. Only the sender should close.

4. **Sending on a closed channel.** This panics. Ensure your protocol guarantees no sends happen after close. Use sync primitives or a `sync.Once` to protect the close operation if multiple goroutines may attempt it.

5. **Ignoring context cancellation in long operations.** If a function accepts a context but never checks `ctx.Done()`, the cancellation signal is useless. Check context between iterations of loops and before expensive operations.

6. **Using `sync.Map` as a general-purpose concurrent map.** `sync.Map` is slower than `map` + `sync.RWMutex` for most workloads. Only use it for the specific access patterns it is optimized for (write-once-read-many or disjoint key sets).

7. **Nested locking without consistent order.** Acquiring mutex A then B in one goroutine and B then A in another causes deadlocks. Establish and document a global lock ordering.

8. **Buffered channels as semaphores without proper draining.** If you use a buffered channel as a semaphore, you must drain it (or use a WaitGroup alongside) to ensure all goroutines complete before proceeding.

9. **Using `time.Sleep` for synchronization.** Sleep-based synchronization is fragile and non-deterministic. Use channels, WaitGroups, or contexts instead.

10. **Returning shared mutable data from goroutines without synchronization.** Writing to a shared slice or map from multiple goroutines without a mutex causes data races. Either give each goroutine its own output slot (indexed by goroutine number) or collect results through a channel.

11. **Capturing loop variables in goroutine closures (pre-Go 1.22).** Before Go 1.22, loop variables were shared across iterations. While Go 1.22+ fixes this, be aware when maintaining older codebases. Always specify your minimum Go version in `go.mod`.

12. **Using `sync.Pool` for objects with long lifetimes.** Pool objects can be garbage collected at any time. Do not rely on objects persisting in the pool. Pool is for short-lived, high-frequency allocations.

## 19. Sources & References

- [Effective Go - Concurrency](https://go.dev/doc/effective_go#concurrency) -- Official Go concurrency guide covering goroutines, channels, and the share-by-communicating philosophy.
- [Go Concurrency Patterns (Rob Pike, Google I/O 2012)](https://go.dev/blog/pipelines) -- The Go Blog article on pipelines and cancellation patterns, with the foundational fan-out/fan-in and pipeline design.
- [Go Data Race Detector](https://go.dev/doc/articles/race_detector) -- Official documentation for the `-race` flag, how it works, and common data races it detects.
- [Package sync - Go Standard Library](https://pkg.go.dev/sync) -- API documentation for Mutex, RWMutex, WaitGroup, Once, OnceFunc, OnceValue, Map, and Pool.
- [Package context - Go Standard Library](https://pkg.go.dev/context) -- API documentation for context.Context, WithCancel, WithTimeout, WithDeadline, WithValue, AfterFunc, and WithoutCancel.
- [Package sync/atomic - Go Standard Library](https://pkg.go.dev/sync/atomic) -- API documentation for atomic types (Bool, Int64, Pointer[T]) and operations.
- [golang.org/x/sync/errgroup](https://pkg.go.dev/golang.org/x/sync/errgroup) -- API documentation for errgroup.Group, WithContext, SetLimit, and TryGo.
- [golang.org/x/sync/semaphore](https://pkg.go.dev/golang.org/x/sync/semaphore) -- Weighted semaphore implementation for bounded concurrency.
- [golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate) -- Token-bucket rate limiter with Allow, Wait, and Reserve methods.
- [Go 1.22 Release Notes - Range over integers and functions](https://go.dev/doc/go1.22) -- Details on range-over-int and the experimental range-over-func iterator support.
- [Go 1.23 Release Notes - Range over func](https://go.dev/doc/go1.23) -- Stabilization of range-over-func iterators and the iter package.
- [Package iter - Go Standard Library](https://pkg.go.dev/iter) -- API documentation for Seq, Seq2, Pull, and Pull2.
- [Uber Go Style Guide - Concurrency](https://github.com/uber-go/guide/blob/master/style.md) -- Industry best practices for Go concurrency from Uber's engineering team.
