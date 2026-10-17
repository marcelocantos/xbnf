// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"

	"github.com/marcelocantos/xbnf/engine"
	"github.com/marcelocantos/xbnf/syntax"
)

const maxRunBody = 1 << 20

type runRequest struct {
	Grammar string `json:"grammar"`
	Input   string `json:"input"`
	Start   string `json:"start"`
	// InputB64, when set, replaces Input with raw bytes, so corpus files that
	// are not valid UTF-8 reach the engine unchanged (as in xbnf eval).
	InputB64 string `json:"input_b64"`
}

func handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBody)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req runRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "json", http.StatusBadRequest)
		return
	}
	if req.InputB64 != "" {
		b, err := base64.StdEncoding.DecodeString(req.InputB64)
		if err != nil {
			http.Error(w, "input_b64", http.StatusBadRequest)
			return
		}
		req.Input = string(b)
	}
	g, err := syntax.Parse([]byte(req.Grammar))
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(w).Encode(engine.Result{Error: err.Error()})
		return
	}
	res := engine.Parse(g, req.Start, req.Input)
	_ = json.NewEncoder(w).Encode(res)
}
