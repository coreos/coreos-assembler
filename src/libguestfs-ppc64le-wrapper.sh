#!/bin/bash
exec qemu-system-ppc64 "$@" -machine pseries,accel=kvm:tcg,vsmt=8,cap-fwnmi=off
