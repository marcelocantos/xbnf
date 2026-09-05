#!/usr/bin/env python3
# Copyright 2026 Marcelo Cantos
# SPDX-License-Identifier: Apache-2.0
"""PyYAML safe_load: events plus value construction. Not a syntax-only event dump."""
import sys

import yaml

src = sys.stdin.read()
try:
    yaml.safe_load(src)
except yaml.YAMLError:
    sys.exit(1)
