// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package xbnf

import "embed"

// Version is set at link time from git describe.
var Version = "dev"

//go:embed agents-guide.md
var AgentGuide string

// Docs is the sandbox content: cheat sheet, spec, plan, and examples.
//
//go:embed docs
var Docs embed.FS
