package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const diagnosticInterval = 15 * time.Second
const diagnosticMaxBytes = 1 << 20

type diagnosticState struct {
	mu       sync.Mutex
	failures map[string]int
	lastLog  map[string]time.Time
	since    time.Time
}

type requestTiming struct {
	mu                                        sync.Mutex
	started, dnsStart, connectStart, tlsStart time.Time
	dns, connect, handshake, firstByte        time.Duration
	reused                                    bool
	phase                                     string
}

// Trace only existing requests while an incident is active. No extra probes,
// retries, or changes to the request's transport and timeout are introduced.
func (a *agent) diagnoseRequest(req *http.Request, channel string) func(int, error) {
	d := &a.diagnostics
	d.mu.Lock()
	active := !d.since.IsZero()
	d.mu.Unlock()
	t := &requestTiming{started: time.Now(), phase: "request"}
	if active {
		trace := &httptrace.ClientTrace{
			DNSStart: func(httptrace.DNSStartInfo) {
				t.mu.Lock()
				t.dnsStart = time.Now()
				t.phase = "dns"
				t.mu.Unlock()
			},
			DNSDone: func(httptrace.DNSDoneInfo) {
				t.mu.Lock()
				t.dns = time.Since(t.dnsStart)
				t.phase = "connect"
				t.mu.Unlock()
			},
			ConnectStart: func(string, string) {
				t.mu.Lock()
				t.connectStart = time.Now()
				t.phase = "connect"
				t.mu.Unlock()
			},
			ConnectDone: func(string, string, error) {
				t.mu.Lock()
				t.connect = time.Since(t.connectStart)
				t.mu.Unlock()
			},
			TLSHandshakeStart: func() {
				t.mu.Lock()
				t.tlsStart = time.Now()
				t.phase = "tls"
				t.mu.Unlock()
			},
			TLSHandshakeDone: func(tls.ConnectionState, error) {
				t.mu.Lock()
				t.handshake = time.Since(t.tlsStart)
				t.phase = "response"
				t.mu.Unlock()
			},
			GotConn: func(info httptrace.GotConnInfo) {
				t.mu.Lock()
				t.reused = info.Reused
				t.phase = "response"
				t.mu.Unlock()
			},
			GotFirstResponseByte: func() {
				t.mu.Lock()
				t.firstByte = time.Since(t.started)
				t.phase = "body"
				t.mu.Unlock()
			},
		}
		*req = *req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	}
	return func(status int, err error) {
		now := time.Now()
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.failures == nil {
			d.failures = make(map[string]int)
			d.lastLog = make(map[string]time.Time)
		}
		previouslyFailed := d.failures[channel] > 0
		failed := err != nil || status >= 400
		if failed {
			d.failures[channel]++
		} else {
			delete(d.failures, channel)
		}
		if d.since.IsZero() {
			if d.failures[channel] < 2 {
				return
			}
			d.since = now
			a.writeDiagnostic(fmt.Sprintf("incident started channel=%s", channel))
		}
		if (!failed && previouslyFailed) || (failed && now.Sub(d.lastLog[channel]) >= diagnosticInterval) {
			t.mu.Lock()
			line := fmt.Sprintf("channel=%s status=%d error=%s phase=%s total_ms=%d dns_ms=%d connect_ms=%d tls_ms=%d first_byte_ms=%d reused=%t traced=%t", channel, status, diagnosticError(err), t.phase, now.Sub(t.started).Milliseconds(), t.dns.Milliseconds(), t.connect.Milliseconds(), t.handshake.Milliseconds(), t.firstByte.Milliseconds(), t.reused, active)
			t.mu.Unlock()
			// Log recovery once per channel while other channels may still be failing.
			if failed || previouslyFailed {
				a.writeDiagnostic(line)
				d.lastLog[channel] = now
			}
			if !failed {
				delete(d.lastLog, channel)
			}
		}
		if len(d.failures) == 0 {
			a.writeDiagnostic(fmt.Sprintf("incident recovered duration_ms=%d", now.Sub(d.since).Milliseconds()))
			d.since = time.Time{}
			clear(d.lastLog)
		}
	}
}

func diagnosticError(err error) string {
	if err == nil {
		return "none"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "transport"
}

// Keep two bounded files, containing only timings and safe error categories.
func (a *agent) writeDiagnostic(message string) {
	if os.MkdirAll(logDir, 0755) != nil {
		return
	}
	path := filepath.Join(logDir, "diagnostics.log")
	if info, err := os.Stat(path); err == nil && info.Size() >= diagnosticMaxBytes {
		if os.Rename(path, path+".1") != nil {
			return
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339Nano), message)
}
