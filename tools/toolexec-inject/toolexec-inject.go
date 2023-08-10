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
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/kakj-go/go-inject/tools/common"
	"github.com/sirupsen/logrus"
	"go/parser"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var flags = &Flags{}

func main() {
	args := os.Args[1:]
	firstNonOptionIndex, err := ParseFlags(flags, args)
	if err != nil {
		PrintUsageWithExit()
	}

	if flags.Help {
		PrintUsageWithExit()
	}

	cmdName := ParseProxyCommandName(args, firstNonOptionIndex)
	if cmdName != "compile" {
		executeDelegateCommand(args[firstNonOptionIndex:])
		return
	}

	// parse the args
	compileOptions := &CompileOptions{}
	if _, err = ParseFlags(compileOptions, args); err != nil {
		executeDelegateCommand(args[firstNonOptionIndex:])
		return
	}

	// execute the enhancement
	args, err = Execute(compileOptions, args)
	if err != nil {
		log.Fatal(err)
	}

	executeDelegateCommand(args[firstNonOptionIndex:])
}

func Execute(opts *CompileOptions, args []string) ([]string, error) {
	// if the options is invalid, just ignore
	if !opts.IsValid() {
		return args, nil
	}

	// remove the vendor directory to get the real package name
	opts.Package = UnVendor(opts.Package)

	// init the logger for the instrument
	loggerFile, err := initLogger(opts)
	if err != nil {
		return nil, err
	}
	defer loggerFile.Close()

	interceptorMap := getAndCacheInterceptor(opts)
	interceptorMap = interceptorAddModInfo(interceptorMap)

	cacheCompileOptions(opts)

	cfgPath := filepath.Join(filepath.Dir(opts.Output), "importcfg")
	cfgFile, err := os.OpenFile(cfgPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf(err.Error())
	}
	defer cfgFile.Close()

	files := parseFilesInArgs(args)

	for file, index := range files {
		afterFile, packageFiles := writeNewFile(opts, interceptorMap, file)
		if file == afterFile {
			continue
		}
		args[index] = afterFile

		for _, packageFile := range packageFiles {
			_, err = cfgFile.WriteString(packageFile + "\n")
			if err != nil {
				log.Fatalf(err.Error())
			}
		}
	}
	return args, nil
}

func getCacheCompileOptions(opts *CompileOptions) map[string]string {
	cacheFilePath := filepath.Join(filepath.Dir(opts.CompileBaseDir()), "toolexec-inject-cache.data")
	if !fileExist(cacheFilePath) {
		return nil
	}
	// 打开文件
	file, err := os.Open(cacheFilePath)
	if err != nil {
		log.Fatalf(err.Error())
	}
	defer file.Close()
	var optMap = map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		splits := strings.SplitN(line, "=", 2)
		if len(splits) == 2 {
			optMap[strings.TrimSpace(splits[0])] = strings.TrimSpace(splits[1])
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf(err.Error())
	}
	return optMap
}

