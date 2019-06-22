#!/usr/bin/env bash
set -euo pipefail

# We want to make use of the `-x|--external-sources` flag, which was made
# available in v0.4.0.  So check for the flag and disable the use of
# ShellCheck if not present.
HASSHELLCHECK=0
set +eo pipefail
if shellcheck |& grep -q -- --external-sources; then
  HASSHELLCHECK=1
fi
set -eo pipefail

if [[ ${HASSHELLCHECK} -ne 1 ]]; then
    echo -e "WARNING: installed ShellCheck does not support --external-sources. " \
            "Shell script checking is disabled."
fi

f="$1"

shebang=$(head -1 "$f")
if [[ $shebang =~ ^#!/.*/python ]]; then
    python3 -m py_compile "${f}"
    echo "OK ${f}"
fi
if [[ ${HASSHELLCHECK} == 1 ]]; then
    if [[ $shebang =~ ^#!/.*/bash.* ]] || [[ $shebang =~ ^#!/.*/env\ bash ]]; then
        shellcheck -x "$f"
        bash -n "$f"
        echo "OK ${f}"
    fi
fi
touch "$2"
