package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/TuanFaied/qryvanta/internal/analyzer"
	"github.com/TuanFaied/qryvanta/internal/connection"
	"github.com/TuanFaied/qryvanta/internal/instrumentation"
	"github.com/TuanFaied/qryvanta/internal/parser"
	"github.com/TuanFaied/qryvanta/internal/schema"
)

// metadataTimeout bounds the catalog reads of one analysis.
const metadataTimeout = 15 * time.Second

// AnalysisTarget is the connection and database whose catalog the metadata rules read. The
// zero value analyzes without metadata.
type AnalysisTarget struct {
	ConnectionID string
	Database     string
}

// Analysis is the result of analyzing a script.
type Analysis struct {
	// ID names the analysis for RunProbes.
	ID string `json:"id"`
	analyzer.Report
	// Statements is the number of top-level statements that were analyzed.
	Statements int `json:"statements"`
	// SyntaxErrors is the number of syntax errors; the batches that have them were not analyzed.
	SyntaxErrors int `json:"syntaxErrors"`
	// Metadata says whether the metadata rules could run, and which tables they could not check.
	Metadata MetadataStatus `json:"metadata"`
	// Probes are the queries that could confirm or dismiss findings, generated from fixed
	// templates (D4). They need a connection and run only when the user approves them.
	Probes []instrumentation.Probe `json:"probes"`
}

// MetadataStatus says what the metadata rules could check.
type MetadataStatus struct {
	// Checked is true when the catalog of a connection was read; Reason says why not.
	Checked bool   `json:"checked"`
	Reason  string `json:"reason"`
	// Unresolved lists the tables and views that could not be checked, and why.
	Unresolved []UnresolvedTable `json:"unresolved"`
}

// UnresolvedTable is a table or view the metadata rules could not check.
type UnresolvedTable struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// Analyze parses sql with the QueryModel and runs the built-in diagnostic rules over it. With a
// connected target, it first reads the catalog entries of the tables the script uses (cached per
// connection), so metadata rules run and static findings can be confirmed or dismissed. It
// executes nothing. A parser that cannot run is an error (wrapping parser.ErrUnavailable); rule
// failures and unresolved tables are part of the result.
func (c *Core) Analyze(ctx context.Context, sql string, target AnalysisTarget) (Analysis, error) {
	res, err := c.parser.Parse(ctx, sql, parser.Options{Model: true})
	if err != nil {
		return Analysis{}, err
	}
	in := &analyzer.Input{SQL: sql, Result: res}
	var status MetadataStatus
	in.Catalog, status = c.catalog(ctx, res, target)
	rep, err := analyzer.NewEngine(analyzer.Default()).Analyze(ctx, in)
	if err != nil {
		return Analysis{}, err
	}
	a := Analysis{ID: newID(), Report: rep, SyntaxErrors: len(res.Diagnostics), Metadata: status, Probes: []instrumentation.Probe{}}
	if status.Checked {
		a.Probes = c.planProbes(ctx, rep.Findings)
	}
	c.analyses.add(&analysisRecord{id: a.ID, target: target, sql: sql, findings: rep.Findings, probes: a.Probes})
	for _, b := range res.Batches {
		a.Statements += len(b.Statements)
	}
	for _, f := range rep.Failures {
		c.log.Warn("a diagnostic rule failed", "rule", f.RuleID, "error", f.Message)
	}
	c.log.Debug("query analyzed", "statements", a.Statements, "findings", len(rep.Findings),
		"rule_failures", len(rep.Failures), "syntax_errors", a.SyntaxErrors, "metadata", status.Checked,
		"unresolved_tables", len(status.Unresolved))
	return a, nil
}

// catalog reads the metadata of every table the script uses. Problems are reported per table and
// never fail the analysis.
func (c *Core) catalog(ctx context.Context, res *parser.Result, target AnalysisTarget) (*analyzer.Catalog, MetadataStatus) {
	status := MetadataStatus{Unresolved: []UnresolvedTable{}}
	if target.ConnectionID == "" {
		status.Reason = "No connection: connect to check keys, types and nullability."
		return nil, status
	}
	db, err := c.pool.DB(target.ConnectionID)
	if errors.Is(err, connection.ErrNotConnected) {
		status.Reason = "Not connected: connect to check keys, types and nullability."
		return nil, status
	}
	if err != nil {
		status.Reason = "The connection cannot be used: " + err.Error()
		return nil, status
	}
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	cat := analyzer.NewCatalog()
	for _, name := range analyzer.TableNames(res) {
		label := displayName(name)
		var lookup analyzer.TableLookup
		switch {
		case name.Server != "":
			lookup.Problem = "on a linked server; not checked"
		default:
			database := name.Database
			if database == "" {
				database = target.Database
			}
			if database == "" {
				lookup.Problem = "no database is selected"
				break
			}
			lookup = c.meta.table(ctx, target.ConnectionID, database, name, func(ctx context.Context) (schema.Table, error) {
				return c.schema.Table(ctx, db, database, name.Schema, name.Name)
			})
		}
		if lookup.Table == nil {
			status.Unresolved = append(status.Unresolved, UnresolvedTable{Name: label, Reason: lookup.Problem})
		}
		cat.Set(name, lookup)
	}
	status.Checked = true
	return cat, status
}

// ForgetMetadata drops the cached catalog entries of a connection (the explorer was refreshed,
// or the connection closed).
func (c *Core) ForgetMetadata(connectionID string) { c.meta.forget(connectionID) }

func displayName(n parser.ObjectName) string {
	var parts []string
	for _, p := range []string{n.Server, n.Database, n.Schema, n.Name} {
		if p != "" || len(parts) > 0 {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}

// metadataCache keeps table metadata per connection, database and name as written. Found and
// not-found results are cached; other errors (timeouts, a dropped connection) are retried next time.
type metadataCache struct {
	mu sync.Mutex
	m  map[string]map[string]analyzer.TableLookup
}

func (m *metadataCache) table(ctx context.Context, connectionID, database string, name parser.ObjectName,
	read func(context.Context) (schema.Table, error)) analyzer.TableLookup {
	key := strings.ToLower(database) + "|" + analyzer.CatalogKey(name)
	m.mu.Lock()
	if l, ok := m.m[connectionID][key]; ok {
		m.mu.Unlock()
		return l
	}
	m.mu.Unlock()

	t, err := read(ctx)
	var l analyzer.TableLookup
	cache := true
	switch {
	case err == nil:
		l.Table = &t
	case errors.Is(err, schema.ErrObjectNotFound):
		l.Problem = "not found, or no permission to see it"
	case errors.Is(err, schema.ErrDatabaseNotFound):
		l.Problem = "database " + database + " not found or not accessible"
	case ctx.Err() != nil:
		l.Problem, cache = "reading the catalog timed out or was cancelled", false
	default:
		l.Problem, cache = "could not read the catalog: "+err.Error(), false
	}
	if cache {
		m.mu.Lock()
		if m.m == nil {
			m.m = map[string]map[string]analyzer.TableLookup{}
		}
		if m.m[connectionID] == nil {
			m.m[connectionID] = map[string]analyzer.TableLookup{}
		}
		m.m[connectionID][key] = l
		m.mu.Unlock()
	}
	return l
}

func (m *metadataCache) forget(connectionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, connectionID)
}
