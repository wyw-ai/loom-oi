package servicecmd

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOptions(t *testing.T) {
	o, e := Parse("oic", []string{"--config", "test.yaml", "start", "--foreground"})
	if e != nil || o.Command != "start" || !o.Foreground {
		t.Fatalf("%+v %v", o, e)
	}
	for _, a := range [][]string{{"load"}, {"stop", "--foreground"}, {"start", "--unknown"}, {"--config"}, {"start", "extra"}} {
		if _, e := Parse("oic", a); e == nil {
			t.Fatalf("accepted invalid args %v", a)
		}
	}
	if _, e := Parse("ois", []string{"load", "app.yaml"}); e == nil {
		t.Fatal("OIS accepted App load")
	}
}

func TestConcurrentStaleLockRecovery(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "service.pid"), []byte("not-a-live-pid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int32
	var releaseLock func()
	var mu sync.Mutex
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			release, err := acquire(dir)
			if err == nil {
				winners.Add(1)
				mu.Lock()
				releaseLock = release
				mu.Unlock()
			}
		}()
	}
	close(start)
	group.Wait()
	if releaseLock != nil {
		releaseLock()
	}
	if winners.Load() != 1 {
		t.Fatalf("concurrent owners = %d", winners.Load())
	}
	release, err := acquire(dir)
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	release()
}

type testBackend struct{ closed atomic.Bool }

func (b *testBackend) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (b *testBackend) Close(context.Context) error   { b.closed.Store(true); return nil }
func (b *testBackend) Status() any                   { return map[string]bool{"ready": true} }
func (b *testBackend) Apps() (any, error)            { return []string{"sample"}, nil }
func (b *testBackend) Load(path string) (any, error) { return map[string]string{"path": path}, nil }
func (b *testBackend) Unload(string) error           { return nil }

func TestLocalControlLifecycle(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "oic.yaml")
	if e := os.WriteFile(config, []byte("version: 1\nstate_dir: state\n"), 0600); e != nil {
		t.Fatal(e)
	}
	state := filepath.Join(dir, "state")
	b := &testBackend{}
	done := make(chan error, 1)
	go func() {
		done <- Execute("oic", []string{"--config", config, "start", "--foreground"}, func(string) (Backend, error) { return b, nil }, io.Discard)
	}()
	var c control
	var e error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, e = readControl(state, config)
		if e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _, _ = request(c, "/stop", nil) })
	response, e := http.Get(c.URL + "/status")
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("unauthenticated local control returned %d", response.StatusCode)
	}
	if _, e = request(c, "/status", nil); e != nil {
		t.Fatal(e)
	}
	if _, e = request(c, "/load", map[string]string{"path": "app.yaml"}); e != nil {
		t.Fatal(e)
	}
	if _, e = request(c, "/stop", nil); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not finish")
	}
	if !b.closed.Load() {
		t.Fatal("backend resources not closed")
	}
	for _, name := range []string{"control.json", "service.pid"} {
		if _, e = os.Stat(filepath.Join(state, name)); !os.IsNotExist(e) {
			t.Fatalf("stale %s: %v", name, e)
		}
	}
}
