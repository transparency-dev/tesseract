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

// Package logger provides structured logging utilities.
package logger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"cloud.google.com/go/logging"
	"go.opentelemetry.io/otel/trace"
)

// MultiHandler is a [Exporter] that invokes all the given Handlers.
// Its Enabled method reports whether any of the handlers' Enabled methods return true.
// Its Handle, WithAttrs and WithGroup methods call the corresponding method on each of the enabled handlers.
// Copied from slog.
// TODO: Move to slog.MultiHandler once the project has moved to go 1.26
type MultiHandler struct {
	multi []slog.Handler
}

// NewMultiHandler creates a [MultiHandler] with the given Handlers.
func NewMultiHandler(handlers ...slog.Handler) *MultiHandler {
	h := make([]slog.Handler, len(handlers))
	copy(h, handlers)
	return &MultiHandler{multi: h}
}

func (h *MultiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for i := range h.multi {
		if h.multi[i].Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for i := range h.multi {
		if h.multi[i].Enabled(ctx, r.Level) {
			if err := h.multi[i].Handle(ctx, r.Clone()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.multi))
	for i := range h.multi {
		handlers = append(handlers, h.multi[i].WithAttrs(attrs))
	}
	return &MultiHandler{multi: handlers}
}

func (h *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.multi))
	for i := range h.multi {
		handlers = append(handlers, h.multi[i].WithGroup(name))
	}
	return &MultiHandler{multi: handlers}
}

// Enricher injects GCP metadata in the record attributes.
type Enricher struct {
	next      slog.Handler
	projectID string
}

// NewEnricher wraps the provided slog.Handler. It injects GCP Cloud Logging
// compatible trace fields extracted from the context if a valid span is present.
func NewEnricher(next slog.Handler, projectID string) *Enricher {
	return &Enricher{next: next, projectID: projectID}
}

// Enabled reports whether the handler handles records at the given level.
// The handler ignores records whose level is lower.
func (h *Enricher) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

// Handle adds the trace ID, span ID, and sampled flag to the record attributes.
func (h *Enricher) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		// GCP Cloud Logging expects the trace ID to be formatted as:
		// projects/[PROJECT_ID]/traces/[TRACE_ID]
		// https://docs.cloud.google.com/logging/docs/structured-logging#structured_logging_special_fields
		tracePath := span.TraceID().String()
		if h.projectID != "" {
			tracePath = fmt.Sprintf("projects/%s/traces/%s", h.projectID, tracePath)
		}

		r.AddAttrs(
			slog.String("logging.googleapis.com/trace", tracePath),
			slog.String("logging.googleapis.com/spanId", span.SpanID().String()),
			slog.Bool("logging.googleapis.com/trace_sampled", span.IsSampled()),
		)
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes, preserving the GCP handling.
func (h *Enricher) WithAttrs(as []slog.Attr) slog.Handler {
	return &Enricher{h.next.WithAttrs(as), h.projectID}
}

// WithGroup returns a new handler with the given group name, preserving the GCP handling.
func (h *Enricher) WithGroup(g string) slog.Handler {
	return &Enricher{h.next.WithGroup(g), h.projectID}
}

// Exporter logs record to GCP Cloud Logging API.
type Exporter struct {
	logger *logging.Logger
	level  slog.Level
	goas   []groupOrAttrs
}

// NewExporter creates an slog.Handler that directly logs to GCP Cloud logging.
func NewExporter(logger *logging.Logger, level slog.Level) *Exporter {
	return &Exporter{logger: logger, level: level}
}

// Enabled reports whether the handler handles records at the given level.
func (h *Exporter) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.level.Level()
}

// Handle converts a record to a Cloud Logging entry, and logs it.
func (h *Exporter) Handle(ctx context.Context, r slog.Record) error {
	payload := make(map[string]any)
	payload["message"] = r.Message

	// Walk through groups and attributes.
	target := payload
	for _, goa := range h.goas {
		if goa.group != "" {
			// Open a new nesting level
			newGroup := make(map[string]any)
			target[goa.group] = newGroup
			target = newGroup
		} else {
			// Add attributes to the current level
			for _, a := range goa.attrs {
				target[a.Key] = a.Value.Any()
			}
		}
	}

	entry := logging.Entry{
		Timestamp: r.Time,
		Severity:  mapSeverity(r.Level),
		Payload:   payload,
	}

	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		// Capture traces from the specialized keys we added in the Enricher.
		case "logging.googleapis.com/trace":
			entry.Trace = a.Value.String()
		case "logging.googleapis.com/spanId":
			entry.SpanID = a.Value.String()
		case "logging.googleapis.com/trace_sampled":
			entry.TraceSampled = a.Value.Bool()
		// Skip adding level and time to the JSON body because they are already
		// in the entry.
		case slog.LevelKey, slog.TimeKey:
		default:
			target[a.Key] = a.Value.Any()
		}
		return true
	})

	h.logger.Log(entry)
	return nil
}

type groupOrAttrs struct {
	group string      // group name if non-empty
	attrs []slog.Attr // attrs if non-empty
}

// WithGroup implements Handler.WithGroup.
func (h *Exporter) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{group: name})
}

// WithAttrs implements Handler.WithAttrs.
func (h *Exporter) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.withGroupOrAttrs(groupOrAttrs{attrs: attrs})
}

func (h *Exporter) withGroupOrAttrs(goa groupOrAttrs) *Exporter {
	h2 := *h
	h2.goas = make([]groupOrAttrs, len(h.goas)+1)
	copy(h2.goas, h.goas)
	h2.goas[len(h2.goas)-1] = goa
	return &h2
}

// mapSeverity translates slog levels into GCP severity.
func mapSeverity(l slog.Level) logging.Severity {
	switch {
	case l >= slog.LevelError:
		return logging.Error
	case l >= slog.LevelWarn:
		return logging.Warning
	case l >= slog.LevelInfo:
		return logging.Info
	case l >= slog.LevelDebug:
		return logging.Debug
	default:
		return logging.Default
	}
}

// GCPReplaceAttr replaces slog attributes with attributes GCP understands.
func GCPReplaceAttr(groups []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.MessageKey:
		a.Key = "message"
	case slog.TimeKey:
		a.Key = "timestamp"
	case slog.SourceKey:
		a.Key = "logging.googleapis.com/sourceLocation"
	case slog.LevelKey:
		a.Key = "severity"
		level := a.Value.Any().(slog.Level)
		switch {
		case level >= slog.LevelError:
			a.Value = slog.StringValue("ERROR")
		case level >= slog.LevelWarn:
			a.Value = slog.StringValue("WARNING")
		case level >= slog.LevelInfo:
			a.Value = slog.StringValue("INFO")
		case level >= slog.LevelDebug:
			a.Value = slog.StringValue("DEBUG")
		default:
			a.Value = slog.StringValue("DEFAULT")
		}
	}
	return a
}
