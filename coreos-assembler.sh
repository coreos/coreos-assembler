#!/bin/bash
# This used to be a separate program, but is now
# merged into rpm-ostree: https://github.com/projectatomic/rpm-ostree/pull/1377
exec rpm-ostree compose tree "$@"
