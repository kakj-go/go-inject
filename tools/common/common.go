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

package common

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

var DocPrefix = "//inject:"

type Interceptor struct {
	InterceptorName string
	StructType      string
	StructTypePoint bool
	StructName      string
	MethodName      string
	MethodResults   []*InterceptorResult
	Params          []*InterceptorParam

	Imports []*ast.ImportSpec
	Body    *ast.BlockStmt
	Results *ast.FieldList

	Mods []module.Version
}

type InterceptorResult struct {
	Name  []string
	Typer string
	Point bool
}

type InterceptorParam struct {
	Name  []string
	Typer string
	Point bool
}

func ParseProjectMod(projectDir string) *modfile.File {
	goModFile, err := os.OpenFile(filepath.Join(projectDir, "go.mod"), os.O_RDONLY, 0666)
	if err != nil {
		log.Fatalf("open go mod file failed. error: %v", err)
	}
	defer goModFile.Close()

	result, err := io.ReadAll(goModFile)
	if err != nil {
		log.Fatalf("open go mod file failed. error: %v", err)
	}
	parse, err := modfile.Parse("go.mod", result, nil)
	if err != nil {
		log.Fatalf("parse go mod file failed. error: %v", err)
	}
	return parse
}

func GetProjectPackageName(projectDir string) string {
	parse := ParseProjectMod(projectDir)
	return parse.Module.Mod.Path
}

func GetGenerateImportPackage(workdir string) []string {
	var packetList []string
	err := WalkGoFile(workdir, func(path string, info os.FileInfo) error {
		node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		if node.Doc == nil || len(node.Doc.List) == 0 {
			return nil
		}

		if !strings.HasPrefix(node.Doc.List[0].Text, "//go:generate generate-inject") {
			return nil
		}
		for _, imp := range node.Imports {
			packetList = append(packetList, strings.Trim(imp.Path.Value, `"`))
		}
		return nil
	})
	if err != nil {
		log.Fatalf("get generate import failed. error: %v", err)
	}
	return packetList
}

func WalkGoFile(basePackage string, callback func(path string, info os.FileInfo) error) error {
	return filepath.Walk(basePackage, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}

		return callback(path, info)
	})
}

func GenInterceptor(node *ast.File) []*Interceptor {
	var interceptors []*Interceptor
	for _, decl := range node.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		var intercept = &Interceptor{
			MethodName: funcDecl.Name.Name,
			Body:       funcDecl.Body,
		}
		if funcDecl.Recv != nil {
			var structName = ""
			if len(funcDecl.Recv.List[0].Names) > 0 {
				structName = funcDecl.Recv.List[0].Names[0].Name
			}

			structIdent, point := GetTyperStructIdent(funcDecl.Recv.List[0].Type)
			if structIdent == nil {
				continue
			}
			if structIdent == nil {
				continue
			}

			intercept.StructName = structName
			intercept.StructType = structIdent.Name
			intercept.StructTypePoint = point
		}

		if funcDecl.Type != nil && funcDecl.Type.Params != nil {
			var params []*InterceptorParam
			for _, param := range funcDecl.Type.Params.List {
				p := InterceptorParam{}
				structIdent, point := GetTyperStructIdent(param.Type)
				if structIdent == nil {
					continue
				}
				p.Point = point
				p.Typer = structIdent.Name
				for _, name := range param.Names {
					p.Name = append(p.Name, name.Name)
				}
				params = append(params, &p)
			}
			intercept.Params = params
		}

		if funcDecl.Type != nil && funcDecl.Type.Results != nil {
			var results []*InterceptorResult
			for index, result := range funcDecl.Type.Results.List {
				p := InterceptorResult{}
				structIdent, point := GetTyperStructIdent(result.Type)
				if structIdent == nil {
					continue
				}

				for _, name := range result.Names {
					p.Name = append(p.Name, name.Name)
				}
				if len(p.Name) == 0 {
					p.Name = append(p.Name, fmt.Sprintf("__injectResult%v", index))
				}

				p.Point = point
				p.Typer = structIdent.Name
				results = append(results, &p)
			}
			intercept.MethodResults = results
			intercept.Results = funcDecl.Type.Results
		}

		intercept.Imports = node.Imports
		interceptors = append(interceptors, intercept)
	}
	return interceptors
}

