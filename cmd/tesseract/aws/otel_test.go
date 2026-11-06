package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestInitOTel(t *testing.T) {
	for _, scenario := range []string{"prometheus", "none", "sample-all", "invalid-exporter", "invalid-port", "occupied-port"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOTelProcess$")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "OTEL_") && !strings.HasPrefix(entry, "TESSERACT_OTEL_TEST=") {
					command.Env = append(command.Env, entry)
				}
			}
			command.Env = append(command.Env,
				"TESSERACT_OTEL_TEST="+scenario,
				"OTEL_METRICS_EXPORTER=none",
				"OTEL_TRACES_EXPORTER=none",
			)
			output, err := command.CombinedOutput()
			if strings.HasPrefix(scenario, "invalid-") || scenario == "occupied-port" {
				if err == nil || !strings.Contains(string(output), "Failed to create the metric reader") {
					t.Fatalf("expected metric reader startup failure, got %v: %s", err, output)
				}
				return
			}
			if err != nil {
				t.Fatalf("telemetry check failed: %v: %s", err, output)
			}
		})
	}
}

func TestOTelProcess(t *testing.T) {
	scenario := os.Getenv("TESSERACT_OTEL_TEST")
	if scenario == "" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var address string
	if scenario == "prometheus" || scenario == "occupied-port" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		address = listener.Addr().String()
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("OTEL_METRICS_EXPORTER", "prometheus")
		t.Setenv("OTEL_EXPORTER_PROMETHEUS_HOST", "127.0.0.1")
		t.Setenv("OTEL_EXPORTER_PROMETHEUS_PORT", port)
		if scenario == "occupied-port" {
			defer func() {
				if err := listener.Close(); err != nil {
					t.Error(err)
				}
			}()
		} else if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "invalid-exporter" {
		t.Setenv("OTEL_METRICS_EXPORTER", "invalid")
	}
	if scenario == "invalid-port" {
		t.Setenv("OTEL_METRICS_EXPORTER", "prometheus")
		t.Setenv("OTEL_EXPORTER_PROMETHEUS_PORT", "invalid-port")
	}
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=test")
	traceFraction := 0.0
	if scenario == "sample-all" {
		traceFraction = 1
	}
	shutdown := initOTel(ctx, traceFraction, "example.com/testlog")
	defer shutdown(ctx)

	tracer := otel.Tracer("aws-otel-test")
	_, ordinary := tracer.Start(ctx, "ordinary")
	if ordinary.SpanContext().IsSampled() != (traceFraction == 1) {
		t.Error("ordinary span sampling does not match trace_fraction")
	}
	ordinary.End()
	_, periodic := tracer.Start(ctx, "periodic", trace.WithAttributes(attribute.Bool("tessera.periodic", true)))
	if !periodic.SpanContext().IsSampled() {
		t.Error("periodic root span was not sampled")
	}
	periodic.End()

	if scenario != "prometheus" {
		return
	}
	counter, err := otel.Meter("aws-otel-test").Int64Counter("aws_otel_test_requests")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(ctx, 1)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("scrape status: %s", response.Status)
	}
	for _, expected := range []string{"aws_otel_test_requests_total", "go_goroutine", `service_name="example.com/testlog"`, `service_namespace="tesseract"`, `deployment_environment="test"`} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("scrape missing %q", expected)
		}
	}
	shutdown(ctx)
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err == nil {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
		t.Error("Prometheus listener still accepts connections after shutdown")
	}
}
