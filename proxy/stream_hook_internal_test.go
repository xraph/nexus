package proxy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/httpstream"
)

type hookLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *hookLog) add(msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, msg+" "+fmt.Sprint(args...))
}
func (l *hookLog) Debug(msg string, args ...any) { l.add(msg, args) }
func (l *hookLog) Info(msg string, args ...any)  { l.add(msg, args) }
func (l *hookLog) Warn(msg string, args ...any)  { l.add(msg, args) }
func (l *hookLog) Error(msg string, args ...any) { l.add(msg, args) }

func (l *hookLog) failures() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.HasPrefix(line, "request failed") {
			n++
		}
	}
	return n
}

func TestAClientThatLeavesIsNotLoggedAsAFailure(t *testing.T) {
	log := &hookLog{}
	gw := nexus.New(nexus.WithLogger(log))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := New(gw.Engine())
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	p.onStreamError(canceled, errors.New("stream closed"))
	p.onStreamError(context.Background(), fmt.Errorf("%w: write tcp: broken pipe", httpstream.ErrClientWrite))
	p.onStreamError(context.Background(), fmt.Errorf("write: %w", syscall.EPIPE))
	if n := log.failures(); n != 0 {
		t.Fatalf("logged %d failures for clients that left, want 0", n)
	}
	p.onStreamError(context.Background(), errors.New("upstream 502"))
	if n := log.failures(); n != 1 {
		t.Fatalf("logged %d failures for a provider error, want 1", n)
	}
}
