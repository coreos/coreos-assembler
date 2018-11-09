#!/usr/bin/bash
set -euo pipefail
dn=$(dirname "$0")
srcdir=$(cd "${dn}"/.. && pwd)/src

# Verify syntax for sources
# see https://github.com/koalaman/shellcheck/wiki/SC2044
# for explanation of this use of while
while IFS= read -r -d '' f
do
    shebang=$(head -1 "$f")
    if [[ $shebang =~ ^#!/.*/python ]]; then
        python3 -m py_compile "${f}"
        echo "OK ${f}"
        continue
    fi
    if [[ $shebang =~ ^#!/.*/bash.* ]] || [[ $shebang =~ ^#!/.*/env\ bash ]]; then
        bash -n "$f"
        echo "OK ${f}"
        continue
    fi
done <  <(find "${srcdir}" -type f -executable -print0)
