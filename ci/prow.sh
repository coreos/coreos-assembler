#!/bin/bash
set -xeuo pipefail
make -j 4
make check
