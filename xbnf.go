// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package xbnf

import _ "embed"

// Version is set at link time from git describe.
var Version = "dev"

//go:embed agents-guide.md
var AgentGuide string
