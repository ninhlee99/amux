package provider

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIdleStreamTransport_FailsSilentStream(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data: hi\n\n"))
		w.(http.Flusher).Flush()
		<-release // then go silent
	}))
	defer srv.Close()
	defer close(release)

	c := &http.Client{Transport: idleStreamTransport{base: http.DefaultTransport, idle: 150 * time.Millisecond}}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	start := time.Now()
	_, err = io.ReadAll(resp.Body)
	if !errors.Is(err, errStreamIdle) {
		t.Fatalf("want errStreamIdle, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("watchdog fired too late")
	}
}

func TestIdleStreamTransport_SlowButLiveStreamCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 5; i++ {
			w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			time.Sleep(80 * time.Millisecond)
		}
	}))
	defer srv.Close()
	c := &http.Client{Transport: idleStreamTransport{base: http.DefaultTransport, idle: 150 * time.Millisecond}}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil || string(b) != "xxxxx" {
		t.Fatalf("live stream must complete: %q %v", b, err)
	}
}
