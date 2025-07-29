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

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"

	"k8s.io/klog/v2"
)

// initOTel initialises the open telemetry support for metrics.
//
// Returns a shutdown function which should be called just before exiting the process.
func initOTel(ctx context.Context, origin string) func(context.Context) {
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
			klog.Errorf("OTel shutdown: %v", err)
		}
	}

	me, err := otlpmetrichttp.New(ctx)
	if err != nil {
		klog.Exitf("Failed to create the OTLP metric exporter: %v", err)
	}

	resources, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(), // unpacks OTEL_RESOURCE_ATTRIBUTES
		resource.WithAttributes(
			semconv.ServiceNameKey.String(origin),
			semconv.ServiceNamespaceKey.String("tesseract"),
		),
	)
	if err != nil {
		klog.Exitf("Failed to detect resources: %v", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me)),
		sdkmetric.WithResource(resources),
	)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)
	otel.SetMeterProvider(mp)

	return shutdown
}
