// Package sidecar implements parser.Parser with the .NET ScriptDom sidecar
// (sidecar/tsqlparser, protocol 1; see docs/architecture D2).
//
// One long-lived process serves one request at a time. A request that times out kills the
// process (it may be stuck); the next request starts a new one. A caller that gives up (its
// context ends) does not: its response is read and discarded in the background, so typing in
// the editor, which cancels parses constantly, never restarts the sidecar. Repeated start
// failures put the client into a cool-down during which Parse fails fast with
// parser.ErrUnavailable.
package sidecar

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TuanFaied/qryvanta/internal/parser"
)

// ProtocolVersion is the sidecar protocol this client speaks.
const ProtocolVersion = 1

// Options configure a Client. Zero values use the defaults.
type Options struct {
	// Path is the sidecar executable (default parser.DefaultPath()).
	Path string
	// Args are extra arguments; tests use them to run a fake sidecar.
	Args []string
	// Env replaces the process environment when non-nil.
	Env []string
	// Timeout bounds one parse request (default 5 s). A timeout restarts the process.
	Timeout time.Duration
	// StartTimeout bounds waiting for the hello message (default 15 s; a cold start of the
	// single-file executable takes about half a second).
	StartTimeout time.Duration
	// MaxFailures consecutive start failures put the client in cool-down (default 3).
	MaxFailures int
	// Cooldown is how long Parse fails fast after MaxFailures (default 30 s).
	Cooldown time.Duration
	// Log receives process lifecycle events and the sidecar's stderr (debug level).
	Log *slog.Logger
}

func (o *Options) defaults() {
	if o.Path == "" {
		o.Path = parser.DefaultPath()
	}
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.StartTimeout <= 0 {
		o.StartTimeout = 15 * time.Second
	}
	if o.MaxFailures <= 0 {
		o.MaxFailures = 3
	}
	if o.Cooldown <= 0 {
		o.Cooldown = 30 * time.Second
	}
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
}

// Client is a parser.Parser backed by the sidecar process.
type Client struct {
	opts Options
	// sem serializes requests: the sidecar answers one request at a time, in order.
	sem    chan struct{}
	nextID atomic.Uint64

	mu        sync.Mutex
	proc      *process
	status    parser.Status
	failures  int
	coolUntil time.Time
	closed    bool
}

var _ parser.Parser = (*Client)(nil)

// New creates a client. The process starts on the first Parse or Check.
func New(opts Options) *Client {
	opts.defaults()
	c := &Client{opts: opts, sem: make(chan struct{}, 1)}
	c.status = parser.Status{Path: opts.Path, Reason: "not started yet"}
	return c
}

// hello is the sidecar's first message.
type hello struct {
	Type      string `json:"type"`
	Protocol  int    `json:"protocol"`
	Version   string `json:"version"`
	ScriptDom string `json:"scriptDom"`
}

type request struct {
	ID          string `json:"id"`
	Op          string `json:"op"`
	SQL         string `json:"sql,omitempty"`
	CompatLevel int    `json:"compatLevel,omitempty"`
}

