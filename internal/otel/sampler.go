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

package otel

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// AttributeSampler is intended to be used to help sample traces which include ones from Tessera.
type AttributeSampler struct {
	always   map[string]struct{}
	fallback sdktrace.Sampler
}

func NewAttributeSampler(always []string, fallback sdktrace.Sampler) *AttributeSampler {
	m := make(map[string]struct{}, len(always))
	for _, s := range always {
		m[s] = struct{}{}
	}
	return &AttributeSampler{
		always:   m,
		fallback: fallback,
	}
}

func (s *AttributeSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	// Always extract the parent span context to get the tracestate.
	psc := trace.SpanContextFromContext(p.ParentContext)

	// Always sample children of sampled parents.
	if psc.IsSampled() {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Attributes: p.Attributes,
			// Critical: preserve the parent's tracestate
			Tracestate: psc.TraceState(),
		}
	}

	// Check for "always" attributes and sample if one is present:
	for _, attr := range p.Attributes {
		if _, found := s.always[string(attr.Key)]; found {
			return sdktrace.SamplingResult{
				Decision:   sdktrace.RecordAndSample,
				Attributes: p.Attributes,
				// Critical: preserve the parent's tracestate
				Tracestate: psc.TraceState(),
			}
		}
	}

	// Otherwise, fallback to the default sampler for other spans.
	result := s.fallback.ShouldSample(p)

	// Ensure tracestate is preserved even when using fallback.
	return sdktrace.SamplingResult{
		Decision:   result.Decision,
		Attributes: result.Attributes,
		Tracestate: psc.TraceState(),
	}
}

func (s *AttributeSampler) Description() string {
	return "AttributeSampler"
}
