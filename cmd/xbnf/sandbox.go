// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/marcelocantos/xbnf"
)

const (
	sandboxBindDefault = "127.0.0.1"
	sandboxPortDefault = 7373
)

func newSandboxMux() (http.Handler, error) {
	sub, err := fs.Sub(xbnf.Docs, "docs")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/docs/", http.StripPrefix("/docs/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/docs/cheatsheet.html", http.StatusFound)
	})
	return mux, nil
}

func listenSandbox(bind string, port int) (net.Listener, http.Handler, error) {
	h, err := newSandboxMux()
	if err != nil {
		return nil, nil, err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		return nil, nil, err
	}
	return ln, h, nil
}

func sandboxURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://127.0.0.1/docs/cheatsheet.html"
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/docs/cheatsheet.html"
}

func runSandbox(bind string, port int) int {
	ln, h, err := listenSandbox(bind, port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xbnf sandbox:", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "xbnf sandbox:", sandboxURL(ln.Addr()))
	err = http.Serve(ln, h)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, "xbnf sandbox:", err)
		return 1
	}
	return 0
}