func cacheCompileOptions(opts *CompileOptions) {
	cacheFilePath := filepath.Join(filepath.Dir(opts.CompileBaseDir()), "toolexec-inject-cache.data")
	file, err := os.OpenFile(cacheFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	_, err = file.WriteString(fmt.Sprintf("%v=%v\n", opts.Package, opts.Output))
	if err != nil {
		log.Fatalf(err.Error())
	}
	file.Close()
}

func writeNewFile(opts *CompileOptions, interceptorMap map[string][]*common.Interceptor, preFile string) (string, []string) {
	if len(interceptorMap) == 0 {
		return preFile, nil
	}

	goRoot := os.Getenv("GOROOT")
	goSrcPrefix := fmt.Sprintf("%v/src/", goRoot)
	goSrcVendorPrefix := fmt.Sprintf("%v/src/vendor/", goRoot)

	gopath := os.Getenv("GOPATH")
	goModPrefix := fmt.Sprintf("%v/pkg/mod/", gopath)

	inputFileName := ""
	if strings.HasPrefix(preFile, goSrcPrefix) {
		inputFileName = strings.TrimPrefix(preFile, goSrcPrefix)
	}
	if strings.HasPrefix(preFile, goSrcVendorPrefix) {
		inputFileName = strings.TrimPrefix(preFile, goSrcVendorPrefix)
	}
	if strings.HasPrefix(preFile, goModPrefix) {
		inputFileName = strings.TrimPrefix(preFile, goModPrefix)
	}
	if inputFileName == "" {
		return preFile, nil
	}
	splits := strings.SplitN(inputFileName, "@", 2)
	if len(splits) == 2 {
		sPaths := strings.SplitN(splits[1], "/", 2)
		if len(sPaths) == 2 {
			inputFileName = fmt.Sprintf("%v/%v", splits[0], sPaths[1])
		} else {
			inputFileName = splits[0]
		}
	}

	interceptors := interceptorMap[inputFileName]
	if len(interceptors) == 0 {
		return preFile, nil
	}

	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, preFile, nil, 0)
	if err != nil {
		log.Fatalf("parser.ParseFile: %v failed. error: %v", preFile, err)
	}

	if node.Doc != nil && len(node.Doc.List) > 0 && strings.HasPrefix(node.Doc.List[0].Text, common.DocPrefix) {
		return preFile, nil
	}

	afterFileName := fmt.Sprintf("%v_%v.go", strings.TrimSuffix(filepath.Base(preFile), ".go"), "go_inject")
	afterFilePath := filepath.Join(filepath.Dir(opts.Output), afterFileName)
	importMap := common.Gen(preFile, afterFilePath, filepath.Dir(inputFileName), interceptors)

	var packageFiles []string
	if len(importMap) > 0 {
		optsMap := getCacheCompileOptions(opts)
		for key := range importMap {
			key = strings.Trim(key, `"`)
			value, ok := optsMap[key]
			if !ok {
				continue
			}
			packageFiles = append(packageFiles, fmt.Sprintf("packagefile %v=%v", key, value))
		}
	}

	return afterFilePath, packageFiles
}

func interceptorAddModInfo(interceptorMap map[string][]*common.Interceptor) map[string][]*common.Interceptor {
	workdir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Getwd failed. error: %v", err)
	}

	projectDir := filepath.Join(workdir, flags.Path)
	modFile := common.ParseProjectMod(projectDir)

	for key, values := range interceptorMap {
		for _, mod := range modFile.Require {
			if !strings.HasPrefix(key, mod.Mod.Path) {
				continue
			}

			for _, interceptor := range values {
				interceptor.Mods = append(interceptor.Mods, mod.Mod)
			}
		}
	}
	return interceptorMap
}

func getAndCacheInterceptor(opts *CompileOptions) map[string][]*common.Interceptor {
	baseDir := opts.CompileBaseDir()

	cacheFilePath := filepath.Join(baseDir, ".go-inject.data")
	var interceptorPath = []string{}
	if fileExist(cacheFilePath) {
		content, err := os.ReadFile(cacheFilePath)
		if err != nil {
			log.Fatalf("os.ReadFile %v failed error: %v \n", cacheFilePath, err)
		}
		err = json.Unmarshal(content, &interceptorPath)
		if err != nil {
			log.Fatalf("json.Unmarshal inject cache file %v failed error: %v \n", cacheFilePath, err)
		}
	} else {
		interceptorPath = getInterceptorPaths()
		interceptorsJson, err := json.Marshal(interceptorPath)
		if err != nil {
			log.Fatalf("json.Marshal inject failed error: %v \n", err)
		}
		cacheFile, err := os.Create(cacheFilePath)
		if err != nil {
			log.Fatalf("os.create inject file: %v failed error: %v \n", cacheFilePath, err)
		}
		defer cacheFile.Close()
		_, err = cacheFile.Write(interceptorsJson)
		if err != nil {
			log.Fatalf("write inject json to file: %v failed error: %v \n", cacheFilePath, err)
		}
	}

	interceptorMap := map[string][]*common.Interceptor{}
	for _, path := range interceptorPath {
		node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			log.Fatalf("parseFile: %v failed. error: %v", path, err)
		}
		interceptorName := strings.TrimSpace(strings.TrimPrefix(node.Doc.List[0].Text, common.DocPrefix))
		if strings.TrimSpace(interceptorName) == "" {
			log.Fatalf("file: %v get interceptorName failed. error: %v", path, err)
		}

		genInterceptors := common.GenInterceptor(node)
		for _, interceptor := range genInterceptors {
			interceptor.InterceptorName = interceptorName
			interceptorMap[interceptorName] = append(interceptorMap[interceptorName], interceptor)
		}
	}

	return interceptorMap
}

