#!/usr/bin/bash

# Check if shellcheck is available. If not, disable shellcheck usage.
command -v shellcheck
HASSHELLCHECK=$?
if [[ ${HASSHELLCHECK} -ne 0 ]]; then
    echo "WARNING: shellcheck is not available. Shell script checking is disabled."
fi

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

    # Only use shellcheck if we know the system has it installed
    if [[ ${HASSHELLCHECK} == 0 ]]; then
        if [[ $shebang =~ ^#!/.*/bash.* ]] || [[ $shebang =~ ^#!/.*/env\ bash ]]; then
            shellcheck -x "$f"
            bash -n "$f"
            echo "OK ${f}"
            continue
        fi
    fi
done <  <(find "${srcdir}" -type f -executable -print0)
