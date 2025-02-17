package main

import "github.com/gcottom/aegisx/server"

func main() {
	err := server.Run()
	if err != nil {
		panic(err)
	}
}
