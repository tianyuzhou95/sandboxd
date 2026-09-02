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

// Package imageconfig contains image metadata shared by the OCI and Nydus
// image paths.
package imageconfig

import v1 "github.com/google/go-containerregistry/pkg/v1"

// Process describes the process-related fields from an OCI image config.
// A non-nil *Process means the image config was resolved, even when all fields
// are empty.
type Process struct {
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	User       string   `json:"user,omitempty"`
}

// FromOCI returns an owned copy of the process-related OCI image config.
func FromOCI(config v1.Config) *Process {
	return &Process{
		Entrypoint: append([]string(nil), config.Entrypoint...),
		Cmd:        append([]string(nil), config.Cmd...),
		Cwd:        config.WorkingDir,
		User:       config.User,
	}
}

// Clone returns an owned copy of p.
func Clone(p *Process) *Process {
	if p == nil {
		return nil
	}
	return &Process{
		Entrypoint: append([]string(nil), p.Entrypoint...),
		Cmd:        append([]string(nil), p.Cmd...),
		Cwd:        p.Cwd,
		User:       p.User,
	}
}
