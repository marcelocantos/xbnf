#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""CPython ast.parse oracle: syntax only, no execution."""
import ast
import sys

src = sys.stdin.read()
try:
    ast.parse(src)
except SyntaxError:
    sys.exit(1)
