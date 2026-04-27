package clickhouse

import (
	"context"
	"fmt"
	"reflect"

	clickhousedriver "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/caffeaun/marc/internal/config"
)

// Client is the interface that callers depend on. It is defined at the
// consumer side so that tests can substitute a fake without a live database.
type Client interface {
	// InsertEvent writes a single Event row to marc.events using a batch
	// of size 1. The row is immediately visible to subsequent SELECT queries
	// (subject to ReplacingMergeTree dedup-on-merge — see Event godoc).
	InsertEvent(ctx context.Context, e Event) error

	// QueryEvents executes a SELECT and returns every row as a map[string]any
	// keyed by column name. Use Exec for statements that do not return rows
	// (DDL, OPTIMIZE, ALTER, etc.).
	QueryEvents(ctx context.Context, sql string, args ...any) ([]map[string]any, error)

	// Exec runs a statement that returns no rows (DDL such as CREATE TABLE,
	// OPTIMIZE TABLE, ALTER, INSERT VALUES, etc.). It is the right entrypoint
	// for marc-server init schema bootstrap and for OPTIMIZE in the dedup test.
	Exec(ctx context.Context, sql string, args ...any) error

	// Ping verifies that the server is reachable and the credentials are valid.
	Ping(ctx context.Context) error

	// Close releases the underlying connection pool.
	Close() error
}

// productionClient wraps the native clickhouse-go driver connection.
type productionClient struct {
	conn clickhousedriver.Conn
	db   string // database name, used to build INSERT statements
}

// Connect opens a native TCP connection pool to the ClickHouse server
// described by cfg. It returns a clear error when the server is unreachable
// (network timeout, wrong port, refused connection).
//
// The native driver (clickhouse.Open) is used instead of the database/sql
// wrapper because it supports the PrepareBatch / AppendStruct batch API that
// is more efficient and type-safe for structured row inserts.
func Connect(cfg config.ClickHouseConfig) (Client, error) {
	opts := &clickhousedriver.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhousedriver.Auth{
			Database: cfg.Database,
			Username: cfg.User,
			Password: cfg.Password,
		},
	}

	conn, err := clickhousedriver.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open: %w", err)
	}

	return &productionClient{conn: conn, db: cfg.Database}, nil
}

// InsertEvent opens a single-row batch, appends the Event via AppendStruct,
// and sends it to ClickHouse. The batch INSERT is the canonical write path
// recommended by clickhouse-go/v2 for structured row inserts.
func (c *productionClient) InsertEvent(ctx context.Context, e Event) error {
	// PrepareBatch validates the column count and types eagerly.
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO "+c.db+".events")
	if err != nil {
		return fmt.Errorf("clickhouse: prepare batch: %w", err)
	}

	if err := batch.AppendStruct(&e); err != nil {
		// Attempt to abort to avoid a dangling open batch.
		_ = batch.Abort()
		return fmt.Errorf("clickhouse: append struct: %w", err)
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("clickhouse: send batch: %w", err)
	}
	return nil
}

// QueryEvents executes sql with args and scans every row into a map[string]any.
// Column types are derived from rows.ColumnTypes() so callers receive native Go
// values (int64, float64, string, time.Time, etc.) rather than raw bytes.
func (c *productionClient) QueryEvents(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := c.conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: query: %w", err)
	}
	defer rows.Close()

	columnTypes := rows.ColumnTypes()
	var result []map[string]any

	for rows.Next() {
		// Allocate a slice of pointers to new zero values for each column type.
		// reflect.New creates a pointer to the underlying type; rows.Scan fills
		// the values through those pointers.
		ptrs := make([]any, len(columnTypes))
		for i, ct := range columnTypes {
			ptrs[i] = reflect.New(ct.ScanType()).Interface()
		}

		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("clickhouse: scan row: %w", err)
		}

		row := make(map[string]any, len(columnTypes))
		for i, ct := range columnTypes {
			// Dereference the pointer to get the actual value.
			row[ct.Name()] = reflect.ValueOf(ptrs[i]).Elem().Interface()
		}
		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("clickhouse: rows iteration: %w", err)
	}
	return result, nil
}

// Exec runs a no-rows statement (DDL / OPTIMIZE / ALTER / INSERT VALUES) via
// the driver's Exec method. Use QueryEvents for SELECTs.
func (c *productionClient) Exec(ctx context.Context, sql string, args ...any) error {
	if err := c.conn.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("clickhouse: exec: %w", err)
	}
	return nil
}

// Ping delegates to the driver's Ping which sends a lightweight keep-alive
// packet and verifies authentication.
func (c *productionClient) Ping(ctx context.Context) error {
	if err := c.conn.Ping(ctx); err != nil {
		return fmt.Errorf("clickhouse: ping: %w", err)
	}
	return nil
}

// Close releases all resources held by the connection pool.
func (c *productionClient) Close() error {
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("clickhouse: close: %w", err)
	}
	return nil
}