type response struct {
	ID          *string `json:"id"`
	OK          bool    `json:"ok"`
	CompatLevel int     `json:"compatLevel"`
	Batches     []struct {
		Index  int    `json:"index"`
		Text   string `json:"text"`
		Offset int    `json:"offset"`
		Length int    `json:"length"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
		Repeat int    `json:"repeat"`
	} `json:"batches"`
	Diagnostics []struct {
		Number  int    `json:"number"`
		Message string `json:"message"`
		Offset  int    `json:"offset"`
		Line    int    `json:"line"`
		Column  int    `json:"column"`
	} `json:"diagnostics"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ProtocolError is a well-formed error response from the sidecar (bad request, too large,
// unsupported compatibility level). It is not a reason to restart the process.
type ProtocolError struct {
	Code    string
	Message string
}

func (e *ProtocolError) Error() string { return "parser: " + e.Code + ": " + e.Message }

// Parse implements parser.Parser.
func (c *Client) Parse(ctx context.Context, sql string, opts parser.Options) (*parser.Result, error) {
	resp, err := c.roundTrip(ctx, request{Op: "parse", SQL: sql, CompatLevel: opts.CompatLevel})
	if err != nil {
		return nil, err
	}
	return convert(sql, resp), nil
}

// Check implements parser.Parser: it starts the process if needed and pings it.
func (c *Client) Check(ctx context.Context) parser.Status {
	_, err := c.roundTrip(ctx, request{Op: "ping"})
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.status
	if err != nil && st.Available {
		// The process answered hello but not the ping.
		st.Available, st.Reason = false, err.Error()
	}
	return st
}

// Close stops the process. Later calls fail with parser.ErrUnavailable.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	p := c.proc
	c.proc = nil
	c.status.Available, c.status.Reason = false, "closed"
	c.mu.Unlock()
	if p != nil {
		p.stop(false)
	}
	return nil
}

// roundTrip sends one request and waits for its response.
func (c *Client) roundTrip(ctx context.Context, req request) (*response, error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	released := false
	release := func() {
		if !released {
			released = true
			<-c.sem
		}
	}
	defer release()

	p, err := c.ensureProcess()
	if err != nil {
		return nil, err
	}

	req.ID = strconv.FormatUint(c.nextID.Add(1), 10)
	if err := p.send(req); err != nil {
		c.fail(p, "write request: "+err.Error())
		return nil, c.unavailable("the parser stopped: " + err.Error())
	}

	timer := time.NewTimer(c.opts.Timeout)
	defer timer.Stop()
	select {
	case line, ok := <-p.lines:
		if !ok {
			c.fail(p, "exited: "+p.exitReason())
			return nil, c.unavailable("the parser stopped: " + p.exitReason())
		}
		return c.decode(p, req.ID, line)
	case <-timer.C:
		c.fail(p, fmt.Sprintf("no response within %s", c.opts.Timeout))
		return nil, fmt.Errorf("%w: the parser did not answer within %s", parser.ErrUnavailable, c.opts.Timeout)
	case <-ctx.Done():
		// Keep the process: read and discard the response in the background, and only then
		// let the next request through.
		released = true
		go func() {
			defer func() { <-c.sem }()
			drain := time.NewTimer(c.opts.Timeout)
			defer drain.Stop()
			select {
			case _, ok := <-p.lines:
				if !ok {
					c.fail(p, "exited: "+p.exitReason())
				}
			case <-drain.C:
				c.fail(p, fmt.Sprintf("no response within %s", c.opts.Timeout))
			}
		}()
		return nil, ctx.Err()
	}
}

func (c *Client) decode(p *process, id string, line []byte) (*response, error) {
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		c.fail(p, "invalid response: "+err.Error())
		return nil, c.unavailable("the parser sent an invalid response")
	}
	if resp.ID == nil || *resp.ID != id {
		got := "null"
		if resp.ID != nil {
			got = strconv.Quote(*resp.ID)
		}
		c.fail(p, fmt.Sprintf("response id %s does not match request %q", got, id))
		return nil, c.unavailable("the parser answered out of order")
	}
	if !resp.OK {
		if resp.Error == nil {
			return nil, &ProtocolError{Code: "unknown", Message: "error without details"}
		}
		return nil, &ProtocolError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return &resp, nil
}

// ensureProcess returns the running process, starting it when needed.
func (c *Client) ensureProcess() (*process, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("%w: closed", parser.ErrUnavailable)
	}
	if c.proc != nil && !c.proc.exited() {
		return c.proc, nil
	}
	c.proc = nil
	if now := time.Now(); now.Before(c.coolUntil) {
		return nil, fmt.Errorf("%w: %s (retrying in %s)", parser.ErrUnavailable, c.status.Reason,
			c.coolUntil.Sub(now).Round(time.Second))
	}

	p, h, err := startProcess(c.opts)
	if err != nil {
		c.failures++
		reason := err.Error()
		if errors.Is(err, fs.ErrNotExist) {
			reason = "the parser was not found at " + c.opts.Path
		}
		c.status = parser.Status{Path: c.opts.Path, Reason: reason}
		if c.failures >= c.opts.MaxFailures {
			c.coolUntil = time.Now().Add(c.opts.Cooldown)
			c.failures = 0
		}
		c.opts.Log.Warn("parser sidecar failed to start", "path", c.opts.Path, "err", err)
		return nil, fmt.Errorf("%w: %s", parser.ErrUnavailable, reason)
	}
	c.failures = 0
	c.proc = p
	c.status = parser.Status{Available: true, Path: c.opts.Path, Version: h.Version, ScriptDom: h.ScriptDom, Protocol: h.Protocol}
	c.opts.Log.Info("parser sidecar started", "version", h.Version, "scriptdom", h.ScriptDom, "pid", p.cmd.Process.Pid)
	return p, nil
}

// fail stops a process that misbehaved; the next request starts a new one.
func (c *Client) fail(p *process, reason string) {
	c.opts.Log.Warn("parser sidecar restarted", "reason", reason)
	c.mu.Lock()
	if c.proc == p {
		c.proc = nil
		c.status.Available, c.status.Reason = false, reason
	}
	c.mu.Unlock()
	p.stop(true)
}

func (c *Client) unavailable(reason string) error {
	return fmt.Errorf("%w: %s", parser.ErrUnavailable, reason)
}

// convert maps the wire response to parser types, adding byte offsets for Go callers.
func convert(sql string, r *response) *parser.Result {
	offsets := make([]int, 0, 2*len(r.Batches)+len(r.Diagnostics))
	for _, b := range r.Batches {
		offsets = append(offsets, b.Offset, b.Offset+b.Length)
	}
	for _, d := range r.Diagnostics {
		offsets = append(offsets, d.Offset)
	}
	bytesAt := parser.ByteOffsets(sql, offsets...)

	res := &parser.Result{
		CompatLevel: r.CompatLevel,
		Batches:     make([]parser.Batch, len(r.Batches)),
		Diagnostics: make([]parser.Diagnostic, len(r.Diagnostics)),
	}
	for i, b := range r.Batches {
		start, end := bytesAt[2*i], bytesAt[2*i+1]
		res.Batches[i] = parser.Batch{
			Index: b.Index, Text: b.Text, Offset: b.Offset, Length: b.Length,
			ByteOffset: start, ByteLength: end - start, Line: b.Line, Column: b.Column, Repeat: b.Repeat,
		}
	}
	base := 2 * len(r.Batches)
	for i, d := range r.Diagnostics {
		res.Diagnostics[i] = parser.Diagnostic{
			Number: d.Number, Message: d.Message, Offset: d.Offset, ByteOffset: bytesAt[base+i],
			Line: d.Line, Column: d.Column,
		}
	}
	return res
}

// process is one running sidecar.
type process struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	// lines carries stdout lines; closed when stdout ends.
	lines chan []byte
	// done is closed by stop, waited when the process has exited.
	done   chan struct{}
	waited chan struct{}

	mu      sync.Mutex
	waitErr error
	stopped bool
}

