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

package common

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteSandboxRuntimeMarker records which adapter owns an OCI bundle.
func WriteSandboxRuntimeMarker(bundlePath, runtimeName string) error {
	data, err := json.Marshal(struct {
		Runtime string `json:"runtime"`
	}{Runtime: runtimeName})
	if err != nil {
		return err
	}
	temporaryPath := filepath.Join(bundlePath, ".runtime.json.tmp")
	finalPath := filepath.Join(bundlePath, "runtime.json")
	if err := os.WriteFile(temporaryPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(temporaryPath, finalPath)
}
