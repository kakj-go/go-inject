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
	"flag"
	"fmt"
	"github.com/kakj-go/go-inject/tools/common"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	path = flag.String("path", "", "project relative path")
)

// Usage is a replacement usage function for the flags package.
func Usage() {
	fmt.Fprintf(os.Stderr, "Usage of generate-inject:\n")
	fmt.Fprintf(os.Stderr, "\tgenerate-inject [flags] -package [directory]\n")
	fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("generate-inject: ")
	flag.Usage = Usage
	flag.Parse()

	if len(*path) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	workdir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Getwd failed. error: %v", err)
	}

	projectDir := filepath.Join(workdir, *path)
	packageName := common.GetProjectPackageName(projectDir)

	vendor := exec.Command("go", "mod", "vendor")
	vendor.Dir = projectDir
	vendor.Stderr = os.Stderr
	err = vendor.Run()
	if err != nil {
		log.Fatalf("failed to run go mod vendor error: %v", err)
	}
	vendorPath := filepath.Join(projectDir, "vendor")
	if !IsDir(vendorPath) {
		log.Fatalf("vendor dir: %v not find", vendorPath)
	}

	var packageList = common.GetGenerateImportPackage(workdir)

	interceptorMap := getInterceptor(projectDir, packageName, packageList)

	err = gen(vendorPath, interceptorMap)
	if err != nil {
		log.Fatalf("file walk vendor dir: %v failed. error: %v", vendorPath, err)
	}
	return
}

func gen(vendorPath string, interceptorMap map[string][]*common.Interceptor) error {
	return common.WalkGoFile(vendorPath, func(path string, info os.FileInfo) error {
		filePath := strings.TrimPrefix(path, vendorPath+"/")
		interceptors := interceptorMap[filePath]
		if interceptors == nil {
			return nil
		}

		common.Gen(path, path, filepath.Dir(filePath), interceptors)
		return nil
	})
}

func getInterceptor(projectDir string, projectPackage string, interceptorPackageList []string) map[string][]*common.Interceptor {
	var interceptorMap = map[string][]*common.Interceptor{}
	_ = common.WalkGoFile(projectDir, func(path string, info os.FileInfo) error {
		for _, packet := range interceptorPackageList {
			if strings.HasPrefix(packet, projectPackage) {
				if strings.HasPrefix(path, filepath.Join(projectDir, "vendor")) {
					continue
				}
				if strings.TrimPrefix(packet, projectPackage) != strings.TrimPrefix(filepath.Dir(path), projectDir) {
					continue
				}
			} else {
				if !strings.HasPrefix(path, filepath.Join(projectDir, "vendor")) {
					continue
				}
				if strings.TrimPrefix(filepath.Dir(path), filepath.Join(projectDir, "vendor")+"/") != packet {
					continue
				}
			}

			node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			if node.Doc == nil || len(node.Doc.List) == 0 {
				continue
			}

			interceptorName := strings.TrimSpace(strings.TrimPrefix(node.Doc.List[0].Text, common.DocPrefix))
			if strings.TrimSpace(interceptorName) == "" {
				continue
			}

			genInterceptors := common.GenInterceptor(node)
			for _, interceptor := range genInterceptors {
				interceptor.InterceptorName = interceptorName
				interceptorMap[interceptorName] = append(interceptorMap[interceptorName], interceptor)
			}
		}
		return nil
	})

	return interceptorMap
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
