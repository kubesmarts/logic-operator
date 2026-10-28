// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package version

// These values are populated by the Go linker from versions.mk. Keep defaults
// useful for builds that bypass the Makefile, but don't use them for releases.
var (
	gitCommit                = "unknown"
	operatorVersion          = "unknown"
	workflowGatewayVersion   = "unknown"
	dataIndexVersion         = "unknown"
	quarkusFlowRunnerVersion = "unknown"
)

// GetGitCommit gets the source revision used to build the operator.
func GetGitCommit() string {
	return gitCommit
}

// GetOperatorVersion gets the current operator release version.
func GetOperatorVersion() string {
	return operatorVersion
}

// GetWorkflowGatewayVersion gets the workflow-gateway image version bundled
// with this operator release.
func GetWorkflowGatewayVersion() string {
	return workflowGatewayVersion
}

// GetDataIndexVersion gets the Data Index image version bundled with this
// operator release.
func GetDataIndexVersion() string {
	return dataIndexVersion
}

// GetQuarkusFlowRunnerVersion gets the Quarkus Flow runner image version
// bundled with this operator release.
func GetQuarkusFlowRunnerVersion() string {
	return quarkusFlowRunnerVersion
}
