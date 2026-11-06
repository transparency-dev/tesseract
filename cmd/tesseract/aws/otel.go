package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	t_otel "github.com/transparency-dev/tesseract/internal/otel"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func initOTel(ctx context.Context, traceFraction float64, origin string) func(context.Context) {
	var shutdownFuncs []func(context.Context) error
	shutdown := func(ctx context.Context) {
		var err error
		for _, shutdownFunc := range shutdownFuncs {
			err = errors.Join(err, shutdownFunc(ctx))
		}
		shutdownFuncs = nil
		if err != nil {
			slog.ErrorContext(ctx, "OTel shutdown", slog.Any("error", err))
		}
	}

	mr, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create the metric reader", slog.Any("error", err))
		os.Exit(1)
	}

	resources, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(origin),
			semconv.ServiceNamespaceKey.String("tesseract"),
		),
		resource.WithFromEnv(),
	)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to detect resources", slog.Any("error", err))
		os.Exit(1)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(mr),
		sdkmetric.WithResource(resources),
	)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)
	otel.SetMeterProvider(mp)

	te, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create the span exporter", slog.Any("error", err))
		os.Exit(1)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(t_otel.NewAttributeSampler([]string{"tessera.periodic"}, sdktrace.TraceIDRatioBased(traceFraction)))),
		sdktrace.WithBatcher(te),
		sdktrace.WithResource(resources),
	)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)
	otel.SetTracerProvider(tp)

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		slog.ErrorContext(ctx, "Failed to start exporting Go runtime metrics", slog.Any("error", err))
		os.Exit(1)
	}
	return shutdown
}
