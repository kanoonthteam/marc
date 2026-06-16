package generate

import (
	"context"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
)

// TestRun_ShufflesOptionsAndRecordsActual verifies A/B positions are randomized
// and actual_option records which displayed slot holds the path actually taken
// (the model emits option_a = actual). RandIntn is forced to make the swap
// deterministic.
func TestRun_ShufflesOptionsAndRecordsActual(t *testing.T) {
	t.Parallel()
	cand := `[{"situation":"S1","question":"Q1","option_a":"ACTUAL","option_b":"ALT","principle_tested":"p","durability_score":9,"obviousness_score":3}]`

	cases := []struct {
		name                     string
		coin                     int
		wantA, wantB, wantActual string
	}{
		{"swap", 0, "ALT", "ACTUAL", "B"},
		{"noswap", 1, "ACTUAL", "ALT", "A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
			db := openTempSQLite(t)
			if err := Run(context.Background(), Options{
				Config:            minimalConfig(),
				NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) { return chFake, nil },
				SQLiteDB:          db,
				ClaudeRunner:      fakeClaude(cand),
				RandIntn:          func(int) int { return tc.coin },
			}); err != nil {
				t.Fatalf("Run: %v", err)
			}
			var a, b, actual string
			if err := db.ExportDB().QueryRow(
				`SELECT option_a, option_b, actual_option FROM pending_questions WHERE status='ready'`,
			).Scan(&a, &b, &actual); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if a != tc.wantA || b != tc.wantB || actual != tc.wantActual {
				t.Errorf("got A=%q B=%q actual=%q; want A=%q B=%q actual=%q",
					a, b, actual, tc.wantA, tc.wantB, tc.wantActual)
			}
		})
	}
}

// TestRun_MinimaxBackendSelected verifies that with randomize_backend on and the
// coin landing on minimax, generation uses the injected MinimaxGenerate (not
// claude) and still inserts the parsed candidate.
func TestRun_MinimaxBackendSelected(t *testing.T) {
	t.Parallel()
	chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
	db := openTempSQLite(t)
	cfg := minimalConfig()
	cfg.Generation.RandomizeBackend = true
	cfg.Generation.MinimaxModel = "MiniMax-M3"

	cand := `[{"situation":"S","question":"Q","option_a":"A","option_b":"B","principle_tested":"p","durability_score":9,"obviousness_score":3}]`
	mmCalled := false
	if err := Run(context.Background(), Options{
		Config:            cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) { return chFake, nil },
		SQLiteDB:          db,
		ClaudeRunner: func(_ context.Context, _ string, _ string, _ []string) ([]byte, []byte, error) {
			t.Error("claude must not be called when minimax backend is selected")
			return nil, nil, nil
		},
		MinimaxGenerate: func(_ context.Context, _ config.MiniMaxConfig, model, _ string) (string, error) {
			mmCalled = true
			if model != "MiniMax-M3" {
				t.Errorf("minimax model = %q, want MiniMax-M3", model)
			}
			return cand, nil
		},
		RandIntn: func(int) int { return 0 }, // backend -> minimax, shuffle -> swap
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !mmCalled {
		t.Error("MinimaxGenerate was not called")
	}
	var n int
	if err := db.ExportDB().QueryRow(`SELECT COUNT(*) FROM pending_questions WHERE status='ready'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("inserted %d ready questions, want 1", n)
	}
}
