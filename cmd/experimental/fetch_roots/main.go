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

// fetch_roots is a command-line tool for fetching PEM roots that production CT
// logs should trust from the Common CA Database (CCADB).
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

var (
	url            = flag.String("url", "https://ccadb.my.salesforce-sites.com/ccadb/RootCACertificatesIncludedByRSReportCSV", "URL to fetch the CSV from.")
	outputFilename = flag.String("output_filename", "roots.pem", "Path of the output file.")
)

var (
	colSubject        = "Subject"
	colIssuer         = "CA Owner"
	colPEM            = "X.509 Certificate (PEM)"
	colSHA            = "SHA-256 Fingerprint"
	colUsecase        = "Intended Use Case(s) Served"
	usecaseServerAuth = "Server Authentication (TLS) 1.3.6.1.5.5.7.3.1"
	dirPerm           = 0o755
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	// 1. Fetch the CSV content from the URL
	client := http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Get(*url)
	if err != nil {
		klog.Exitf("Error fetching URL: %v", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			klog.Errorf("resp.Body.Close(): %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		klog.Exitf("Received non-OK HTTP status: %s", resp.Status)
	}

	// 2. Set up the CSV reader
	r := csv.NewReader(resp.Body)
	// Set FieldsPerRecord to -1 to allow records to have a variable number of fields,
	// which is safer for complex CSVs.
	r.FieldsPerRecord = -1
	// The Go CSV parser correctly handles quoted fields with internal newlines by default.

	// 3. Read the header row
	header, err := r.Read()
	if err != nil {
		klog.Exitf("Error reading header row: %v", err)
	}

	// 4. Dynamically find the required column indices
	indices := make(map[string]int)
	requiredHeaders := []string{colSubject, colIssuer, colPEM, colSHA, colUsecase}

	for i, colName := range header {
		// Clean up the header name (trim potential whitespace or encoding artifacts)
		cleanName := strings.TrimSpace(colName)
		indices[cleanName] = i
	}

	minNumColumns := 0
	// Verify all required headers were found
	for _, req := range requiredHeaders {
		i, found := indices[req]
		if !found {
			klog.Exitf("Required column not found in CSV header: %s", req)
		}
		if i+1 > minNumColumns {
			minNumColumns = i + 1
		}
	}

	// 5. Set up the output file
	outFile, err := createFile(*outputFilename)
	if err != nil {
		klog.Exitf("Error creating output file: %v", err)
	}

	defer func() {
		if err := outFile.Close(); err != nil {
			klog.Errorf("Error closing %q: %v", outFile.Name(), err)
		}
	}()

	// 6. Process the remaining records
	for {
		row, err := r.Read()
		if err == io.EOF {
			break // End of file
		}
		if err != nil {
			klog.Exitf("Malformed record: %v", err)
		}

		// Ensure the record is long enough before attempting to access fields
		if len(row) < minNumColumns {
			klog.Exitf("Row is too short: %q", row)
		}

		usecase := row[indices[colUsecase]]
		if !strings.Contains(usecase, usecaseServerAuth) {
			continue
		}

		issuer := row[indices[colIssuer]]
		subject := row[indices[colSubject]]
		sha256 := row[indices[colSHA]]
		cert := row[indices[colPEM]]

		// Format and write the metadata (prefixed by #) and the certificate
		output := fmt.Sprintf("# Issuer: %s\n# Subject: %s\n# SHA256 Fingerprint: %s\n%s\n",
			issuer, subject, sha256, cert)

		if _, err := outFile.WriteString(output); err != nil {
			klog.Exitf("Error writing to output file: %v", err)
		}
	}

	klog.Infof("Successfully extracted certificates to %s", *outputFilename)
}

// createFile creates a file at path p, and creates necessary parent directories.
func createFile(p string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(p), os.FileMode(dirPerm)); err != nil {
		return nil, fmt.Errorf("os.MkdirAll: %v", err)
	}
	return os.Create(p)
}
