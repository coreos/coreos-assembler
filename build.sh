#!/usr/bin/bash
set -xeuo pipefail
mkdir -p /usr/app/
cd /usr/app/
git clone https://github.com/ostreedev/ostree-releng-scripts
cd /root/src
ls -al
cargo build --release
mv target/release/coreos-assembler /usr/bin
rm target -rf
