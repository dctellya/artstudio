// Command stubserver runs the vendor stub, or with `ctl <fault>` sets its fault.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dctellya/artstudio/internal/artic/stubserver"
)

const (
	defaultAddr = ":8080"
	ctlURL      = "http://localhost:8080/__ctl"
	ctlTimeout  = 5 * time.Second
	readHeader  = 5 * time.Second
	readTimeout = 10 * time.Second
	idleTimeout = 60 * time.Second
	stopTimeout = 5 * time.Second
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "ctl" {
		if err := ctl(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "stubserver ctl:", err)
			os.Exit(1)
		}
		return
	}
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, "stubserver:", err)
		os.Exit(1)
	}
}

func ctl(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: stubserver ctl <429|503|slow|timeout|clear>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ctlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ctlURL+"?fault="+url.QueryEscape(args[0]), nil) //nolint:gosec // G704: fixed localhost URL, fault is query-escaped
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: fixed localhost URL
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}

func serve() error {
	addr := os.Getenv("STUB_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           stubserver.New(),
		ReadHeaderTimeout: readHeader,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	return srv.Shutdown(sctx)
}
