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

package gcp

import (
	"context"
	"fmt"
	"io"

	gcs "cloud.google.com/go/storage"
	"github.com/transparency-dev/tessera/api/layout"
	"github.com/transparency-dev/tesseract/internal/client"
)

// NewGSFetcher creates a new GSFetcher for the Google Cloud Storage bucket, using
// the provided GCS client.
//
// bucket should not contain any slash.
// c may be nil, in which case a new GCS client will be used.
func NewGSFetcher(ctx context.Context, bucket string, c *gcs.Client) (*GSFetcher, error) {
	if c == nil {
		var err error
		c, err = gcs.NewClient(ctx, gcs.WithJSONReads())
		if err != nil {
			return nil, err
		}
	}
	return &GSFetcher{
		bucket: bucket,
		c:      c,
	}, nil
}

// GSFetcher knows how to fetch log artifacts from a Google Cloud Storage bucket.
type GSFetcher struct {
	bucket string
	c      *gcs.Client
}

func (f GSFetcher) fetch(ctx context.Context, p string) ([]byte, error) {
	r, err := f.c.Bucket(f.bucket).Object(p).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("getObject: failed to create reader for object %q in bucket %q: %w", p, f.bucket, err)
	}

	d, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q: %v", p, err)
	}
	return d, r.Close()
}

func (f GSFetcher) ReadCheckpoint(ctx context.Context) ([]byte, error) {
	return f.fetch(ctx, layout.CheckpointPath)
}

func (f GSFetcher) ReadTile(ctx context.Context, l, i uint64, p uint8) ([]byte, error) {
	return client.PartialOrFullResource(ctx, p, func(ctx context.Context, p uint8) ([]byte, error) {
		return f.fetch(ctx, layout.TilePath(l, i, p))
	})
}

func (f GSFetcher) ReadEntryBundle(ctx context.Context, i uint64, p uint8) ([]byte, error) {
	return client.PartialOrFullResource(ctx, p, func(ctx context.Context, p uint8) ([]byte, error) {
		return f.fetch(ctx, fmt.Sprintf("tile/data/%s", layout.NWithSuffix(0, i, p)))
	})
}
