package util

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/traefik/yaegi/stdlib/unsafe"
)

func NewYaegiInterpreter() (*interp.Interpreter, *bytes.Buffer) {
	outputBuffer := new(bytes.Buffer)
	goPath, err := filepath.Abs(GetYaegiGoPath())
	if err != nil {
		panic(err)
	}
	interpreter := interp.New(interp.Options{Stdout: outputBuffer, Stderr: outputBuffer, GoPath: goPath})
	interpreter.Use(stdlib.Symbols)
	interpreter.Use(unsafe.Symbols)
	return interpreter, outputBuffer
}

func GetYaegiGoPath() string {
	/*goPath := filepath.Join("../", GetAppRoot())
	goPath = filepath.Join(goPath, "yaegiGoPath")
	goPath, err := filepath.Abs(goPath)
	if err != nil {
		panic(err)
	}*/
	goPath := os.Getenv("GOPATH")
	return goPath
}
