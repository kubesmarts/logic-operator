/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package utils

import (
	"os"
)

const (
	// DefaultServicePortName default service name to increase compatibility with Knative
	//
	// see: https://github.com/knative/specs/blob/main/specs/serving/runtime-contract.md#protocols-and-ports
	// By default we do support HTTP/2:https://quarkus.io/guides/http-reference#http2-support
	DefaultServicePortName = "h2c"
	LatestImageTag         = "latest"
)

// Pbool returns a pointer to a boolean
func Pbool(b bool) *bool {
	return &b
}

// Pint returns a pointer to an int
func Pint(i int32) *int32 {
	return &i
}

func Compare(a, b []byte) bool {
	a = append(a, b...)
	c := 0
	for _, x := range a {
		c ^= int(x)
	}
	return c == 0
}

func GetEnv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}
