/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

const (
	// QuarkusFlowRunnerHTTPPort is the Quarkus Flow runner's HTTP port inside a pod.
	QuarkusFlowRunnerHTTPPort int32 = 8080

	// FlowRunnerRegistry is the registry hosting the Quarkus Flow runner image.
	FlowRunnerRegistry = "quay.io/quarkiverse"
	// FlowRunnerImage is the Quarkus Flow runner image name.
	FlowRunnerImage = "quarkus-flow-runner"
	// ImageVariantMinimal is the runner image variant without persistence support.
	ImageVariantMinimal = "minimal"
	// ImageVariantStandard is the runner image variant with persistence support.
	ImageVariantStandard = "standard"
)
