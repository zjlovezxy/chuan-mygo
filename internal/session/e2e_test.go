//go:build !js

package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zjlovezxy/chuan-mygo/internal/protocol"
	"github.com/zjlovezxy/chuan-mygo/internal/server"
	"github.com/zjlovezxy/chuan-mygo/internal/transfer"
)

// memorySource is a small in-memory file that transfer reads in chunks.
type memorySource struct {
	name string
	data []byte
}

func (s *memorySource) Name() string { return s.name }
func (s *memorySource) Size() int64  { return int64(len(s.data)) }
func (s *memorySource) Type() string { return "application/octet-stream" }
func (s *memorySource) ReadAt(offset int64, length int) ([]byte, error) {
	end := offset + int64(length)
	if end > int64(len(s.data)) {
		end = int64(len(s.data))
	}
	if offset >= end {
		return nil, nil
	}
	return s.data[offset:end], nil
}
func (s *memorySource) Close() error { return nil }

// TestEndToEndTransfer builds a room, connects two sessions over loopback
// WebRTC through the signalling server, and moves a file larger than one chunk.
func TestEndToEndTransfer(t *testing.T) {
	srv := server.New(server.Options{})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	code, err := srv.NewRoom()
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	dir := t.TempDir()
	payload := make([]byte, protocol.ChunkSize*2+1234)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("payload: %v", err)
	}

	connected := make(chan string, 4)
	finished := make(chan string, 1)

	// The receiver's transfer manager is created after the session, so the
	// session's event closures read it through an atomic pointer.
	var receiverMgr atomic.Pointer[transfer.Manager]

	// pion keeps gathering after Close; a late callback must not log through
	// testing.T once the test is done, or the runtime panics.
	var stopped atomic.Bool
	t.Cleanup(func() { stopped.Store(true) })
	logf := func(format string, args ...any) {
		if stopped.Load() {
			return
		}
		t.Logf(format, args...)
	}

	stateEvents := func(label string) Events {
		return Events{
			State: func(state State, detail string) {
				if state == StateConnected {
					select {
					case connected <- label:
					default:
					}
					return
				}
				if state == StateFailed {
					logf("[%s] failed: %s", label, detail)
				}
			},
			Log:   func(s string) { logf("[%s] %s", label, s) },
			Error: func(err error) { logf("[%s] error: %v", label, err) },
		}
	}

	recvEvents := stateEvents("receiver")
	recvEvents.Message = func(msg protocol.Message) {
		if mgr := receiverMgr.Load(); mgr != nil {
			mgr.HandleMessage(msg)
		}
	}
	recvEvents.Binary = func(data []byte) {
		if mgr := receiverMgr.Load(); mgr != nil {
			mgr.HandleBinary(data)
		}
	}

	sender := New(Config{Server: ts.URL, Code: code, Role: protocol.RoleSender, Events: stateEvents("sender")})
	receiver := New(Config{Server: ts.URL, Code: code, Role: protocol.RoleReceiver, Events: recvEvents})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := sender.Start(ctx); err != nil {
		t.Fatalf("start sender: %v", err)
	}
	defer sender.Close()
	if err := receiver.Start(ctx); err != nil {
		t.Fatalf("start receiver: %v", err)
	}
	defer receiver.Close()

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case label := <-connected:
			seen[label] = true
		case <-ctx.Done():
			t.Fatalf("data channel never opened for %v", missing(seen))
		}
	}

	receiverMgr.Store(transfer.New(receiver, transfer.NewDiskSink(dir), transfer.Events{
		Finished: func(_ protocol.FileMetadata, path string) {
			select {
			case finished <- path:
			default:
			}
		},
		Log:   func(s string) { t.Logf("[receiver] %s", s) },
		Error: func(err error) { t.Logf("[receiver] transfer error: %v", err) },
	}))

	senderMgr := transfer.New(sender, transfer.NewDiskSink(t.TempDir()), transfer.Events{
		Log:   func(s string) { t.Logf("[sender] %s", s) },
		Error: func(err error) { t.Logf("[sender] transfer error: %v", err) },
	})

	const name = "报告 2026.bin"
	if err := senderMgr.Send(ctx, []transfer.Source{&memorySource{name: name, data: payload}}); err != nil {
		t.Fatalf("send: %v", err)
	}

	var path string
	select {
	case path = <-finished:
	case <-time.After(45 * time.Second):
		t.Fatal("receiver never finished the file")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("received %d bytes, want %d, contents differ", len(got), len(payload))
	}
	if base := filepath.Base(path); base != name {
		t.Fatalf("saved as %q, want %q", base, name)
	}
}

func missing(seen map[string]bool) []string {
	var out []string
	for _, label := range []string{"sender", "receiver"} {
		if !seen[label] {
			out = append(out, label)
		}
	}
	return out
}
