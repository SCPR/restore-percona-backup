package main

import (
	"bufio"
	"flag"
	"io"
	_ "io/ioutil"
	_ "log"
	"os"
	"path"
)

func main() {
	var (
		_   = flag.Bool("x", true, "Extract")
		dir = flag.String("C", "", "Output dir")
	)

	flag.Parse()

	// write stdin to "fake-output" in dir
	f, err := os.Create(path.Join(*dir, "fake-output"))
	if err != nil {
		panic(err)
	}

	defer f.Close()

	stdin := bufio.NewReader(os.Stdin)

	io.Copy(f, stdin)
}
