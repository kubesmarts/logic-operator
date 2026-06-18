#!/bin/bash
#
# Copyright 2026 The Kubesmarts Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -e

# Pull required images from quay.io/kubesmarts for e2e testing
echo "📥 Pulling Kubesmarts test images from quay.io..."

# Source .env file if it exists to get image references
if [ -f .env ]; then
    source .env
fi

# Use environment variables or defaults
BUILDER_IMAGE=${RELATED_IMAGE_BASE_BUILDER:-quay.io/kubesmarts/incubator-kie-sonataflow-builder:main}
DEVMODE_IMAGE=${RELATED_IMAGE_DEVMODE:-quay.io/kubesmarts/incubator-kie-sonataflow-devmode:main}

echo "🔄 Pulling builder image: ${BUILDER_IMAGE}"
docker pull "${BUILDER_IMAGE}"

echo "🔄 Pulling devmode image: ${DEVMODE_IMAGE}"
docker pull "${DEVMODE_IMAGE}"

echo "✅ All required images pulled successfully"
