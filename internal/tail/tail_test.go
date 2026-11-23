package tail

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFollowRotationAndPartialLine(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(path, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	lines := make(chan string, 4)
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, path, func() { close(ready) }, func(line string) error { lines <- line; return nil })
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	appendLine := func(value string) {
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.WriteString(value); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(want string) {
		select {
		case value := <-lines:
			if value != want {
				t.Fatalf("got %q, want %q", value, want)
			}
		case err := <-done:
			t.Fatal(err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	appendLine("first")
	select {
	case line := <-lines:
		t.Fatalf("partial: %q", line)
	case <-time.After(300 * time.Millisecond):
	}
	appendLine(" line\n")
	expect("first line")
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after rotation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	expect("after rotation")
	cancel()
	<-done
}