func GetTyperStructIdent(typer ast.Expr) (*ast.Ident, bool) {
	var structIdent *ast.Ident
	var point bool

	switch typer.(type) {
	case *ast.SelectorExpr:
		selectorExpr := typer.(*ast.SelectorExpr)
		structIdent = selectorExpr.Sel
		point = false
	case *ast.StarExpr:
		structType := typer.(*ast.StarExpr)
		ident, ok := structType.X.(*ast.Ident)
		if !ok {
			selectorExpr, ok := structType.X.(*ast.SelectorExpr)
			if !ok {
				return nil, false
			}
			structIdent = selectorExpr.Sel
		} else {
			structIdent = ident
		}
		point = true
	case *ast.Ident:
		structIdent = typer.(*ast.Ident)
	}

	return structIdent, point
}

func Gen(path string, afterPath string, packet string, interceptors []*Interceptor) map[string]*ast.ImportSpec {
	var needImportMap = map[string]*ast.ImportSpec{}

	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		log.Fatalf("gen parser.ParseFile: %v failed. error: %v", path, err)
	}

	if node.Doc != nil && len(node.Doc.List) > 0 && strings.HasPrefix(node.Doc.List[0].Text, DocPrefix) {
		return needImportMap
	}

	var nodeImportMap = map[string]*ast.ImportSpec{}
	for _, importInfo := range node.Imports {
		nodeImportMap[importInfo.Path.Value] = importInfo
	}

	var change = false
	for _, nodeFunc := range GenInterceptor(node) {
		for _, interceptor := range interceptors {
			if interceptor.Body == nil || len(interceptor.Body.List) == 0 ||
				nodeFunc.Body == nil || len(nodeFunc.Body.List) == 0 {
				continue
			}
			if !funcMatch(nodeFunc, interceptor) {
				continue
			}

			var newBody = interceptor.Body.List
			if len(interceptor.MethodResults) > 0 {
				newBody = newBody[:len(newBody)-1]
			}
			newBody = append(newBody, nodeFunc.Body.List...)
			nodeFunc.Body.List = newBody

			if nodeFunc.Results != nil && interceptor.Results != nil {
				for index, result := range nodeFunc.Results.List {
					result.Names = interceptor.Results.List[index].Names
				}
			}

			for _, importInfo := range interceptor.Imports {
				if nodeImportMap[importInfo.Path.Value] != nil {
					continue
				}
				needImportMap[importInfo.Path.Value] = importInfo
			}
			change = true
		}
	}

	if change {
		for key, importInfo := range needImportMap {
			if key == fmt.Sprintf(`"%v"`, packet) {
				continue
			}
			node.Imports = append(node.Imports, importInfo)
			for _, d := range node.Decls {
				d, ok := d.(*ast.GenDecl)
				if !ok || d.Tok != token.IMPORT {
					continue
				}
				d.Specs = append(d.Specs, importInfo)
			}
		}

		file, err := os.Create(afterPath)
		if err != nil {
			log.Fatal(err)
		}
		err = format.Node(file, fileSet, node)
		if err != nil {
			log.Fatal(err)
		}
	}

	return needImportMap
}

func funcMatch(pre *Interceptor, next *Interceptor) bool {
	if pre.StructTypePoint != next.StructTypePoint {
		return false
	}
	if pre.StructType != next.StructType {
		return false
	}
	if pre.StructName != next.StructName {
		return false
	}
	if pre.MethodName != next.MethodName {
		return false
	}
	if !reflect.DeepEqual(pre.Params, next.Params) {
		return false
	}
	if !reflect.DeepEqual(pre.MethodResults, next.MethodResults) {
		return false
	}

	return true
}
