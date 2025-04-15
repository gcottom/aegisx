package code

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// CodeValidator validates generated Go code before Yaegi execution.
type CodeValidator struct {
	RequiredFunctions []string
	ForbiddenPackages []string
	FormActionPrefix  string
}

// DefaultValidator returns a validator with default rules.
func DefaultValidator(id string) *CodeValidator {
	return &CodeValidator{
		RequiredFunctions: []string{"main", "Shutdown"},
		ForbiddenPackages: []string{"syscall"},
		FormActionPrefix:  fmt.Sprintf("/runtime/%s/", id),
	}
}

// Validate performs all checks on the provided Go code.
func (v *CodeValidator) Validate(code string) error {
	if err := v.checkSyntax(code); err != nil {
		return fmt.Errorf("syntax error: %w", err)
	}
	if err := v.checkPackage(code); err != nil {
		return fmt.Errorf("package error: %w", err)
	}
	if err := v.checkRequiredFunctions(code); err != nil {
		return fmt.Errorf("missing required functions: %w", err)
	}
	if err := v.checkForbiddenPackages(code); err != nil {
		return fmt.Errorf("forbidden packages used: %w", err)
	}
	if err := v.checkFormActionPrefix(code); err != nil {
		return fmt.Errorf("form action routing error: %w", err)
	}
	if err := v.checkHandlerRoot(code); err != nil {
		return fmt.Errorf("handler routing error: %w", err)
	}
	return nil
}

// checkSyntax validates the Go code for syntax errors.
func (v *CodeValidator) checkSyntax(code string) error {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "", code, parser.AllErrors)
	return err
}

// checkPackage ensures that 'package main' is defined.
func (v *CodeValidator) checkPackage(code string) error {
	if !strings.Contains(code, "package main") {
		return fmt.Errorf("missing 'package main'")
	}
	return nil
}

// checkRequiredFunctions ensures required functions exist.
func (v *CodeValidator) checkRequiredFunctions(code string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", code, parser.AllErrors)
	if err != nil {
		return err
	}

	found := map[string]bool{}
	for _, decl := range node.Decls {
		if fn, isFn := decl.(*ast.FuncDecl); isFn {
			found[fn.Name.Name] = true
		}
	}

	for _, req := range v.RequiredFunctions {
		if !found[req] {
			return fmt.Errorf("missing function: %s", req)
		}
	}
	return nil
}

// checkForbiddenPackages ensures no restricted packages are imported.
func (v *CodeValidator) checkForbiddenPackages(code string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", code, parser.ImportsOnly)
	if err != nil {
		return err
	}

	for _, imp := range node.Imports {
		packageName := strings.Trim(imp.Path.Value, `"`)
		for _, forbidden := range v.ForbiddenPackages {
			if packageName == forbidden {
				return fmt.Errorf("forbidden package used: %s", packageName)
			}
		}
	}
	return nil
}

// checkFormActionPrefix ensures all HTML form actions use the correct runtime prefix.
func (v *CodeValidator) checkFormActionPrefix(code string) error {
	for line := range strings.Lines(code) {
		if strings.Contains(line, "<form") && strings.Contains(line, "action=") {
			if !strings.Contains(line, v.FormActionPrefix) {
				return fmt.Errorf("form action must use prefix: %s", v.FormActionPrefix)
			}
		}
	}
	return nil
}

// checkHandlerRoot ensures all handlers are at the root.
func (v *CodeValidator) checkHandlerRoot(code string) error {
	for line := range strings.Lines(code) {
		if strings.Contains(line, ".HandleFunc(") {
			if strings.Contains(line, v.FormActionPrefix) {
				return fmt.Errorf("handler must be at root, but found under runtime prefix: %s", v.FormActionPrefix)
			}
		}
	}
	return nil
}
