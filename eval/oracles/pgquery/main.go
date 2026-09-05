// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build pgquery

// PostgreSQL raw-parser oracle for T22.2. stdin is SQL; exit 0 accept, else reject.
// Uses libpg_query (the same engine as pg_query_go), not query execution.
//
//	CGO_ENABLED=1 go build -tags pgquery -o pg_query ./eval/oracles/pgquery
package main

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lpg_query
#include <pg_query.h>
#include <stdlib.h>
*/
import "C"
import (
	"io"
	"os"
	"unsafe"
)

func main() {
	src, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	csrc := C.CString(string(src))
	defer C.free(unsafe.Pointer(csrc))
	r := C.pg_query_parse(csrc)
	defer C.pg_query_free_parse_result(r)
	if r.error != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
