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

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirecrackerCheckpointArchiveRoundTrip(t *testing.T) {
	for _, compress := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw", true: "gzip"}[compress], func(t *testing.T) {
			root := t.TempDir()
			source := firecrackerCheckpointFiles{
				State:   filepath.Join(root, "source-state"),
				Memory:  filepath.Join(root, "source-memory"),
				Overlay: filepath.Join(root, "source-overlay"),
			}
			want := map[string]string{
				source.State:   "vm-state-data",
				source.Memory:  strings.Repeat("memory-page-", 4096),
				source.Overlay: strings.Repeat("overlay-block-", 4096),
			}
			for path, data := range want {
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			image := filepath.Join(root, "checkpoint.img")
			if err := createFirecrackerCheckpointArchive(
				context.Background(), image, compress, source,
			); err != nil {
				t.Fatal(err)
			}
			prefix, err := os.ReadFile(image)
			if err != nil {
				t.Fatal(err)
			}
			isGzip := len(prefix) >= 2 && prefix[0] == 0x1f && prefix[1] == 0x8b
			if isGzip != compress {
				t.Fatalf("gzip magic = %v, compress = %v", isGzip, compress)
			}

			destinationRoot := filepath.Join(root, "restore-with-new-id")
			if err := os.Mkdir(destinationRoot, 0700); err != nil {
				t.Fatal(err)
			}
			destination := firecrackerCheckpointFiles{
				State:   filepath.Join(destinationRoot, "vmstate"),
				Memory:  filepath.Join(destinationRoot, "memory"),
				Overlay: filepath.Join(destinationRoot, "overlay.ext4"),
			}
			if err := extractFirecrackerCheckpointArchive(
				context.Background(), image, destination,
			); err != nil {
				t.Fatal(err)
			}
			pairs := map[string]string{
				destination.State:   want[source.State],
				destination.Memory:  want[source.Memory],
				destination.Overlay: want[source.Overlay],
			}
			for path, expected := range pairs {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != expected {
					t.Fatalf("restored %s content differs", path)
				}
			}
		})
	}
}

func TestFirecrackerCheckpointArchiveCancellationRemovesOutput(t *testing.T) {
	root := t.TempDir()
	files := firecrackerCheckpointFiles{
		State:   filepath.Join(root, "state"),
		Memory:  filepath.Join(root, "memory"),
		Overlay: filepath.Join(root, "overlay"),
	}
	for _, path := range []string{files.State, files.Memory, files.Overlay} {
		if err := os.WriteFile(path, []byte("content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	image := filepath.Join(root, "checkpoint.img")
	err := createFirecrackerCheckpointArchive(ctx, image, true, files)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("checkpoint error = %v, want context canceled", err)
	}
	if _, err := os.Stat(image); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial checkpoint retained: %v", err)
	}
}
