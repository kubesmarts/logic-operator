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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENV_DIR="$REPO_ROOT/venv"

# Create virtual environment if it doesn't exist
if [ ! -d "$VENV_DIR" ]; then
    echo "Creating Python virtual environment..."
    python3 -m venv "$VENV_DIR"
fi

# Activate virtual environment and install cekit
echo "Installing CEKit and dependencies..."
source "$VENV_DIR/bin/activate"
pip3 install -q -r "$REPO_ROOT/images/requirements.txt"

echo "CEKit setup complete. Virtual environment at: $VENV_DIR"
echo "To use cekit, run: source venv/bin/activate"
