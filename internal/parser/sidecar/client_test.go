package sidecar

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// The test binary doubles as a fake sidecar: with QRYVANTA_FAKE_SIDECAR set it speaks the
// protocol instead of running tests. The SQL text scripts its behaviour:
//
//	SLEEP    never answer              CRASH   exit without answering
//	SLOW     answer after 300 ms       WRONGID answer with another id
//	GARBAGE  answer with non-JSON      BAD     answer with a protocol error
//	ERR      report a diagnostic at the first "ERR"
func TestMain(m *testing.M) {
	if mode := os.Getenv("QRYVANTA_FAKE_SIDECAR"); mode != "" {
		fakeSidecar(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func u16len(s string) int { return len(utf16.Encode([]rune(s))) }

func fakeSidecar(mode string) {
	out := bufio.NewWriter(os.Stdout)
	write := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(b)
		out.WriteByte('\n')
		out.Flush()
	}
	switch mode {
	case "nohello":
		time.Sleep(time.Minute)
		return
	case "badhello":
		fmt.Println("hello there")
		return
	case "exit":
		os.Exit(4)
	}
	protocol := 1
	if mode == "protocol2" {
		protocol = 2
	}
	// The pid identifies the process, so tests can tell whether it was restarted.
	write(map[string]any{"type": "hello", "protocol": protocol, "version": strconv.Itoa(os.Getpid()), "scriptDom": "fake"})

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 64<<20)
	for in.Scan() {
		var req request
		if err := json.Unmarshal(in.Bytes(), &req); err != nil {
			write(map[string]any{"id": nil, "ok": false, "error": map[string]string{"code": "bad_request", "message": err.Error()}})
			continue
		}
		sql := req.SQL
		switch {
		case strings.Contains(sql, "SLEEP"):
			time.Sleep(time.Minute)
		case strings.Contains(sql, "CRASH"):
			os.Exit(3)
		case strings.Contains(sql, "SLOW"):
			time.Sleep(300 * time.Millisecond)
		case strings.Contains(sql, "WRONGID"):
			req.ID = "nope"
		case strings.Contains(sql, "GARBAGE"):
			fmt.Println("this is not json")
			continue
		case strings.Contains(sql, "BAD"):
			write(map[string]any{"id": req.ID, "ok": false, "error": map[string]string{"code": "too_large", "message": "too big"}})
			continue
		}
		resp := map[string]any{"id": req.ID, "ok": true, "compatLevel": 170}
		if req.Op == "parse" {
			resp["batches"] = []map[string]any{{"index": 0, "text": sql, "offset": 0, "length": u16len(sql), "line": 1, "column": 1, "repeat": 1}}
			diags := []map[string]any{}
			if i := strings.Index(sql, "ERR"); i >= 0 {
				diags = append(diags, map[string]any{"number": 46010, "message": "Incorrect syntax near 'ERR'.",
					"offset": u16len(sql[:i]), "line": 1, "column": u16len(sql[:i]) + 1})
			}
			resp["diagnostics"] = diags
		}
		write(resp)
	}
}

func fakeClient(t *testing.T, mode string, tweak func(*Options)) *Client {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	o := Options{
		Path:         exe,
		Env:          append(os.Environ(), "QRYVANTA_FAKE_SIDECAR="+mode),
		Timeout:      2 * time.Second,
		StartTimeout: 10 * time.Second,
	}
	if tweak != nil {
		tweak(&o)
	}
	c := New(o)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func pid(t *testing.T, c *Client) string {
	t.Helper()
	st := c.Check(context.Background())
	if !st.Available {
		t.Fatalf("parser unavailable: %s", st.Reason)
	}
	return st.Version
}

func TestParseRoundTripAndByteOffsets(t *testing.T) {
	c := fakeClient(t, "ok", nil)
	sql := "SELECT N'🐘', 'é' ERR"
	res, err := c.Parse(context.Background(), sql, parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.CompatLevel != 170 || len(res.Batches) != 1 || len(res.Diagnostics) != 1 {
		t.Fatalf("result = %+v", res)
	}
	b := res.Batches[0]
	if b.Text != sql || b.ByteOffset != 0 || b.ByteLength != len(sql) || b.Length != u16len(sql) {
		t.Errorf("batch = %+v", b)
	}
	d := res.Diagnostics[0]
	if d.ByteOffset != strings.Index(sql, "ERR") || sql[d.ByteOffset:] != "ERR" {
		t.Errorf("diagnostic byte offset %d does not point at ERR (utf16 offset %d)", d.ByteOffset, d.Offset)
	}
	st := c.Check(context.Background())
	if !st.Available || st.ScriptDom != "fake" || st.Protocol != 1 {
		t.Errorf("status = %+v", st)
	}
}

func TestConcurrentCallersGetTheirOwnResults(t *testing.T) {
	c := fakeClient(t, "ok", nil)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sql := fmt.Sprintf("SELECT %d", i)
			res, err := c.Parse(context.Background(), sql, parser.Options{})
			if err != nil {
				errs <- err
				return
			}
			if res.Batches[0].Text != sql {
				errs <- fmt.Errorf("asked for %q, got %q", sql, res.Batches[0].Text)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestTimeoutRestartsTheProcess(t *testing.T) {
	c := fakeClient(t, "ok", func(o *Options) { o.Timeout = 300 * time.Millisecond })
	before := pid(t, c)
	_, err := c.Parse(context.Background(), "SLEEP", parser.Options{})
	if !errors.Is(err, parser.ErrUnavailable) || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Parse(context.Background(), "SELECT 1", parser.Options{}); err != nil {
		t.Fatalf("parse after restart: %v", err)
	}
	if after := pid(t, c); after == before {
		t.Error("the process was not restarted after a timeout")
	}
}

func TestCrashMidRequestRestarts(t *testing.T) {
	c := fakeClient(t, "ok", nil)
	before := pid(t, c)
	if _, err := c.Parse(context.Background(), "CRASH", parser.Options{}); !errors.Is(err, parser.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Parse(context.Background(), "SELECT 1", parser.Options{}); err != nil {
		t.Fatal(err)
	}
	if pid(t, c) == before {
		t.Error("expected a new process")
	}
}

func TestCancelledCallerKeepsTheProcess(t *testing.T) {
	c := fakeClient(t, "ok", nil)
	before := pid(t, c)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Parse(ctx, "SLOW", parser.Options{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	// The next request waits for the abandoned response, then gets its own.
	res, err := c.Parse(context.Background(), "SELECT 2", parser.Options{})
	if err != nil || res.Batches[0].Text != "SELECT 2" {
		t.Fatalf("parse after cancel = %+v, %v", res, err)
	}
	if pid(t, c) != before {
		t.Error("cancelling a request must not restart the sidecar")
	}
}

func TestQueuedCallerHonoursItsContext(t *testing.T) {
	c := fakeClient(t, "ok", nil)
	pid(t, c)
	go func() { _, _ = c.Parse(context.Background(), "SLOW", parser.Options{}) }()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Parse(ctx, "SELECT 1", parser.Options{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued caller: err = %v", err)
	}
}

func TestProtocolErrorsKeepTheProcess(t *testing.T) {
	c := fakeClient(t, "ok", nil)
	before := pid(t, c)
	_, err := c.Parse(context.Background(), "BAD", parser.Options{})
	var pe *ProtocolError
	if !errors.As(err, &pe) || pe.Code != "too_large" || errors.Is(err, parser.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if pid(t, c) != before {
		t.Error("a protocol error must not restart the sidecar")
	}
}

func TestMisbehavingSidecarIsRestarted(t *testing.T) {
	for _, sql := range []string{"WRONGID", "GARBAGE"} {
		t.Run(sql, func(t *testing.T) {
			c := fakeClient(t, "ok", nil)
			before := pid(t, c)
			if _, err := c.Parse(context.Background(), sql, parser.Options{}); !errors.Is(err, parser.ErrUnavailable) {
				t.Fatalf("err = %v", err)
			}
			if pid(t, c) == before {
				t.Error("expected a restart")
			}
		})
	}
}

func TestStartFailures(t *testing.T) {
	cases := map[string]string{
		"nohello":   "did not start within",
		"badhello":  "did not identify itself",
		"protocol2": "protocol 2",
		"exit":      "exited before it was ready",
	}
	for mode, want := range cases {
		t.Run(mode, func(t *testing.T) {
			c := fakeClient(t, mode, func(o *Options) { o.StartTimeout = 500 * time.Millisecond })
			_, err := c.Parse(context.Background(), "SELECT 1", parser.Options{})
			if !errors.Is(err, parser.ErrUnavailable) || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want %q", err, want)
			}
			if st := c.Check(context.Background()); st.Available || !strings.Contains(st.Reason, want) {
				t.Errorf("status = %+v", st)
			}
		})
	}
}

func TestMissingExecutableAndCooldown(t *testing.T) {
	missing := filepath.Join(t.TempDir(), parser.ExecutableName())
	c := New(Options{Path: missing, MaxFailures: 2, Cooldown: time.Hour})
	defer c.Close()

	for i := range 2 {
		_, err := c.Parse(context.Background(), "SELECT 1", parser.Options{})
		if !errors.Is(err, parser.ErrUnavailable) || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("attempt %d: err = %v", i, err)
		}
	}
	// In cool-down: fails fast without trying again.
	_, err := c.Parse(context.Background(), "SELECT 1", parser.Options{})
	if !errors.Is(err, parser.ErrUnavailable) || !strings.Contains(err.Error(), "retrying in") {
		t.Fatalf("err = %v", err)
	}
	st := c.Check(context.Background())
	if st.Available || st.Path != missing || !strings.Contains(st.Reason, "not found") {
		t.Errorf("status = %+v", st)
	}
}

func TestCloseStopsTheProcessWithoutLeaks(t *testing.T) {
	baseline := runtime.NumGoroutine()
	exe, _ := os.Executable()
	c := New(Options{Path: exe, Env: append(os.Environ(), "QRYVANTA_FAKE_SIDECAR=ok")})
	if _, err := c.Parse(context.Background(), "SELECT 1", parser.Options{}); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.waited:
	case <-time.After(5 * time.Second):
		t.Fatal("the sidecar process is still running after Close")
	}
	if _, err := c.Parse(context.Background(), "SELECT 1", parser.Options{}); !errors.Is(err, parser.ErrUnavailable) {
		t.Errorf("parse after Close: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline {
		t.Errorf("goroutines: %d after Close, %d before New", n, baseline)
	}
}
