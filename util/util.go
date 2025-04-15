package util

import (
	"fmt"
	"go/parser"
	"go/token"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/traefik/yaegi/stdlib"
	"github.com/traefik/yaegi/stdlib/unsafe"
)

func ExtractGoCode(response string) string {
	// Regular expression to match Go code blocks (```go ... ```)
	codeBlockRegex := regexp.MustCompile("(?s)```go\\n(.*?)```")

	// Try to find Go code inside markdown-style code blocks
	matches := codeBlockRegex.FindStringSubmatch(response)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]) // Extracted Go code
	}

	// If no markdown code block found, try to detect Go functions manually
	altRegex := regexp.MustCompile(`(?s)(package\s+\w+.*?func\s+\w+$begin:math:text$.*?$end:math:text$\s*{.*})`)
	altMatches := altRegex.FindStringSubmatch(response)
	if len(altMatches) > 1 {
		return strings.TrimSpace(altMatches[1])
	}

	// Return full response if no Go code found (fallback case)
	return response
}

func ExtractPort(logs string) int {
	lines := strings.Split(logs, "\n")
	for _, line := range lines {
		if strings.Contains(line, "PORT=") {
			portStr := strings.Split(line, "=")[1]
			portStr = strings.TrimPrefix(portStr, ":")
			port, err := strconv.Atoi(portStr)
			if err == nil {
				return port
			}
		} else if strings.Contains(line, ":") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				portStr := strings.TrimSpace(parts[len(parts)-1])
				port, err := strconv.Atoi(portStr)
				if err == nil {
					return port
				}
			}
		}
	}
	return 0
}

func GetAppRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for wd != "/" {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		wd = filepath.Dir(wd)
	}

	panic("Project root not found")
}

// ExtractImports parses Go code and returns a list of imported packages.
func ExtractImports(code string) []string {
	fset := token.NewFileSet()

	// Parse the code into an AST
	node, err := parser.ParseFile(fset, "", code, parser.ImportsOnly)
	if err != nil {
		return []string{}
	}

	var imports []string
	for _, imp := range node.Imports {
		packageName := strings.Trim(imp.Path.Value, `"`)
		if !IsStandardPackage(packageName) {
			imports = append(imports, packageName)
		}
	}

	return imports
}

// isStandardPackage checks if a package belongs to the Go standard library.
func IsStandardPackage(pkg string) bool {
	// List of Go standard library packages - shortened here for brevity.
	for j, _ := range stdlib.Symbols {
		if pkg == j {
			return true
		}
		k := strings.Split(j, "/")
		if len(k) > 1 && k[0] == k[1] && k[0] == pkg {
			return true
		}
	}
	for j, _ := range unsafe.Symbols {
		if pkg == j {
			return true
		}
		k := strings.Split(j, "/")
		if len(k) > 1 && k[0] == k[1] && k[0] == pkg {
			return true
		}
	}
	return false
}

// DownloadNonStandardPackages downloads all non-standard imports.
func DownloadNonStandardPackages(code string, targetDir string) error {
	packages := ExtractImports(code)
	if len(packages) == 0 {
		fmt.Println("No non-standard packages to download.")
		return nil
	}

	for _, pkg := range packages {
		fmt.Printf("Downloading package: %s\n", pkg)
		if err := DownloadPackage(pkg, targetDir); err != nil {
			return fmt.Errorf("failed to download package %s: %w", pkg, err)
		}
	}
	return nil
}

// DownloadPackage downloads a Go package into a specified directory.
func DownloadPackage(pkg, targetDir string) error {
	// Ensure the target directory exists
	if err := os.MkdirAll(filepath.Join(targetDir, "src"), 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Save the original GOPATH
	originalGoPath := os.Getenv("GOPATH")

	// Set the environment variables for the command
	cmd := exec.Command("go", "get", pkg)
	cmd.Env = append(os.Environ(), "GOPATH="+targetDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the command to download the package
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to download package %s: %w", pkg, err)
	}

	// Restore the original GOPATH
	if err := os.Setenv("GOPATH", originalGoPath); err != nil {
		return fmt.Errorf("failed to restore original GOPATH: %w", err)
	}

	return nil
}

func RuntimeHealthCheck(runtimeID string) bool {
	log.Println("Performing health check for runtime:", runtimeID)
	res, err := http.Get(fmt.Sprintf("http://localhost:8080/runtime/%s", runtimeID))
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// RemoveItem removes the first occurrence of an item from a slice of strings.
func RemoveItem(slice []string, item string) []string {
	for i, v := range slice {
		if v == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
