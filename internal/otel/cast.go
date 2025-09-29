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

package otel

import (
	"math"
	"time"
)

var (
	// SubSecondLatencyHistogramBuckets is a range of millisecond scale bucket boundaries which remain useful at around 1-2 seconds timescale in addition to smaller latencies.
	// We use seconds as units, Open Telemetry's default unit for time.
	SubSecondLatencyHistogramBuckets = createBuckets(
		[]float64{0, 10, 50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1200, 1400, 1600, 1800, 2000, 2500, 3000, 4000, 5000, 6000, 8000, 10000},
		time.Millisecond,
		time.Second)

	// SubmissionAgeHistogramBuckets is an exponential range of buckets defined in seconds and intended to cover some reasonable ages of submissions.
	// This range is based on the default OTel buckets, but ranges from ~5s at the low end up to ~70days.
	SubmissionAgeHistogramBuckets = createBuckets(
		[]float64{0, 0.1, 0.25, 0.5, 0.75, 1, 2, 5, 7.5, 10, 25, 50, 75, 100, 250, 500, 750, 1000, 2500, 5000, 7500, 10000, 25000, 50000, 75000, 100000},
		time.Minute,
		time.Second)
)

// createBuckets is a helper for creating histogram buckets in a readable fashion by taking a list of values and the duration unit they represent, and returns
// a slice of floats representing these durations scaled to the output duration.
func createBuckets(n []float64, d time.Duration, output time.Duration) []float64 {
	scale := float64(d) / float64(output)
	r := make([]float64, 0, len(n))
	for _, f := range n {
		r = append(r, f*scale)
	}
	return r
}

// Clamp64 casts a uint64 to an int64, clamping it at MaxInt64 if the value is above.
//
// Intended only for converting Tessera uint64 internal values to int64 for use with
// open telemetry metrics.
func Clamp64(u uint64) int64 {
	if u > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(u)
}
