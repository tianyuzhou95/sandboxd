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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CreateRootfsMountTargets seeds mount points in a new per-sandbox upper or
// placeholder directory. Callers must not pass a shared image rootfs here.
func CreateRootfsMountTargets(root string, mounts []Mount) error {
	for _, mount := range mounts {
		target, ok := placeholderMountTarget(root, mount.Destination)
		if !ok {
			continue
		}
		if mountSourceIsRegularFile(mount) {
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent for mount %q: %w", mount.Destination, err)
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("create file target for mount %q: %w", mount.Destination, err)
			}
			if err := file.Close(); err != nil {
				return fmt.Errorf("close file target for mount %q: %w", mount.Destination, err)
			}
			continue
		}
		if err := os.MkdirAll(target, 0755); err != nil {
			return fmt.Errorf("create directory target for mount %q: %w", mount.Destination, err)
		}
	}
	return nil
}

// RootfsMountTargetsReady checks a lower rootfs without following symlinks.
// A symlinked component is treated as requiring a private upper entry so
// privileged preparation never writes through an untrusted image symlink.
func RootfsMountTargetsReady(bundlePath string, spec *Spec) (bool, error) {
	if spec == nil || spec.Root == nil || spec.Root.Path == "" {
		return false, errors.New("root filesystem is missing")
	}
	root := spec.Root.Path
	if !filepath.IsAbs(root) {
		root = filepath.Join(bundlePath, root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return false, fmt.Errorf("stat rootfs %s: %w", root, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("rootfs %s is not a directory", root)
	}

	for _, mount := range spec.Mounts {
		ready, err := rootfsMountTargetReady(root, mount)
		if err != nil {
			return false, err
		}
		if !ready {
			return false, nil
		}
	}
	return true, nil
}

func rootfsMountTargetReady(root string, mount Mount) (bool, error) {
	target, ok := placeholderMountTarget(root, mount.Destination)
	if !ok {
		return true, nil
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false, fmt.Errorf("resolve rootfs mount target %q: %w", mount.Destination, err)
	}
	components := strings.Split(relative, string(os.PathSeparator))
	current := root
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect rootfs mount target %q: %w", mount.Destination, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, nil
		}
		if index < len(components)-1 {
			if !info.IsDir() {
				return false, nil
			}
			continue
		}
		if mountSourceIsRegularFile(mount) {
			return info.Mode().IsRegular(), nil
		}
		return info.IsDir(), nil
	}
	return true, nil
}
