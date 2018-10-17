#!/usr/bin/bash
set -euo pipefail
dn=$(dirname $0)
srcdir=$(cd ${dn}/.. && pwd)/src

# Verify syntax for sources
for f in $(find ${srcdir} -type f -executable); do
    shebang=$(head -1 $f)
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
done
