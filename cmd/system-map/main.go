package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const systemMapSchemaVersion = "1"

type SystemMap struct {
	SchemaVersion    string            `json:"schema_version"`
	Commands         []CommandSurface  `json:"commands"`
	HTTPRoutes       []HTTPRoute       `json:"http_routes"`
	SourceOperations []SourceOperation `json:"source_operations"`
	DurableObjects   []DurableObject   `json:"durable_objects"`
}

type CommandSurface struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type HTTPRoute struct {
	Path          string `json:"path"`
	File          string `json:"file"`
	Line          int    `json:"line"`
	Authenticated bool   `json:"authenticated"`
}

type SourceOperation struct {
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type DurableObject struct {
	Name string `json:"name"`
	File string `json:"file"`
	Line int    `json:"line"`
}

func main() {
	root := flag.String("root", ".", "repository root")
	out := flag.String("out", "", "output path; stdout when empty")
	flag.Parse()

	systemMap, err := GenerateSystemMap(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	payload, err := json.MarshalIndent(systemMap, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	payload = append(payload, '\n')
	if strings.TrimSpace(*out) == "" {
		_, _ = os.Stdout.Write(payload)
		return
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, payload, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func GenerateSystemMap(root string) (SystemMap, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return SystemMap{}, err
	}
	var goFiles []string
	if err := filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "frontend", "node_modules", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			goFiles = append(goFiles, path)
		}
		return nil
	}); err != nil {
		return SystemMap{}, err
	}
	sort.Strings(goFiles)

	systemMap := SystemMap{SchemaVersion: systemMapSchemaVersion}
	routeSeen := map[string]bool{}
	operationSeen := map[string]bool{}
	objectSeen := map[string]bool{}

	for _, path := range goFiles {
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return SystemMap{}, err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "cmd/") && strings.HasSuffix(rel, "/main.go") {
			parts := strings.Split(rel, "/")
			if len(parts) >= 3 {
				systemMap.Commands = append(systemMap.Commands, CommandSurface{Name: parts[1], Path: rel})
			}
		}
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return SystemMap{}, fmt.Errorf("parse %s: %w", rel, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.BasicLit:
				if typed.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(typed.Value)
				if err != nil {
					return true
				}
				line := fileSet.Position(typed.Pos()).Line
				if isHTTPRouteLiteral(value) {
					key := value + "\x00" + rel
					if !routeSeen[key] {
						routeSeen[key] = true
						systemMap.HTTPRoutes = append(systemMap.HTTPRoutes, HTTPRoute{
							Path:          value,
							File:          rel,
							Line:          line,
							Authenticated: isAuthenticatedRoute(value),
						})
					}
				}
				if isSourceOperationLiteral(value) {
					key := value + "\x00" + rel
					if !operationSeen[key] {
						operationSeen[key] = true
						systemMap.SourceOperations = append(systemMap.SourceOperations, SourceOperation{Name: value, File: rel, Line: line})
					}
				}
			case *ast.TypeSpec:
				if _, ok := typed.Type.(*ast.StructType); ok && isDurableObjectName(typed.Name.Name) {
					key := typed.Name.Name + "\x00" + rel
					if !objectSeen[key] {
						objectSeen[key] = true
						systemMap.DurableObjects = append(systemMap.DurableObjects, DurableObject{
							Name: typed.Name.Name,
							File: rel,
							Line: fileSet.Position(typed.Pos()).Line,
						})
					}
				}
			}
			return true
		})
	}
	sort.Slice(systemMap.Commands, func(i, j int) bool {
		if systemMap.Commands[i].Name != systemMap.Commands[j].Name {
			return systemMap.Commands[i].Name < systemMap.Commands[j].Name
		}
		return systemMap.Commands[i].Path < systemMap.Commands[j].Path
	})
	sort.Slice(systemMap.HTTPRoutes, func(i, j int) bool {
		if systemMap.HTTPRoutes[i].Path != systemMap.HTTPRoutes[j].Path {
			return systemMap.HTTPRoutes[i].Path < systemMap.HTTPRoutes[j].Path
		}
		return systemMap.HTTPRoutes[i].File < systemMap.HTTPRoutes[j].File
	})
	sort.Slice(systemMap.SourceOperations, func(i, j int) bool {
		if systemMap.SourceOperations[i].Name != systemMap.SourceOperations[j].Name {
			return systemMap.SourceOperations[i].Name < systemMap.SourceOperations[j].Name
		}
		return systemMap.SourceOperations[i].File < systemMap.SourceOperations[j].File
	})
	sort.Slice(systemMap.DurableObjects, func(i, j int) bool {
		if systemMap.DurableObjects[i].Name != systemMap.DurableObjects[j].Name {
			return systemMap.DurableObjects[i].Name < systemMap.DurableObjects[j].Name
		}
		return systemMap.DurableObjects[i].File < systemMap.DurableObjects[j].File
	})
	return systemMap, nil
}

func isHTTPRouteLiteral(value string) bool {
	return value == "/health" || strings.HasPrefix(value, "/api/")
}

func isAuthenticatedRoute(value string) bool {
	return strings.HasPrefix(value, "/api/")
}

func isSourceOperationLiteral(value string) bool {
	switch value {
	case "sync_articles", "sync_media", "sync_content", "existing_articles", "discover_articles":
		return true
	default:
		return false
	}
}

func isDurableObjectName(name string) bool {
	suffixes := []string{"Envelope", "Feedback", "Manifest", "Package", "Record", "Report", "Run", "Subscription", "Task", "Version"}
	prefixes := []string{"BookKnowledge", "Knowledge", "Source", "WeChat", "WCPlus"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
