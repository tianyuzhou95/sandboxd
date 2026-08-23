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
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateRootfsMountTargets(t *testing.T) {
	root := t.TempDir()
	fileSource := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(fileSource, []byte("hosts"), 0644); err != nil {
		t.Fatal(err)
	}
	mounts := []Mount{
		{Type: "bind", Source: fileSource, Destination: "/etc/hosts"},
		{Type: "bind", Source: t.TempDir(), Destination: "/data"},
	}
	if err := CreateRootfsMountTargets(root, mounts); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(root, "etc", "hosts")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("file target was not created: info=%v err=%v", info, err)
	}
	if info, err := os.Lstat(filepath.Join(root, "data")); err != nil || !info.IsDir() {
		t.Fatalf("directory target was not created: info=%v err=%v", info, err)
	}
}

func TestRootfsMountTargetsReadyForReadonlyDirectory(t *testing.T) {
	root := t.TempDir()
	fileSource := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(fileSource, []byte("hosts"), 0644); err != nil {
		t.Fatal(err)
	}
	spec := &Spec{
		Root:   &Root{Path: root, Readonly: true},
		Mounts: []Mount{{Type: "bind", Source: fileSource, Destination: "/etc/hosts"}},
	}
	ready, err := RootfsMountTargetsReady(t.TempDir(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("missing target was reported ready")
	}
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "hosts"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	ready, err = RootfsMountTargetsReady(t.TempDir(), spec)
	if err != nil || !ready {
		t.Fatalf("existing target was not ready: ready=%v err=%v", ready, err)
	}
}

func TestRootfsMountTargetsReadyDoesNotFollowImageSymlink(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "etc")); err != nil {
		t.Fatal(err)
	}
	fileSource := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(fileSource, nil, 0644); err != nil {
		t.Fatal(err)
	}
	spec := &Spec{
		Root:   &Root{Path: root, Readonly: true},
		Mounts: []Mount{{Type: "bind", Source: fileSource, Destination: "/etc/hosts"}},
	}
	ready, err := RootfsMountTargetsReady(t.TempDir(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if ready {
		t.Fatal("symlinked target was reported ready")
	}
	if _, err := os.Stat(filepath.Join(external, "hosts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external path was modified: %v", err)
	}
}
