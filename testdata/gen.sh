#!/usr/bin/env bash
# Regenerate synthetic fixtures under testdata/ from sample.tex.
#
# The committed fixtures (sample.synctex.gz, sample.aux, sample.bbl) were
# produced by running this script; re-run it whenever sample.tex changes.
set -euo pipefail

cd "$(dirname "$0")"

if ! command -v pdflatex >/dev/null 2>&1; then
    echo "gen.sh: pdflatex not found in PATH" >&2
    exit 1
fi

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT
cp sample.tex "$workdir/"
(
    cd "$workdir"
    pdflatex -synctex=1 -interaction=nonstopmode sample.tex >/dev/null
)

cp "$workdir/sample.synctex.gz" sample.synctex.gz
echo "regenerated: sample.synctex.gz"
