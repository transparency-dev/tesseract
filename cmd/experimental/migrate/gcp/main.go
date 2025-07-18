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

// migrate-gcp is a command-line tool for migrating data from a static-ct
// compliant log, into a TesseraCT log instance.
package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/transparency-dev/tessera"
	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tessera/client"
	"github.com/transparency-dev/tessera/storage/gcp"
	gcp_as "github.com/transparency-dev/tessera/storage/gcp/antispam"
	"k8s.io/klog/v2"
)

var (
	bucket  = flag.String("bucket", "", "Bucket to use for storing log")
	spanner = flag.String("spanner", "", "Spanner resource URI ('projects/.../...')")

	sourceURL          = flag.String("source_url", "", "Base URL for the source log.")
	numWorkers         = flag.Uint("num_workers", 30, "Number of migration worker goroutines.")
	persistentAntispam = flag.Bool("antispam", false, "EXPERIMENTAL: Set to true to enable GCP-based persistent antispam storage.")
	antispamBatchSize  = flag.Uint("antispam_batch_size", 1500, "EXPERIMENTAL: maximum number of antispam rows to insert in a batch (1500 gives good performance with 300 Spanner PU and above, smaller values may be required for smaller allocs).")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	ctx := context.Background()

	srcURL, err := url.Parse(*sourceURL)
	if err != nil {
		klog.Exitf("Invalid --source_url %q: %v", *sourceURL, err)
	}
	// TODO(phbnf): This is currently built using the Tessera client lib, with a stand-alone func below for
	// fetching the Static CT entry bundles as they live in an different place.
	// When there's a Static CT client we can probably switch over to using it in here.
	src, err := client.NewHTTPFetcher(srcURL, nil)
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
	gcpCfg := storageConfigFromFlags()
	driver, err := gcp.New(ctx, gcpCfg)
	if err != nil {
		klog.Exitf("Failed to create new GCP storage driver: %v", err)
	}

	opts := tessera.NewMigrationOptions().WithCTLayout()
	// Configure antispam storage, if necessary
	var antispam tessera.Antispam
	// Persistent antispam is currently experimental, so there's no OpenTofu or documentation yet!
	if *persistentAntispam {
		as_opts := gcp_as.AntispamOpts{
			// 1500 appears to be give good performance for migrating logs, but you may need to lower it if you have
			// less than 300 Spanner PU available. (Consider temporarily raising your Spanner CPU quota to be at least
			// this amount for the duration of the migration.)
			MaxBatchSize: *antispamBatchSize,
		}
		antispam, err = gcp_as.NewAntispam(ctx, fmt.Sprintf("%s-antispam", *spanner), as_opts)
		if err != nil {
			klog.Exitf("Failed to create new GCP antispam storage: %v", err)
		}
		opts.WithAntispam(antispam)
	}

	m, err := tessera.NewMigrationTarget(ctx, driver, opts)
	if err != nil {
		klog.Exitf("Failed to create MigrationTarget: %v", err)
	}

	readEntryBundle := readCTEntryBundle(*sourceURL)
	if err := m.Migrate(context.Background(), *numWorkers, sourceSize, sourceRoot, readEntryBundle); err != nil {
		klog.Exitf("Migrate failed: %v", err)
	}

	// TODO(phbnf): This will need extending to identify and copy over the entries from the intermediate cert storage.

	// TODO(Tessera #341): wait for antispam follower to complete
	<-make(chan bool)
}

// storageConfigFromFlags returns a gcp.Config struct populated with values
// provided via flags.
func storageConfigFromFlags() gcp.Config {
	if *bucket == "" {
		klog.Exit("--bucket must be set")
	}
	if *spanner == "" {
		klog.Exit("--spanner must be set")
	}
	return gcp.Config{
		Bucket:  *bucket,
		Spanner: *spanner,
	}
}

func readCTEntryBundle(srcURL string) func(ctx context.Context, i uint64, p uint8) ([]byte, error) {
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
		rsp, err := http.DefaultClient.Do(req)
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
