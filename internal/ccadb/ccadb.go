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

package ccadb

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

var (
	ColSubject        = "Subject"
	ColIssuer         = "CA Owner"
	ColPEM            = "X.509 Certificate (PEM)"
	ColSHA            = "SHA-256 Fingerprint"
	ColUseCase        = "Intended Use Case(s) Served"
	UseCaseServerAuth = "Server Authentication (TLS) 1.3.6.1.5.5.7.3.1"
	KnownHeaders      = []string{ColSubject, ColIssuer, ColPEM, ColSHA, ColUseCase}
)

// Fetch retrieves a CCADB CSV and returns rows with a Use Case set to Server Authentication.
//
// It expects the CSV to have a header with at least the following columns:
//
//	Subject, CA Owner, X.509 Certificate (PEM), SHA-256 Fingerprint, Intended Use Case(s) Served
//
// Callers chose which columns are returned, and can request additional ones.
func Fetch(ctx context.Context, url string, fetchHeaders []string) ([][][]byte, error) {
	// 1. Fetch the CSV content from the URL
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("NewRequestWithContext(%q): %v", url, err)
	}
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET(%q): %v", url, err)
	}

	defer func() {
		if err := rsp.Body.Close(); err != nil {
			klog.Errorf("resp.Body.Close(): %v", err)
		}
	}()

	if rsp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-OK HTTP status: %s", rsp.Status)
	}

	// 2. Set up the CSV reader
	r := csv.NewReader(rsp.Body)
	// Set FieldsPerRecord to -1 to allow records to have a variable number of fields,
	// which is safer for complex CSVs.
	r.FieldsPerRecord = -1
	// The Go CSV parser correctly handles quoted fields with internal newlines by default.

	// 3. Verify that necessary headers are present
	headers, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("error reading header row: %v", err)
	}

	indices := make(map[string]int)
	for i, h := range headers {
		// Clean up the header name (trim potential whitespace or encoding artifacts)
		cleanName := strings.TrimSpace(h)
		indices[cleanName] = i
	}

	allHeadersMap := make(map[string]struct{})
	for _, h := range KnownHeaders {
		allHeadersMap[h] = struct{}{}
	}
	for _, h := range fetchHeaders {
		allHeadersMap[h] = struct{}{}
	}

	minNumColumns := 0
	for h := range allHeadersMap {
		i, found := indices[h]
		if !found {
			return nil, fmt.Errorf("required column %q not found in CSV headers %q", h, headers)
		}
		if i+1 > minNumColumns {
			minNumColumns = i + 1
		}
	}

	// 4. Process records
	rows := [][][]byte{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break // End of file
		}
		if err != nil {
			return nil, fmt.Errorf("malformed record: %v", err)
		}

		// Ensure the record is long enough before attempting to access fields
		if len(row) < minNumColumns {
			return nil, fmt.Errorf("row is too short: %q", row)
		}

		usecase := row[indices[ColUseCase]]
		if !strings.Contains(usecase, UseCaseServerAuth) {
			continue
		}

		elems := [][]byte{}
		for _, col := range fetchHeaders {
			elems = append(elems, []byte(row[indices[col]]))
		}
		rows = append(rows, elems)
	}

	return rows, nil
}
