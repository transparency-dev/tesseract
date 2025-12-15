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
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/transparency-dev/tesseract/internal/ccadb"
	"k8s.io/klog/v2"
)

var (
	url            = flag.String("url", "https://ccadb.my.salesforce-sites.com/ccadb/RootCACertificatesIncludedByRSReportCSV", "URL to fetch the CSV from.")
	outputFilename = flag.String("output_filename", "roots.pem", "Path of the output file.")
)

var (
	dirPerm    = 0o755
	allColumns = []string{ccadb.ColIssuer, ccadb.ColSubject, ccadb.ColSHA, ccadb.ColPEM}
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	roots, err := ccadb.Fetch(context.Background(), *url, allColumns)
	if err != nil {
		klog.Exitf("Error fetching roots: %s", err)
	}

	outFile, err := createFile(*outputFilename)
	if err != nil {
		klog.Exitf("Error creating output file: %v", err)
	}

	defer func() {
		if err := outFile.Close(); err != nil {
			klog.Errorf("Error closing %q: %v", outFile.Name(), err)
		}
	}()

	for _, row := range roots {
		if len(row) != len(allColumns) {
			klog.Errorf("Unexpected number of columns in row: got %d, want %d", len(row), len(allColumns))
		}

		sha256 := formatBase64(string(row[2]), ":", 2)
		// Format and write the metadata (prefixed by #) and the certificate
		output := fmt.Sprintf("# Issuer: %s\n# Subject: %s\n# SHA256 Fingerprint: %s\n%s\n",
			row[0], row[1], sha256, row[3])

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

// formatBase64 adds a separator string every X characters of an input string.
// For instance, with ":", and 2: DEADBEEF --> DE:AD:BE:EF
func formatBase64(input string, separator string, chunkSize int) string {
	var parts []string
	for i := 0; i < len(input); i += chunkSize {
		end := i + chunkSize
		if end > len(input) {
			end = len(input)
		}
		parts = append(parts, input[i:end])
	}
	return strings.Join(parts, separator)
}
