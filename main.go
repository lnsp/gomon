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

	"github.com/fsnotify/fsnotify"
)

var (
	pattern = flag.String("p", "*.go", "set the watch pattern")
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
	args := flag.Args()
	cmd := exec.Command(args[0], args[1:]...)
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
		fmt.Fprintln(os.Stderr, "usage: gomon [-r] [-p *.go] cmd [args]")
		return
	}

	// creates a new file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("failed to create watcher: %v", err)
	}
	defer watcher.Close()

	// set up shutdown handler
	done := make(chan os.Signal)
	signal.Notify(done, syscall.SIGTERM)
	signal.Notify(done, syscall.SIGINT)

	// wait for events
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				// ignore errors for now
				cmd.Process.Kill()
				cmd.Process.Wait()
				cmd = parseCommand()
				cmd.Start()
				log.Printf("File %s %v, restarted process %v", event.Name, event.Op, cmd.Args)
			case err := <-watcher.Errors:
				log.Fatalf("failed to watch: %v", err)
			}
		}
	}()

	// out of the box fsnotify can watch a single file, or a single directory
	matches, err := filepath.Glob(*pattern)
	if err != nil {
		log.Fatalf("failed to list files: %v", err)
	}
	for _, file := range matches {
		if err := watcher.Add(file); err != nil {
			log.Fatalf("failed to watch: %v", err)
		}
	}

	// start command
	log.Println("Waiting for file changes ...")
	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to start: %v", err)
	}
	<-done
	cmd.Process.Kill()
}
