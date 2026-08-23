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

import runtime "github.com/inclusionAI/sandboxd/api/runtime/v1"

const (
	// A full snapshot faults every guest page into the VMM. Keep host-only
	// headroom for Firecracker itself without changing guest-visible memory.
	firecrackerHostMemoryOverheadBytes = int64(64 * 1024 * 1024)
)

func firecrackerHostResources(
	resource *runtime.LinuxSandboxResources,
) *runtime.LinuxSandboxResources {
	host := cloneHostResources(resource)
	if host == nil || host.MemoryLimitInBytes <= 0 {
		return host
	}
	host.MemoryLimitInBytes = addFirecrackerMemoryOverhead(host.MemoryLimitInBytes)
	if host.MemorySwapLimitInBytes > 0 {
		host.MemorySwapLimitInBytes = addFirecrackerMemoryOverhead(
			host.MemorySwapLimitInBytes,
		)
	}
	return host
}

func addFirecrackerMemoryOverhead(value int64) int64 {
	if value > int64(^uint64(0)>>1)-firecrackerHostMemoryOverheadBytes {
		return int64(^uint64(0) >> 1)
	}
	return value + firecrackerHostMemoryOverheadBytes
}
