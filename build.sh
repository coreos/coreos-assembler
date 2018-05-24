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

cd /root
git clone https://github.com/coreos/mantle
# for now just build ore, we can add more components as we use them
mantle/build ore
mv mantle/bin/ore /usr/bin
rm mantle -rf
