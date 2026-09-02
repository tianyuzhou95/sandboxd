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

package imageconfig

import (
	"reflect"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestFromOCI(t *testing.T) {
	config := v1.Config{
		Entrypoint: []string{"/entrypoint", "--verbose"},
		Cmd:        []string{"serve", "8080"},
		WorkingDir: "/workspace",
		User:       "1000:1000",
	}
	got := FromOCI(config)
	want := &Process{
		Entrypoint: []string{"/entrypoint", "--verbose"},
		Cmd:        []string{"serve", "8080"},
		Cwd:        "/workspace",
		User:       "1000:1000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FromOCI() = %#v, want %#v", got, want)
	}

	config.Entrypoint[0] = "changed"
	config.Cmd[0] = "changed"
	if got.Entrypoint[0] != "/entrypoint" || got.Cmd[0] != "serve" {
		t.Fatalf("FromOCI() retained aliases: %#v", got)
	}
}

func TestClone(t *testing.T) {
	original := &Process{Entrypoint: []string{"/entrypoint"}, Cmd: []string{"serve"}}
	clone := Clone(original)
	clone.Entrypoint[0] = "changed"
	clone.Cmd[0] = "changed"
	if original.Entrypoint[0] != "/entrypoint" || original.Cmd[0] != "serve" {
		t.Fatalf("Clone() retained aliases: %#v", original)
	}
	if Clone(nil) != nil {
		t.Fatal("Clone(nil) returned non-nil")
	}
}
