// Copyright 2026 The Tessera authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package lifecycle

import (
	"context"
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"
)

func startTestServer(t *testing.T, h http.Handler) (*http.Server, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return srv, "http://" + ln.Addr().String()
}

func TestShutdownOrder(t *testing.T) {
	var (
		mu              sync.Mutex
		steps           []string
		inHandler       = make(chan struct{})
		shutdownStarted = make(chan struct{})
		reqDone         = make(chan error, 1)
	)
	record := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		steps = append(steps, step)
	}

	srv, url := startTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(inHandler)
		<-shutdownStarted
		record("http")
		w.WriteHeader(http.StatusOK)
	}))
	srv.RegisterOnShutdown(func() { close(shutdownStarted) })

	go func() {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
		reqDone <- err
	}()
	<-inHandler

	Shutdown(time.Second, srv, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			t.Errorf("log shutdown received expired ctx: %v", err)
		}
		record("log")
		return nil
	})

	if err := <-reqDone; err != nil {
		t.Fatalf("in-flight HTTP request failed: %v", err)
	}
	want := []string{"http", "log"}
	if !slices.Equal(steps, want) {
		t.Errorf("Shutdown() steps = %v, want %v", steps, want)
	}
}

func TestShutdownHTTPTimeoutReservesLogBudget(t *testing.T) {
	var (
		mu          sync.Mutex
		steps       []string
		inHandler   = make(chan struct{})
		handlerDone = make(chan struct{})
		reqDone     = make(chan struct{})
	)
	record := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		steps = append(steps, step)
	}

	srv, url := startTestServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		close(inHandler)
		<-r.Context().Done()
		record("http_closed")
	}))

	go func() {
		defer close(reqDone)
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-inHandler

	Shutdown(400*time.Millisecond, srv, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			t.Errorf("log shutdown received already-expired ctx after HTTP timeout: %v", err)
		}
		<-handlerDone
		<-ctx.Done()
		record("log_timeout")
		return ctx.Err()
	})
	<-reqDone

	want := []string{"http_closed", "log_timeout"}
	if !slices.Equal(steps, want) {
		t.Errorf("Shutdown() steps = %v, want %v", steps, want)
	}
}
