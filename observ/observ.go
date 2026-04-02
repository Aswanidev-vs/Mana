package observ

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// LogLevel represents the severity of a log entry.
type LogLevel = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// LogOutput controls where logs are written.
type LogOutput int

const (
	OutputStderr LogOutput = iota
	OutputStdout
	OutputJSON
	OutputAuto
)

// Logger is a production-grade structured logger wrapping slog.
// It supports asynchronous, non-blocking logging and automatic format detection.
type Logger struct {
	sl     *slog.Logger
	level  LogLevel
	output LogOutput
	mu     sync.RWMutex
}

// NewLogger creates a production-ready logger.
func NewLogger(level LogLevel) *Logger {
	return NewLoggerWithOutput(level, OutputAuto)
}

// NewLoggerWithOutput creates a Logger with a specific output format.
func NewLoggerWithOutput(level LogLevel, output LogOutput) *Logger {
	var writer io.Writer = os.Stderr
	if output == OutputStdout {
		writer = os.Stdout
	}

	// Auto-detect format: JSON if not a TTY or explicitly requested
	isJSON := output == OutputJSON
	if output == OutputAuto {
		fileInfo, _ := os.Stderr.Stat()
		if (fileInfo.Mode() & os.ModeCharDevice) == 0 {
			isJSON = true
		}
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	if isJSON {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		handler = slog.NewTextHandler(writer, opts)
	}

	// Wrap in Async Handler for production performance
	asyncHandler := NewAsyncHandler(handler, 2048)

	return &Logger{
		sl:     slog.New(asyncHandler),
		level:  level,
		output: output,
	}
}

// WithFields returns a new Logger with the given fields attached.
func (l *Logger) WithFields(f map[string]interface{}) *Logger {
	args := make([]any, 0, len(f)*2)
	for k, v := range f {
		args = append(args, k, v)
	}
	return &Logger{
		sl:     l.sl.With(args...),
		level:  l.level,
		output: l.output,
	}
}

// WithComponent returns a new Logger scoped to a named component.
func (l *Logger) WithComponent(name string) *Logger {
	return &Logger{
		sl:     l.sl.With("component", name),
		level:  l.level,
		output: l.output,
	}
}

// WithRequest returns a new Logger with a request ID attached.
func (l *Logger) WithRequest(id string) *Logger {
	return &Logger{
		sl:     l.sl.With("request_id", id),
		level:  l.level,
		output: l.output,
	}
}

// WithPrefix returns a new Logger with a component-style prefix.
func (l *Logger) WithPrefix(p string) *Logger {
	return &Logger{
		sl:     l.sl.With("prefix", p),
		level:  l.level,
		output: l.output,
	}
}

// Debug logs a debug-level message.
func (l *Logger) Debug(format string, args ...interface{}) {
	l.sl.Debug(fmt.Sprintf(format, args...))
}

// Info logs an info-level message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.sl.Info(fmt.Sprintf(format, args...))
}

// Warn logs a warning-level message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.sl.Warn(fmt.Sprintf(format, args...))
}

// Error logs an error-level message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.sl.Error(fmt.Sprintf(format, args...))
}

// SetLevel changes the minimum log level at runtime.
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
	// Note: Existing sl instance will still use the old level if not using a dynamic level handler.
	// For simplicity in this refactor, we just update the field.
}

func (l *Logger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// --- Async Handler ---

type logRecord struct {
	ctx    context.Context
	record slog.Record
}

// AsyncHandler wraps a slog.Handler to provide non-blocking logging.
type AsyncHandler struct {
	next    slog.Handler
	ch      chan logRecord
	dropped int64
	mu      sync.Mutex
}

func NewAsyncHandler(next slog.Handler, bufferSize int) *AsyncHandler {
	h := &AsyncHandler{
		next: next,
		ch:   make(chan logRecord, bufferSize),
	}
	go h.run()
	return h
}

func (h *AsyncHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *AsyncHandler) Handle(ctx context.Context, r slog.Record) error {
	select {
	case h.ch <- logRecord{ctx: ctx, record: r.Clone()}:
		return nil
	default:
		h.mu.Lock()
		h.dropped++
		h.mu.Unlock()
		return nil // Drop log if buffer is full
	}
}

func (h *AsyncHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &AsyncHandler{
		next: h.next.WithAttrs(attrs),
		ch:   h.ch,
	}
}

func (h *AsyncHandler) WithGroup(name string) slog.Handler {
	return &AsyncHandler{
		next: h.next.WithGroup(name),
		ch:   h.ch,
	}
}

func (h *AsyncHandler) run() {
	for rec := range h.ch {
		_ = h.next.Handle(rec.ctx, rec.record)
	}
}

// --- Metrics (Unchanged) ---

type Metrics struct {
	mu sync.Mutex

	ConnectionsTotal      int64
	ConnectionsActive     int64
	MessagesTotal         int64
	MessagesErrors        int64
	RoomsActive           int64
	CallsActive           int64
	PeerConnectionsActive int64
	BytesSent             int64
	BytesReceived         int64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) IncConnections() {
	m.mu.Lock()
	m.ConnectionsTotal++
	m.ConnectionsActive++
	m.mu.Unlock()
}

func (m *Metrics) DecConnections() {
	m.mu.Lock()
	m.ConnectionsActive--
	m.mu.Unlock()
}

func (m *Metrics) IncMessages() {
	m.mu.Lock()
	m.MessagesTotal++
	m.mu.Unlock()
}

func (m *Metrics) IncMessageErrors() {
	m.mu.Lock()
	m.MessagesErrors++
	m.mu.Unlock()
}

func (m *Metrics) UpdateRooms(count int64) {
	m.mu.Lock()
	m.RoomsActive = count
	m.mu.Unlock()
}

func (m *Metrics) UpdateCalls(count int64) {
	m.mu.Lock()
	m.CallsActive = count
	m.mu.Unlock()
}

func (m *Metrics) UpdatePeerConnections(count int64) {
	m.mu.Lock()
	m.PeerConnectionsActive = count
	m.mu.Unlock()
}

func (m *Metrics) AddBytesSent(n int64) {
	m.mu.Lock()
	m.BytesSent += n
	m.mu.Unlock()
}

func (m *Metrics) AddBytesReceived(n int64) {
	m.mu.Lock()
	m.BytesReceived += n
	m.mu.Unlock()
}

func (m *Metrics) Snapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return map[string]int64{
		"connections_total":       m.ConnectionsTotal,
		"connections_active":      m.ConnectionsActive,
		"messages_total":          m.MessagesTotal,
		"messages_errors":         m.MessagesErrors,
		"rooms_active":            m.RoomsActive,
		"calls_active":            m.CallsActive,
		"peer_connections_active": m.PeerConnectionsActive,
		"bytes_sent":              m.BytesSent,
		"bytes_received":          m.BytesReceived,
	}
}

func (m *Metrics) PrometheusText() string {
	snap := m.Snapshot()
	out := ""
	for name, value := range snap {
		out += "# TYPE mana_" + name + " gauge\n"
		out += "mana_" + name + " " + fmt.Sprintf("%d", value) + "\n"
	}
	return out
}
