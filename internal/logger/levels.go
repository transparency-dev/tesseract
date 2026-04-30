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

package logger

import (
	"context"
	"log/slog"
)

const (
	LevelDebugExtra = slog.Level(-8)
	LevelExtreme    = slog.Level(-12)
)

func DebugExtraContext(ctx context.Context, msg string, args ...any) {
	slog.Default().Log(ctx, LevelDebugExtra, msg, args...)
}

func ExtremeContext(ctx context.Context, msg string, args ...any) {
	slog.Default().Log(ctx, LevelExtreme, msg, args...)
}
