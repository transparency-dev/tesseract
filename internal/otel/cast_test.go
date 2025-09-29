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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestCreateBuckets(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []float64
		inUnit time.Duration
		toUnit time.Duration
		want   []float64
	}{
		{
			name:   "small units to large units",
			values: []float64{2e6, 4e6, 5e6, 1e6},
			inUnit: time.Microsecond,
			toUnit: time.Second,
			want:   []float64{2, 4, 5, 1},
		}, {
			name:   "large units to small units",
			values: []float64{1, 2, 3},
			inUnit: time.Hour,
			toUnit: time.Second,
			want:   []float64{1 * 3600, 2 * 3600, 3 * 3600},
		}, {
			name:   "idential units",
			values: []float64{1, 2, 3},
			inUnit: time.Second,
			toUnit: time.Second,
			want:   []float64{1, 2, 3},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			goodEnough := cmpopts.EquateApprox(0, 0.0001)
			if got := createBuckets(test.values, test.inUnit, test.toUnit); !cmp.Equal(got, test.want, goodEnough) {
				t.Fatalf("Got %v, want %v", got, test.want)
			}
		})
	}

}
