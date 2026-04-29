// Copyright 2025 The Tessera authors. All Rights Reserved.
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
	"errors"
	"log/slog"
	"os"

	t_otel "github.com/transparency-dev/tesseract/internal/otel"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	mexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"

	"github.com/google/uuid"
)

// initOTel initialises the open telemetry support for metrics and tracing.
//
// Tracing is enabled with statistical sampling, with the probability passed in.
// Returns a shutdown function which should be called just before exiting the process.
func initOTel(ctx context.Context, traceFraction float64, origin string, projectID string) func(context.Context) {
	var shutdownFuncs []func(context.Context) error
	// shutdown combines shutdown functions from multiple OpenTelemetry
	// components into a single function.
	shutdown := func(ctx context.Context) {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		if err != nil {
			slog.ErrorContext(ctx, "OTel shutdown", slog.Any("error", err))
		}
	}

	instanceID, err := os.Hostname()
	if err != nil {
		slog.ErrorContext(ctx, "os.Hostname() failed, setting OTel service instance ID to UUID", slog.Any("error", err))
		instanceID = uuid.NewString()
	}
	resources, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		// Add your own custom attributes to identify your application
		resource.WithAttributes(
			semconv.ServiceNamespaceKey.String("tesseract"),
			semconv.ServiceNameKey.String(origin),
			semconv.ServiceInstanceIDKey.String(instanceID),
		),
		resource.WithFromEnv(), // unpacks OTEL_RESOURCE_ATTRIBUTES
		resource.WithDetectors(gcp.NewDetector()),
	)
	if err != nil {
		fatal(ctx, "Failed to detect resources", slog.Any("error", err))
	}

	mopts := []mexporter.Option{}
	if projectID != "" {
		mopts = append(mopts, mexporter.WithProjectID(projectID))
	}
	me, err := mexporter.New(mopts...)
	if err != nil {
		fatal(ctx, "Failed to create metric exporter", slog.Any("error", err))
		return nil
	}
	// initialize a MeterProvider that periodically exports to the GCP exporter.
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me)),
		sdkmetric.WithResource(resources),
	)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)
	otel.SetMeterProvider(mp)

	topts := []texporter.Option{}
	if projectID != "" {
		topts = append(topts, texporter.WithProjectID(projectID))
	}
	te, err := texporter.New(topts...)
	if err != nil {
		fatal(ctx, "Failed to create trace exporter", slog.Any("error", err))
		return nil
	}
	// initialize a TracerProvier that periodically exports to the GCP exporter.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(t_otel.NewAttributeSampler([]string{"tessera.periodic"}, sdktrace.TraceIDRatioBased(traceFraction)))),
		sdktrace.WithBatcher(te),
		sdktrace.WithResource(resources),
	)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)
	otel.SetTracerProvider(tp)

	// 	https://github.com/open-telemetry/opentelemetry-go-contrib

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		fatal(ctx, "Failed to start exporting Go runtime metrics", slog.Any("error", err))
	}
	return shutdown
}
