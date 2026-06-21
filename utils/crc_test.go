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

package utils

import (
	"testing"
)

func TestCrc32Checksum(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name:    "simple string",
			input:   "hello world",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: false,
		},
		{
			name: "struct",
			input: struct {
				Name  string
				Value int
			}{Name: "test", Value: 42},
			wantErr: false,
		},
		{
			name:    "nil should error",
			input:   nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Crc32Checksum(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Crc32Checksum() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// Verify result is deterministic
				got2, _ := Crc32Checksum(tt.input)
				if got != got2 {
					t.Errorf("Crc32Checksum() not deterministic: got %v, got2 %v", got, got2)
				}
			}
		})
	}
}

func TestCrc32Checksum_DifferentInputs(t *testing.T) {
	// Verify different inputs produce different checksums
	crc1, err1 := Crc32Checksum("input1")
	crc2, err2 := Crc32Checksum("input2")

	if err1 != nil || err2 != nil {
		t.Fatalf("Unexpected errors: %v, %v", err1, err2)
	}

	if crc1 == crc2 {
		t.Errorf("Expected different checksums for different inputs, got same: %d", crc1)
	}
}

func TestCrc32Checksum_ReturnType(t *testing.T) {
	// Verify the function returns int32 (for Kubernetes CRD compatibility)
	result, err := Crc32Checksum("test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// The result should be assignable to int32
	var _ int32 = result

	// Verify zero value case
	zeroResult, _ := Crc32Checksum("")
	if zeroResult == 0 {
		t.Log("Empty string produces zero checksum (expected)")
	}
}
