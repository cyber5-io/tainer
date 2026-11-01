package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyber5-io/tainer/pkg/tainer/runtime"
	"github.com/cyber5-io/tainer/pkg/tainer/tui"
)

// watchMaxLine bounds a single relayed NDJSON record.
const watchMaxLine = 4 << 20

// cmdWatch implements `tainer watch --json`: a verbatim relay of the stack's
// event stream. There is no human mode and no auto-reconnect.
func cmdWatch(args []string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	open := func(ctx context.Context) (io.ReadCloser, error) {
		eng, err := runtime.Engine(ctx, runtime.Options{AutoStart: true})
		if err != nil {
			return nil, engineErr(err)
		}
		// The client lives as long as the stream; process exit reclaims it.
		return eng.EventsStream(ctx)
	}
	exitFn(runWatch(ctx, args, open, os.Stdout, os.Stderr))
}

// runWatch is cmdWatch with its I/O injected; it returns the process exit code.
func runWatch(ctx context.Context, args []string, open func(context.Context) (io.ReadCloser, error), out, errOut io.Writer) int {
	flags, _ := tui.ParseOutputFlags(args)
	if !flags.JSON {
		fmt.Fprintln(errOut, "tainer watch requires --json")
		return 1
	}
	jsonActive = true

	rc, err := open(ctx)
	if err != nil {
		return failTo(out, errCode(err), err.Error())
	}
	defer rc.Close()

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64<<10), watchMaxLine)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		line = append(line, 0x0a)
		if _, err := out.Write(line); err != nil {
			return 1
		}
	}
	if ctx.Err() != nil {
		return 0
	}
	msg := "the event stream ended"
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		msg = fmt.Sprintf("the event stream ended: %v", err)
	}
	return failTo(out, "watch-ended", msg)
}

// failTo writes the v1 error envelope to w and returns exit status 1.
func failTo(w io.Writer, code, message string) int {
	env := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	env.Error.Code = code
	env.Error.Message = message
	_ = jsonLine(w, env)
	return 1
}
