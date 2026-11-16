// Command grasp-desktop runs the desktop image's helpers:
//
//	grasp-desktop launcher [-addr 127.0.0.1:6091]   launch page + POST /launch
//	grasp-desktop mcp                               desktop MCP on stdio
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"backend/internal/desktop"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: grasp-desktop launcher|mcp")
		return 2
	}
	paths := desktop.DefaultPaths()
	runner := desktop.ExecRunner{}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	switch args[0] {
	case "launcher":
		fs := flag.NewFlagSet("launcher", flag.ContinueOnError)
		fs.SetOutput(stderr)
		addr := fs.String("addr", "127.0.0.1:"+strconv.Itoa(desktop.LauncherPort), "listen address (loopback only)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if err := serveLauncher(ctx, *addr, paths, runner); err != nil {
			_, _ = fmt.Fprintln(stderr, "grasp-desktop launcher:", err)
			return 1
		}
		return 0
	case "mcp":
		srv := &desktop.MCPServer{
			Paths:    paths,
			Control:  desktop.NewControlStore(paths, nil),
			Runner:   runner,
			LookPath: exec.LookPath,
		}
		if err := srv.Serve(ctx, stdin, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "grasp-desktop mcp:", err)
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintln(stderr, "unknown subcommand:", args[0])
		return 2
	}
}

func serveLauncher(ctx context.Context, addr string, paths desktop.Paths, runner desktop.Runner) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("refusing non-loopback address %s", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	l := &desktop.Launcher{Paths: paths, Runner: runner, LookPath: exec.LookPath, Port: port}
	srv := &http.Server{Handler: l.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
