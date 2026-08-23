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
	runtime "github.com/inclusionAI/sandboxd/api/runtime/v1"
	"github.com/inclusionAI/sandboxd/config"
	"google.golang.org/protobuf/proto"
)

func cloneHostResources(
	resource *runtime.LinuxSandboxResources,
) *runtime.LinuxSandboxResources {
	if resource == nil {
		return nil
	}
	return proto.Clone(resource).(*runtime.LinuxSandboxResources)
}

// HostCgroupResources maps guest-visible resources to the host cgroup that
// encloses the selected runtime process. VM runtimes receive clones because
// guest sizing and outer runtime constraints are separate concerns.
func HostCgroupResources(
	runtimeName string,
	resource *runtime.LinuxSandboxResources,
) *runtime.LinuxSandboxResources {
	switch runtimeName {
	case config.RuntimeNameFirecracker:
		return firecrackerHostResources(resource)
	case config.RuntimeNameKata:
		return kataHostResources(resource)
	default:
		return resource
	}
}
