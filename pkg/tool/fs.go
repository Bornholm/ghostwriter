package tool

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bornholm/genai/llm"
	"github.com/gobwas/glob"
	"github.com/pkg/errors"
)

func NewFSTools(root fs.FS, ignoredFiles ...string) []llm.Tool {
	return []llm.Tool{
		NewReadDirTool(root, ignoredFiles...),
		NewReadFileTool(root),
	}
}

func NewReadDirTool(root fs.FS, ignoredFiles ...string) *llm.FuncTool {
	return llm.NewFuncTool(
		"readdir",
		"list files in the given directory",
		llm.NewJSONSchema().
			RequiredProperty("path", "the path to the directory", "string"),
		func(ctx context.Context, params map[string]any) (string, error) {
			path, err := llm.ToolParam[string](params, "path")
			if err != nil {
				return "", errors.WithStack(err)
			}

			tree, err := GenerateDirectoryTree(root, path, ignoredFiles...)
			if err != nil {
				return "", errors.WithStack(err)
			}

			var sb strings.Builder

			sb.WriteString("**Directory Tree**:\n\n")
			sb.WriteString(tree)

			return sb.String(), nil
		},
	)
}

func NewReadFileTool(root fs.FS) *llm.FuncTool {
	return llm.NewFuncTool(
		"readfile",
		"read a file",
		llm.NewJSONSchema().
			RequiredProperty("path", "the path to the file", "string"),
		func(ctx context.Context, params map[string]any) (string, error) {
			path, err := llm.ToolParam[string](params, "path")
			if err != nil {
				return "", errors.WithStack(err)
			}

			data, err := fs.ReadFile(root, path)
			if err != nil {
				return "", errors.WithStack(err)
			}

			return string(data), nil
		},
	)
}

func GenerateDirectoryTree(root fs.FS, path string, ignoredFiles ...string) (string, error) {
	var sb strings.Builder
	var initialPath string

	ignoredPatterns := make([]glob.Glob, 0)
	for _, p := range ignoredFiles {
		pattern, err := glob.Compile(p)
		if err != nil {
			return "", errors.WithStack(err)
		}

		ignoredPatterns = append(ignoredPatterns, pattern)
	}

	err := fs.WalkDir(root, path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.WithStack(err)
		}

		if path == "." || path == initialPath {
			return nil
		}

		for _, p := range ignoredPatterns {
			if p.Match(path) {
				if d.IsDir() {
					return fs.SkipDir
				}

				return nil
			}
		}

		relPath, err := filepath.Rel(initialPath, path)
		if err != nil {
			return errors.WithStack(err)
		}
		depth := strings.Count(relPath, string(os.PathSeparator))

		sb.WriteString(strings.Repeat("  ", depth))

		sb.WriteString("|- ")
		sb.WriteString(d.Name())
		sb.WriteString(" (")
		if d.IsDir() {
			sb.WriteString("directory")
		} else {
			sb.WriteString("file")
		}
		sb.WriteString(", ")
		sb.WriteString(d.Type().String())
		sb.WriteString(")")
		sb.WriteString("\n")

		return nil
	})
	if err != nil {
		return "", errors.WithStack(err)
	}

	return sb.String(), nil
}
