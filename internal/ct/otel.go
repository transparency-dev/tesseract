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

package ct

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/klog/v2"
)

const name = "github.com/transparency-dev/tesseract/internal/ct"

var (
	meter  = otel.Meter(name)
	tracer = otel.Tracer(name)
)

var (
	codeKey                  = attribute.Key("http.response.status_code")
	operationKey             = attribute.Key("tesseract.operation")
	originKey                = attribute.Key("tesseract.origin")
	duplicateKey             = attribute.Key("tesseract.duplicate")
	tooManyRequestsReasonKey = attribute.Key("tesseract.too_many_requests")
	rateLimitReasonKey       = attribute.Key("tesseract.rate_limit")
)

func mustCreate[T any](t T, err error) T {
	if err != nil {
		klog.Exit(err.Error())
	}
	return t
}
