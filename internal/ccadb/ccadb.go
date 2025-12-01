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
	ColUsecase        = "Intended Use Case(s) Served"
	UsecaseServerAuth = "Server Authentication (TLS) 1.3.6.1.5.5.7.3.1"
	KnownHeaders      = []string{ColSubject, ColIssuer, ColPEM, ColSHA, ColUsecase}
)

func Fetch(url string, fetchHeaders []string) ([][][]byte, error) {
	// 1. Fetch the CSV content from the URL
	client := http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching URL: %v", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			klog.Errorf("resp.Body.Close(): %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-OK HTTP status: %s", resp.Status)
	}

	// 2. Set up the CSV reader
	r := csv.NewReader(resp.Body)
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

		usecase := row[indices[ColUsecase]]
		if !strings.Contains(usecase, UsecaseServerAuth) {
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
