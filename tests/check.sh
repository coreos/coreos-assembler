#!/usr/bin/env bash
set -euo pipefail
dn=$(dirname "$0")
srcdir=$(cd "${dn}"/.. && pwd)

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

# Build the list of files to syntax check
# The whitelist of directories is:
#  - ./
#  - ./src
#  - ./tests
#
tmpdir=$(mktemp -d)
find "${srcdir}" -maxdepth 1 -type f -executable -print > "${tmpdir}/files"
find "${srcdir}/src" -maxdepth 1 -type f -executable -print >> "${tmpdir}/files"
find "${srcdir}/tests" -maxdepth 1 -type f -executable -print >> "${tmpdir}files"

# Verify syntax for sources
# see https://github.com/koalaman/shellcheck/wiki/SC2044
# for explanation of this use of while
while IFS= read -r f
do
    shebang=$(head -1 "$f")
    if [[ $shebang =~ ^#!/.*/python ]]; then
        python3 -m py_compile "${f}"
        echo "OK ${f}"
        continue
    fi
    if [[ ${HASSHELLCHECK} == 1 ]]; then
        if [[ $shebang =~ ^#!/.*/bash.* ]] || [[ $shebang =~ ^#!/.*/env\ bash ]]; then
            shellcheck -x "$f"
            bash -n "$f"
            echo "OK ${f}"
            continue
        fi
    fi
done < "${tmpdir}/files"
rm -rf "${tmpdir}"
