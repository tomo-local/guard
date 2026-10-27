package report

// StateStore aggregates check.jsonl/record.jsonl into an in-memory
// Report that the caller renders itself (RenderText/RenderHTML). This
// is "self-managed state": every byte of aggregation happens in this
// process, in Go, with no external dependency.
//
// This package only ever renders a report -- it doesn't own where or
// how guard's data is persisted (that's internal/sidecar's job, via
// AppendCheckLog/AppendRecordLog and, for DuckDB, sidecar.DuckDBStore).
// StateStore exists as its own interface purely so a report backend has
// the same "injectable interface" shape as sidecar.DuckDBStore, even
// though -- unlike DuckDB, which is an external process -- there's
// nothing here that actually needs swapping out today.
type StateStore interface {
	Analyze(checkLogPath, recordLogPath string) Report
}

// GoStateStore is the StateStore backed by this package's own JSONL
// parsing and in-memory aggregation (analyze.go).
type GoStateStore struct{}

func (GoStateStore) Analyze(checkLogPath, recordLogPath string) Report {
	return Analyze(checkLogPath, recordLogPath)
}
