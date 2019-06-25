package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/dc0d/dirwatch"
)

var (
	pattern = flag.String("p", ".", "set the watch pattern")
)

func parseFlags() (*exec.Cmd, error) {
	// set up flags
	flag.Parse()

	// set up command args
	if flag.NArg() < 1 {
		return nil, errors.New("missing arguments")
	}
	return parseCommand(), nil
}

func parseCommand() *exec.Cmd {
	args := append([]string{"run"}, flag.Args()...)
	cmd := exec.Command("go", args...)
	cmd.Stdout = &linePrefixWriter{Prefix: []byte("[stdout] "), Output: os.Stdout}
	cmd.Stderr = &linePrefixWriter{Prefix: []byte("[stderr] "), Output: os.Stderr}
	return cmd
}

type linePrefixWriter struct {
	Prefix       []byte
	Output       io.Writer
	ExistingLine bool
}

func (w *linePrefixWriter) Write(p []byte) (n int, err error) {
	for i, b := range p {
		if !w.ExistingLine {
			w.Output.Write(w.Prefix)
			w.ExistingLine = true
		}
		if n, err := w.Output.Write(p[i : i+1]); n == 0 || err != nil {
			return i + n, err
		}
		switch b {
		case '\n':
			w.ExistingLine = false
		}
	}
	return len(p), nil
}

func main() {
	cmd, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage: gomon [gofile] [args]")
		return
	}

	// set up shutdown handler
	done := make(chan os.Signal)
	signal.Notify(done, syscall.SIGTERM)
	signal.Notify(done, syscall.SIGINT)

	changed := func(event dirwatch.Event) {
		// ignore errors for now
		cmd.Process.Kill()
		cmd.Process.Wait()
		cmd = parseCommand()
		cmd.Start()
		log.Printf("File %s %v, restarted process %v", event.Name, event.Op, cmd.Args)
	}
	watcher := dirwatch.New(dirwatch.Notify(changed))
	defer watcher.Stop()

	// out of the box fsnotify can watch a single file, or a single directory
	matches, err := filepath.Glob(*pattern)
	if err != nil {
		log.Fatalf("failed to list files: %v", err)
	}
	for _, file := range matches {
		watcher.Add(file, true)
	}

	// start command
	log.Println("Waiting for file changes ...")
	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to start: %v", err)
	}
	<-done
	cmd.Process.Kill()
}
