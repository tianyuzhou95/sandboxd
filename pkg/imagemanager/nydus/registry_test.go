// Copyright (c) 2026 Ant Group Corporation.
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

package nydus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	remoteTransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

func TestFetchAndExtractBootstrapFallsBackToDirectFetchAfterProxyRetries(t *testing.T) {
	prevDelay := nydusFetchRetryDelay
	prevMaxDelay := nydusFetchRetryMaxDelay
	nydusFetchRetryDelay = time.Millisecond
	nydusFetchRetryMaxDelay = time.Millisecond
	defer func() {
		nydusFetchRetryDelay = prevDelay
		nydusFetchRetryMaxDelay = prevMaxDelay
	}()

	var proxyFetches int
	var directFetches int
	var extractCalls int

	client := &RegistryClient{
		fetchImageWithFallbackFn: func(context.Context, string, string) (v1.Image, error) {
			proxyFetches++
			return empty.Image, nil
		},
		fetchImageFn: func(_ context.Context, _ string, proxyURL string, useHTTP bool) (v1.Image, error) {
			if proxyURL != "" || useHTTP {
				t.Fatalf("direct fallback should use empty proxy and HTTPS, got proxy=%q useHTTP=%v", proxyURL, useHTTP)
			}
			directFetches++
			return empty.Image, nil
		},
		isNydusImageFn: func(v1.Image) (bool, error) {
			return true, nil
		},
		extractBootstrapFn: func(_ context.Context, _ v1.Image, outputDir string) (string, error) {
			extractCalls++
			if extractCalls <= nydusFetchRetryAttempts {
				return "", fmt.Errorf("failed to decompress layer: %w", &remoteTransport.Error{StatusCode: http.StatusInternalServerError})
			}
			return filepath.Join(outputDir, "bootstrap"), nil
		},
	}

	got, _, _, err := client.FetchAndExtractBootstrapWithImageConfig(context.Background(), "registry.example/test:nydus", t.TempDir(), "http://proxy.local")
	if err != nil {
		t.Fatalf("FetchAndExtractBootstrap() error = %v", err)
	}
	if got == "" {
		t.Fatalf("FetchAndExtractBootstrap() returned empty bootstrap path")
	}
	if proxyFetches != nydusFetchRetryAttempts {
		t.Fatalf("proxy fetch count = %d, want %d", proxyFetches, nydusFetchRetryAttempts)
	}
	if directFetches != 1 {
		t.Fatalf("direct fetch count = %d, want 1", directFetches)
	}
	if extractCalls != nydusFetchRetryAttempts+1 {
		t.Fatalf("extract call count = %d, want %d", extractCalls, nydusFetchRetryAttempts+1)
	}
}

func TestFetchAndExtractBootstrapRetriesOnUnexpectedEOF(t *testing.T) {
	prevDelay := nydusFetchRetryDelay
	prevMaxDelay := nydusFetchRetryMaxDelay
	nydusFetchRetryDelay = time.Millisecond
	nydusFetchRetryMaxDelay = time.Millisecond
	defer func() {
		nydusFetchRetryDelay = prevDelay
		nydusFetchRetryMaxDelay = prevMaxDelay
	}()

	var proxyFetches int
	var directFetches int

	client := &RegistryClient{
		fetchImageWithFallbackFn: func(context.Context, string, string) (v1.Image, error) {
			proxyFetches++
			return nil, fmt.Errorf("failed to fetch layer: %w", io.ErrUnexpectedEOF)
		},
		fetchImageFn: func(_ context.Context, _ string, proxyURL string, useHTTP bool) (v1.Image, error) {
			directFetches++
			return empty.Image, nil
		},
		isNydusImageFn: func(v1.Image) (bool, error) {
			return true, nil
		},
		extractBootstrapFn: func(_ context.Context, _ v1.Image, outputDir string) (string, error) {
			return filepath.Join(outputDir, "bootstrap"), nil
		},
	}

	got, _, _, err := client.FetchAndExtractBootstrapWithImageConfig(context.Background(), "registry.example/test:nydus", t.TempDir(), "http://proxy.local")
	if err != nil {
		t.Fatalf("FetchAndExtractBootstrap() error = %v", err)
	}
	if got == "" {
		t.Fatalf("FetchAndExtractBootstrap() returned empty bootstrap path")
	}
	if proxyFetches != nydusFetchRetryAttempts {
		t.Fatalf("proxy fetch count = %d, want %d", proxyFetches, nydusFetchRetryAttempts)
	}
	if directFetches != 1 {
		t.Fatalf("direct fetch count = %d, want 1", directFetches)
	}
}

func TestFetchAndExtractBootstrapSkipsDirectFallbackForNonRetryableError(t *testing.T) {
	prevDelay := nydusFetchRetryDelay
	prevMaxDelay := nydusFetchRetryMaxDelay
	nydusFetchRetryDelay = time.Millisecond
	nydusFetchRetryMaxDelay = time.Millisecond
	defer func() {
		nydusFetchRetryDelay = prevDelay
		nydusFetchRetryMaxDelay = prevMaxDelay
	}()

	var proxyFetches int
	var directFetches int

	client := &RegistryClient{
		fetchImageWithFallbackFn: func(context.Context, string, string) (v1.Image, error) {
			proxyFetches++
			return nil, fmt.Errorf("failed to fetch manifest: %w", &remoteTransport.Error{StatusCode: http.StatusNotFound})
		},
		fetchImageFn: func(context.Context, string, string, bool) (v1.Image, error) {
			directFetches++
			return nil, nil
		},
	}

	if _, _, _, err := client.FetchAndExtractBootstrapWithImageConfig(context.Background(), "registry.example/test:nydus", t.TempDir(), "http://proxy.local"); err == nil {
		t.Fatalf("FetchAndExtractBootstrap() error = nil, want non-nil")
	}
	if proxyFetches != 1 {
		t.Fatalf("proxy fetch count = %d, want 1", proxyFetches)
	}
	if directFetches != 0 {
		t.Fatalf("direct fetch count = %d, want 0", directFetches)
	}
}