func fileExist(filepath string) bool {
	_, err := os.Stat(filepath)
	if err == nil {
		return true
	} else if os.IsNotExist(err) {
		return false
	} else {
		return false
	}
}

func getInterceptorPaths() []string {
	workdir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Getwd failed. error: %v", err)
	}

	projectDir := filepath.Join(workdir, flags.Path)
	packageName := common.GetProjectPackageName(projectDir)

	var importPackageList = common.GetGenerateImportPackage(workdir)

	var projectImportPackages []string
	var modImportPackages []string
	for _, packagePath := range importPackageList {
		if strings.HasPrefix(packagePath, packageName) {
			projectImportPackages = append(projectImportPackages, packagePath)
		} else {
			modImportPackages = append(modImportPackages, packagePath)
		}
	}

	var interceptorPaths = []string{}
	err = common.WalkGoFile(projectDir, func(path string, info os.FileInfo) error {
		packagePath := filepath.Dir(strings.TrimPrefix(path, projectDir))
		for _, projectImportPackage := range projectImportPackages {
			projectImportPackagePath := strings.TrimPrefix(projectImportPackage, packageName)
			if packagePath != projectImportPackagePath {
				continue
			}

			node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			if node.Doc == nil || len(node.Doc.List) == 0 {
				return nil
			}

			interceptorPath := strings.TrimSpace(strings.TrimPrefix(node.Doc.List[0].Text, common.DocPrefix))
			if strings.TrimSpace(interceptorPath) == "" {
				return nil
			}
			interceptorPaths = append(interceptorPaths, path)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("get project interceptor failed. error: %v", err)
	}

	goModCache := os.Getenv("GOMODCACHE")
	if goModCache == "" {
		return interceptorPaths
	}

	modFile := common.ParseProjectMod(projectDir)
	for _, modImport := range modImportPackages {
		for _, require := range modFile.Require {
			if require.Indirect {
				continue
			}

			if !strings.HasPrefix(modImport, require.Mod.Path) {
				continue
			}

			importPacketPath := filepath.Join(goModCache, require.Mod.String())
			packagePath := strings.TrimPrefix(modImport, require.Mod.Path)
			packagePath = strings.TrimPrefix(packagePath, "/")
			importRealPath := filepath.Join(importPacketPath, packagePath)

			files, err := os.ReadDir(importRealPath)
			if err != nil {
				log.Fatalf("reading directory error: %v", err)
			}

			for _, file := range files {
				if file.IsDir() || filepath.Ext(file.Name()) != ".go" {
					continue
				}

				path := filepath.Join(importRealPath, file.Name())
				node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
				if err != nil {
					log.Fatalf("get mod interceptor failed. error: %v", err)
				}

				if node.Doc == nil || len(node.Doc.List) == 0 {
					continue
				}

				interceptorPath := strings.TrimSpace(strings.TrimPrefix(node.Doc.List[0].Text, common.DocPrefix))
				if strings.TrimSpace(interceptorPath) == "" {
					continue
				}

				interceptorPaths = append(interceptorPaths, path)
			}
		}
	}

	return interceptorPaths
}

func initLogger(opts *CompileOptions) (*os.File, error) {
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})
	logFilePath := filepath.Join(filepath.Dir(opts.CompileBaseDir()), "toolexec-inject.log")
	file, err := os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	logrus.SetOutput(file)
	return file, nil
}

func parseFilesInArgs(args []string) map[string]int {
	parsedFiles := map[string]int{}
	for inx, path := range args {
		// only process the go file
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		parsedFiles[path] = inx
	}
	return parsedFiles
}

func executeDelegateCommand(args []string) {
	path := args[0]
	args = args[1:]
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e := cmd.Run(); e != nil {
		log.Fatal(e)
	}
}

func UnVendor(path string) string {
	i := strings.Index(path, "/vendor/")
	if i == -1 {
		return path
	}
	return path[i+len("/vendor/"):]
}
