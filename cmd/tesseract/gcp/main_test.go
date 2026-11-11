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

package main

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
	return srv, "http://" + ln.Addr().String()
}

func TestGracefulShutdownOrder(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()

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
		if runCtx.Err() != nil {
			t.Errorf("runCtx cancelled before in-flight HTTP request finished: %v", runCtx.Err())
		}
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

	st := newGCPStorage(nil, nil)
	st.shutdownAppender = func(ctx context.Context) error {
		if ctx.Err() != nil {
			t.Errorf("appender shutdown received expired ctx: %v", ctx.Err())
		}
		if runCtx.Err() != nil {
			t.Errorf("runCtx cancelled before appender shutdown finished: %v", runCtx.Err())
		}
		record("appender")
		return nil
	}

	gracefulShutdown(time.Second, srv, st.shutdown, func() {
		record("cancel_run")
		cancelRun()
	})

	if err := <-reqDone; err != nil {
		t.Fatalf("in-flight HTTP request failed: %v", err)
	}
	if runCtx.Err() == nil {
		t.Fatal("expected runCtx to be cancelled")
	}
	want := []string{"http", "appender", "cancel_run"}
	if !slices.Equal(steps, want) {
		t.Errorf("gracefulShutdown steps = %v, want %v", steps, want)
	}
}

func TestGracefulShutdownHTTPTimeoutReservesAppenderBudget(t *testing.T) {
	runCtx, cancelRun := context.WithCancel(t.Context())
	defer cancelRun()

	var (
		mu          sync.Mutex
		steps       []string
		inHandler   = make(chan struct{})
		handlerDone = make(chan struct{})
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
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-inHandler

	st := newGCPStorage(nil, nil)
	st.shutdownAppender = func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			t.Errorf("appender shutdown received already-expired ctx after HTTP timeout: %v", err)
		}
		if err := runCtx.Err(); err != nil {
			t.Errorf("runCtx cancelled before appender shutdown: %v", err)
		}
		<-handlerDone
		<-ctx.Done()
		record("appender_timeout")
		return ctx.Err()
	}

	gracefulShutdown(80*time.Millisecond, srv, st.shutdown, func() {
		record("cancel_run")
		cancelRun()
	})

	if runCtx.Err() == nil {
		t.Fatal("expected runCtx to be cancelled after shutdown timeout")
	}
	want := []string{"http_closed", "appender_timeout", "cancel_run"}
	if !slices.Equal(steps, want) {
		t.Errorf("gracefulShutdown steps = %v, want %v", steps, want)
	}
}

func TestGCPStorageShutdownBeforeCreate(t *testing.T) {
	st := newGCPStorage(nil, nil)
	if err := st.shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown before create = %v, want nil", err)
	}
	if _, err := st.create(t.Context(), nil); err == nil {
		t.Fatal("create with empty flags succeeded, want error")
	}
	if st.shutdownAppender != nil {
		t.Fatal("shutdownAppender set after failed create")
	}
}
