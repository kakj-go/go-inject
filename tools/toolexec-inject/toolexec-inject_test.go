/*
 * Copyright 2023 The kakj-go Authors.
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *     http://www.apache.org/licenses/LICENSE-2.0
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bou.ke/monkey"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"testing"
)

func Test_getInterceptor(t *testing.T) {
	_, err := ParseFlags(flags, []string{"-path", "../"})
	if err != nil {
		PrintUsageWithExit()
	}
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	patch := monkey.Patch(os.Getwd, func() (dir string, err error) {
		return filepath.Join(wd, "../../example/gin-toolexec-inject/server"), nil
	})
	defer patch.Unpatch()

	result := getInterceptorPaths()
	assert.Equal(t, len(result), 2)
}
