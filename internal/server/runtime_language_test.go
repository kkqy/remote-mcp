package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// 仅检查生产字符串字面量（包含 jsonschema 标签）；中文注释和测试中的用户内容不受限制。
func TestRuntimeStringsUseEnglish(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, directory := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			positions := token.NewFileSet()
			file, err := parser.ParseFile(positions, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Errorf("生产字符串解析失败：%s", positions.Position(literal.Pos()))
					return true
				}
				for _, character := range value {
					if unicode.Is(unicode.Han, character) {
						// 只输出位置，不把可能的字面量内容复制进诊断。
						t.Errorf("运行字符串须使用英文：%s", positions.Position(literal.Pos()))
						break
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal("生产源码语言检查失败", err)
		}
	}
}
