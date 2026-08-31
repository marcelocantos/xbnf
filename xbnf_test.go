// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package xbnf

import "testing"

func TestAgentGuidePresent(t *testing.T) {
	t.Parallel()
	if AgentGuide == "" {
		t.Fatal("AgentGuide is empty")
	}
	if Version == "" {
		t.Fatal("Version is empty")
	}
}
