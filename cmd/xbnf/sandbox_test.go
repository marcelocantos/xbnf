// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRunSandboxHelp(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := capture(t, []string{"sandbox", "-h"})
	if code != 0 {
		t.Fatalf("sandbox -h exit: %d", code)
	}
	out := stdout + stderr
	if !strings.Contains(out, "sandbox") {
		t.Fatalf("sandbox -h missing sandbox: %q", out)
	}
	if !strings.Contains(out, "-port") || !strings.Contains(out, "-bind") {
		t.Fatalf("sandbox -h missing listen flags: %q", out)
	}
}

func TestRunHelpMentionsSandbox(t *testing.T) {
	t.Parallel()
	code, _, stderr := capture(t, []string{"-h"})
	if code != 0 {
		t.Fatalf("help exit: %d", code)
	}
	if !strings.Contains(stderr, "sandbox") {
		t.Fatalf("help missing sandbox: %q", stderr)
	}
}

func TestRunHelpAgentMentionsSandbox(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := capture(t, []string{"-help-agent"})
	if code != 0 {
		t.Fatalf("help-agent exit: %d", code)
	}
	if stderr != "" {
		t.Fatalf("help-agent stderr: %q", stderr)
	}
	if !strings.Contains(stdout, "sandbox") {
		t.Fatalf("help-agent missing sandbox: %q", stdout)
	}
}

func TestRunSandboxUnexpectedArg(t *testing.T) {
	t.Parallel()
	code, _, stderr := capture(t, []string{"sandbox", "nope"})
	if code != 2 {
		t.Fatalf("unexpected arg exit: %d", code)
	}
	if !strings.Contains(stderr, "unexpected argument") {
		t.Fatalf("unexpected arg stderr: %q", stderr)
	}
}

func TestSandboxServesCheatSheet(t *testing.T) {
	t.Parallel()
	base := startSandbox(t)

	res, body := sandboxGet(t, base+"/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET / (follow redirect) status: %d", res.StatusCode)
	}
	if !strings.Contains(body, "Syntax reference") {
		t.Fatalf("GET / missing hub: %q", body)
	}

	res, body = sandboxGet(t, base+"/docs/cheatsheet.html")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cheatsheet status: %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("cheatsheet content-type: %q", ct)
	}
	for _, want := range []string{
		"xbnf language cheat sheet",
		"id=\"sigils\"",
		"First slice",
		"member:\",\"?",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("cheatsheet missing %q", want)
		}
	}
}

func TestSandboxRedirectAndSpec(t *testing.T) {
	t.Parallel()
	base := startSandbox(t)

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("GET / status: %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if loc != "/docs/index.html" {
		t.Fatalf("GET / Location: %q", loc)
	}

	res, body := sandboxGet(t, base+"/docs/xbnf.xbnf")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("spec status: %d", res.StatusCode)
	}
	if !strings.Contains(body, "grammar -> stmt+") {
		t.Fatalf("spec missing grammar production: %q", body[:min(200, len(body))])
	}

	res, body = sandboxGet(t, base+"/docs/examples/json.xbnf")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("json example status: %d", res.StatusCode)
	}
	if !strings.Contains(body, "json -> value") {
		t.Fatalf("json example missing rule")
	}
}

func TestSandboxUnknownPath(t *testing.T) {
	t.Parallel()
	base := startSandbox(t)
	res, _ := sandboxGet(t, base+"/nope")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status: %d", res.StatusCode)
	}
}

func TestSandboxURLLoopback(t *testing.T) {
	t.Parallel()
	addr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:7373")
	if err != nil {
		t.Fatal(err)
	}
	got := sandboxURL(addr)
	if got != "http://127.0.0.1:7373/docs/index.html" {
		t.Fatalf("sandboxURL: %q", got)
	}
}

func TestSandboxSyntaxPage(t *testing.T) {
	t.Parallel()
	base := startSandbox(t)
	res, body := sandboxGet(t, base+"/docs/syntax.html")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("syntax status: %d", res.StatusCode)
	}
	if !strings.Contains(body, "Syntax reference") || !strings.Contains(body, "/run") {
		t.Fatalf("syntax page missing runner: %q", body[:min(200, len(body))])
	}
	res, body = sandboxGet(t, base+"/docs/syntax-clauses.json")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clauses status: %d", res.StatusCode)
	}
	if !strings.Contains(body, `"id": "rule"`) {
		t.Fatalf("clauses missing rule: %s", body[:min(120, len(body))])
	}
}

func TestSandboxRun(t *testing.T) {
	t.Parallel()
	base := startSandbox(t)
	body, err := jsonPOST(t, base+"/run", `{"grammar":"start -> \"hi\" ;\n","input":"hi"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("run: %s", body)
	}
}

func startSandbox(t *testing.T) string {
	t.Helper()
	ln, h, err := listenSandbox("127.0.0.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

func jsonPOST(t *testing.T, url, raw string) (string, error) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(raw))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", errString("status " + res.Status)
	}
	return string(b), nil
}

type errString string

func (e errString) Error() string { return string(e) }

func sandboxGet(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, string(b)
}
