#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""PyYAML event-stream oracle: syntax/events, not tag/alias value construction.

yaml.parse yields parsing events. It does not build Python objects, resolve
custom tags, or construct application values. Exit 0 if the event stream
completes; YAMLError is reject. Exit 125 (eval.ExitCannotRun) if PyYAML is
not importable, so a missing module is an oracle error, not a reject.
"""
import sys

try:
    import yaml
except ImportError as e:
    print(f"pyyaml_events: cannot import PyYAML with {sys.executable}: {e}", file=sys.stderr)
    sys.exit(125)

try:
    list(yaml.parse(sys.stdin, Loader=yaml.SafeLoader))
except yaml.YAMLError:
    sys.exit(1)
