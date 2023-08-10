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
	"fmt"
	"os"
)

type Flags struct {
	Help  bool   `flag:"-h"`
	Debug string `flag:"-debug"`
	Path  string `flag:"-path"`
}

func PrintUsageWithExit() {
	fmt.Printf(`Usage: go {build,install} -a [-work] -toolexec "%s" PACKAGE...

The toolexec-inject is designed for automatic inject of Golang programs.

Options:
		-h
				Print the usage message.
		-debug
				Hepling to debug the toolexec-inject process, the value is the path of the debug file.
		-path
				Project relative path
`, os.Args[0])
	os.Exit(1)
}
