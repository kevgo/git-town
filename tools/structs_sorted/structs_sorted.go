package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

var (
	// file paths to ignore
	ignorePaths = []string{ //nolint:gochecknoglobals
		"vendor/",
		"tools/structs_sorted/test.go",
	}

	// struct types to ignore
	ignoreTypes = []string{ //nolint:gochecknoglobals
		"BranchSpan",
		"Change",
		"InconsistentChange",
		"Parts",
		"ProdRunner",
	}
)

func main() {
	issues := Issues{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".go") || isIgnoredPath(path) {
			return err
		}
		issues = append(issues, LintFile(path)...)
		return nil
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if len(issues) > 0 {
		fmt.Println(issues)
	}
	os.Exit(len(issues))
}

func LintFile(path string) Issues {
	result := Issues{}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
	if err != nil {
		return result
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typedNode := node.(type) {
		case *ast.TypeSpec:
			result = append(result, lintStructDefinition(typedNode, fileSet)...)
		case *ast.CompositeLit:
			result = append(result, lintStructLiteral(typedNode, fileSet)...)
		}
		return true
	})
	return result
}

func isIgnoredPath(path string) bool {
	for _, ignore := range ignorePaths {
		if strings.HasPrefix(path, ignore) {
			return true
		}
	}
	return false
}
