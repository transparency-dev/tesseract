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

// Package lifecycle contains lifecycle helpers shared by TesseraCT binaries.
package lifecycle

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Shutdown drains in-flight HTTP requests, then the log, within timeout.
// The HTTP drain gets at most half of timeout.
func Shutdown(timeout time.Duration, srv *http.Server, shutdownLog func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	httpCtx, cancelHTTP := context.WithTimeout(ctx, timeout/2)
	defer cancelHTTP()

	slog.InfoContext(ctx, "Shutting down HTTP server...")
	if err := srv.Shutdown(httpCtx); err != nil {
		slog.ErrorContext(ctx, "srv.Shutdown()", slog.Any("error", err))
		if closeErr := srv.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "srv.Close()", slog.Any("error", closeErr))
		}
	}
	slog.InfoContext(ctx, "HTTP server shutdown")

	slog.InfoContext(ctx, "Shutting down log...")
	if err := shutdownLog(ctx); err != nil {
		slog.ErrorContext(ctx, "log shutdown", slog.Any("error", err))
	}
	slog.InfoContext(ctx, "Log shutdown")
}
