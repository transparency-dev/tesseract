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
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

type fakeDetector struct {
	attrs []attribute.KeyValue
}

func (f fakeDetector) Detect(context.Context) (*resource.Resource, error) {
	return resource.NewSchemaless(f.attrs...), nil
}

func TestDetectResources(t *testing.T) {
	for _, test := range []struct {
		desc                  string
		detectorAttrs         []attribute.KeyValue
		hostname              func() (string, error)
		wantNoServiceInstance bool
		wantServiceInstance   string
		wantUUIDInstance      bool
	}{
		{
			desc: "Cloud Run supplies faas.instance, service.instance.id not set",
			detectorAttrs: []attribute.KeyValue{
				semconv.FaaSInstanceKey.String("00bf4bf02da24966c43a39d0c6d3bd884d3b81c4"),
			},
			hostname:              func() (string, error) { return "localhost", nil },
			wantNoServiceInstance: true,
		},
		{
			desc: "GCE supplies host.id, service.instance.id not set",
			detectorAttrs: []attribute.KeyValue{
				semconv.HostIDKey.String("1234567890123456789"),
			},
			hostname:              func() (string, error) { return "gce-vm-1", nil },
			wantNoServiceInstance: true,
		},
		{
			desc:                  "fallback to non-localhost hostname when detector supplies no instance ID",
			hostname:              func() (string, error) { return "workstation-01", nil },
			wantServiceInstance:   "workstation-01",
		},
		{
			desc:             "fallback to UUID when hostname is localhost",
			hostname:         func() (string, error) { return "localhost", nil },
			wantUUIDInstance: true,
		},
		{
			desc:             "fallback to UUID when os.Hostname fails",
			hostname:         func() (string, error) { return "", errors.New("hostname error") },
			wantUUIDInstance: true,
		},
	} {
		t.Run(test.desc, func(t *testing.T) {
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
			res, err := detectResources(t.Context(), "example.com/log", "my-project", test.hostname, fakeDetector{attrs: test.detectorAttrs})
			if err != nil {
				t.Fatalf("detectResources() unexpected error: %v", err)
			}

			s := res.Set()
			val, hasServiceInstance := s.Value(semconv.ServiceInstanceIDKey)
			if test.wantNoServiceInstance {
				if hasServiceInstance {
					t.Errorf("service.instance.id = %q, want unset", val.AsString())
				}
				return
			}

			if !hasServiceInstance {
				t.Fatalf("service.instance.id not set on resource: %v", res)
			}
			got := val.AsString()
			if test.wantUUIDInstance {
				if _, err := uuid.Parse(got); err != nil {
					t.Errorf("service.instance.id = %q, want valid UUID: %v", got, err)
				}
			} else if got != test.wantServiceInstance {
				t.Errorf("service.instance.id = %q, want %q", got, test.wantServiceInstance)
			}
		})
	}
}