func startProcess(o Options) (*process, *hello, error) {
	if _, err := os.Stat(o.Path); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(o.Path, o.Args...)
	if o.Env != nil {
		cmd.Env = o.Env
	}
	hideWindow(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start %s: %w", o.Path, err)
	}

	p := &process{cmd: cmd, stdin: stdin, lines: make(chan []byte), done: make(chan struct{}), waited: make(chan struct{})}
	go func() {
		// The sidecar logs only internal errors; never SQL text.
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
		for sc.Scan() {
			o.Log.Debug("parser sidecar stderr", "line", sc.Text())
		}
	}()
	go func() {
		defer close(p.lines)
		r := bufio.NewReaderSize(stdout, 256*1024)
		for {
			line, err := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				select {
				case p.lines <- line:
				case <-p.done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(p.waited)
	}()

	var h hello
	select {
	case line, ok := <-p.lines:
		if !ok {
			<-p.waited
			reason := p.exitReason()
			p.stop(true)
			return nil, nil, fmt.Errorf("the parser exited before it was ready: %s", reason)
		}
		if err := json.Unmarshal(line, &h); err != nil || h.Type != "hello" {
			p.stop(true)
			return nil, nil, errors.New("the parser did not identify itself (is the path right?)")
		}
	case <-time.After(o.StartTimeout):
		p.stop(true)
		return nil, nil, fmt.Errorf("the parser did not start within %s", o.StartTimeout)
	}
	if h.Protocol != ProtocolVersion {
		p.stop(true)
		return nil, nil, fmt.Errorf("the parser speaks protocol %d, Qryvanta needs %d (reinstall Qryvanta)", h.Protocol, ProtocolVersion)
	}
	return p, &h, nil
}

func (p *process) send(r request) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// The sidecar's size limits assume unescaped text (see Protocol.cs).
	enc.SetEscapeHTML(false)
	if err := enc.Encode(r); err != nil {
		return err
	}
	_, err := p.stdin.Write(buf.Bytes())
	return err
}

// exited reports whether the process has ended or was stopped.
func (p *process) exited() bool {
	select {
	case <-p.waited:
		return true
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

func (p *process) exitReason() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.waitErr != nil {
		return p.waitErr.Error()
	}
	return "it closed its output"
}

// stop ends the process and waits for it to exit. With kill, it is killed at once (it
// misbehaved); otherwise closing stdin lets it exit cleanly, with a kill after 2 s.
// Safe to call more than once.
func (p *process) stop(kill bool) {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		<-p.waited
		return
	}
	p.stopped = true
	p.mu.Unlock()
	close(p.done)
	_ = p.stdin.Close() // a healthy sidecar exits at end of input
	if kill {
		_ = p.cmd.Process.Kill()
	}
	select {
	case <-p.waited:
	case <-time.After(2 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.waited
	}
}
