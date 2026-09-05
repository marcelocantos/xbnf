#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""CPython compile oracle: syntax only, no execution.

Reads stdin as bytes so encoding cookies are honoured (unknown encodings
are SyntaxError). Does not import, exec, or produce bytecode for running.
"""
import sys

src = sys.stdin.buffer.read()
try:
    compile(src, "<stdin>", "exec")
except (SyntaxError, ValueError, UnicodeDecodeError):
    sys.exit(1)

