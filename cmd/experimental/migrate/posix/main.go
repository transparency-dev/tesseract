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

// migrate-posix is a command-line tool for migrating data from a static-ct
// compliant log, into a TesseraCT log instance using POSIX storage.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/transparency-dev/tessera"
	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tessera/client"
	"github.com/transparency-dev/tessera/storage/posix"
	tposix_as "github.com/transparency-dev/tessera/storage/posix/antispam"
	"k8s.io/klog/v2"
)

var (
	storageDir         = flag.String("storage_dir", "", "Path to directory in which to store the migrated data.")
	sourceURL          = flag.String("source_url", "", "Base monitoring URL for the source log.")
	numWorkers         = flag.Uint("num_workers", 30, "Number of migration worker goroutines.")
	persistentAntispam = flag.Bool("antispam", true, "Set to true to populate antispam storage.")
	antispamBatchSize  = flag.Uint("antispam_batch_size", 50000, "Maximum number of antispam rows to insert per batch update.")
	clientHTTPTimeout  = flag.Duration("client_http_timeout", 30*time.Second, "Timeout for outgoing HTTP requests")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()

	if *storageDir == "" {
		klog.Exit("--storage_dir must be set")
	}

	srcURL, err := url.Parse(*sourceURL)
	if err != nil {
		klog.Exitf("Invalid --source_url %q: %v", *sourceURL, err)
	}
	hc := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        int(*numWorkers) * 2,
			MaxIdleConnsPerHost: int(*numWorkers),
			DisableKeepAlives:   false,
		},
		Timeout: *clientHTTPTimeout,
	}
	// TODO(phbnf): This is currently built using the Tessera client lib, with a stand-alone func below for
	// fetching the Static CT entry bundles as they live in an different place.
	// When there's a Static CT client we can probably switch over to using it in here.
	src, err := client.NewHTTPFetcher(srcURL, hc)
	if err != nil {
		klog.Exitf("Failed to create HTTP fetcher: %v", err)
	}
	sourceCP, err := src.ReadCheckpoint(ctx)
	if err != nil {
		klog.Exitf("fetch initial source checkpoint: %v", err)
	}
	// TODO(AlCutter): We should be properly verifying and opening the checkpoint here with the source log's
	// public key.
	bits := strings.Split(string(sourceCP), "\n")
	sourceSize, err := strconv.ParseUint(bits[1], 10, 64)
	if err != nil {
		klog.Exitf("invalid CP size %q: %v", bits[1], err)
	}
	sourceRoot, err := base64.StdEncoding.DecodeString(bits[2])
	if err != nil {
		klog.Exitf("invalid checkpoint roothash %q: %v", bits[2], err)
	}

	// Create our Tessera storage backend:
	cfg := posix.Config{
		Path: *storageDir,
	}
	driver, err := posix.New(ctx, cfg)
	if err != nil {
		klog.Exitf("Failed to create new POSIX storage driver: %v", err)
	}

	opts := tessera.NewMigrationOptions().WithCTLayout()
	// Configure antispam storage, if necessary
	var antispam tessera.Antispam
	if *persistentAntispam {
		as_opts := tposix_as.AntispamOpts{
			MaxBatchSize: *antispamBatchSize,
		}
		antispam, err = tposix_as.NewAntispam(ctx, filepath.Join(*storageDir, ".state", "antispam"), as_opts)
		if err != nil {
			klog.Exitf("Failed to create new POSIX antispam storage: %v", err)
		}
		opts.WithAntispam(antispam)
	}

	m, err := tessera.NewMigrationTarget(ctx, driver, opts)
	if err != nil {
		klog.Exitf("Failed to create MigrationTarget: %v", err)
	}

	readEntryBundle := readCTEntryBundle(*sourceURL, hc)
	if err := m.Migrate(context.Background(), *numWorkers, sourceSize, sourceRoot, readEntryBundle); err != nil {
		klog.Exitf("Migrate failed: %v", err)
	}

	// TODO(phbnf): This will need extending to identify and copy over the entries from the intermediate cert storage.

	// TODO(Tessera #341): wait for antispam follower to complete
	<-make(chan bool)
}

func readCTEntryBundle(srcURL string, hc *http.Client) func(ctx context.Context, i uint64, p uint8) ([]byte, error) {
	return func(ctx context.Context, i uint64, p uint8) ([]byte, error) {
		up := strings.Replace(layout.EntriesPath(i, p), "entries", "data", 1)
		reqURL, err := url.JoinPath(srcURL, up)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return nil, err
		}
		rsp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() {
			if err := rsp.Body.Close(); err != nil {
				klog.Warningf("Failed to close response body: %v", err)
			}
		}()
		if rsp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %q: %v", req.URL.Path, rsp.Status)
		}
		return io.ReadAll(rsp.Body)
	}
}
