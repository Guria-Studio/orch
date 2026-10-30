#!/bin/sh
# docs/launch/measure.sh — re-measure the numbers docs/HN-POST-DRAFT.md cites.
#
# Run from the repo root on the commit you are about to cite. Needs go, rg,
# gh (authenticated, for the release sizes) and python3 (startup timing).
# It builds into a scratch dir and touches nothing in the tree except what
# `make web` rebuilds. Output is the "Sources" table's raw material; paste
# the numbers with the commit it printed first.
set -eu

TAG="${1:-$(gh release list -R Guria-Studio/orch --limit 1 --json tagName -q '.[0].tagName')}"
OUT="${TMPDIR:-/tmp}/orch-measure"
rm -rf "$OUT" && mkdir -p "$OUT/rel"

echo "commit: $(git rev-parse --short HEAD) ($(git log -1 --format=%cs)); $(go version)"
echo "release measured: $TAG"

echo "## released binaries (what install.sh downloads), bytes"
gh release download "$TAG" -R Guria-Studio/orch -D "$OUT/rel" -p "orch_${TAG}_*" -p checksums.txt
(cd "$OUT/rel" && sha256sum -c checksums.txt --ignore-missing >/dev/null && echo "checksums OK")
for f in "$OUT"/rel/orch_*.tar.gz; do
  d="${f%.tar.gz}"; mkdir -p "$d"; tar -xzf "$f" -C "$d" orch
  echo "$(basename "$d")  binary=$(stat -c %s "$d/orch")  archive=$(stat -c %s "$f")"
done

echo "## HEAD built with .goreleaser.yaml's flags, bytes"
make web >/dev/null
for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${t%/*}"; arch="${t#*/}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -ldflags="-s -w -X main.version=measure" -o "$OUT/orch_${os}_${arch}" ./cmd/orch
  echo "$t  $(stat -c %s "$OUT/orch_${os}_${arch}")"
done
echo "operator JS (bytes): $(du -cb internal/dashboard/dist/build/assets/*.js | tail -1 | cut -f1)"
echo "stakeholder JS (bytes): $(du -cb internal/publish/dist/stakeholder/assets/*.js | tail -1 | cut -f1)"

echo "## code"
echo "Go non-test lines: $(rg --files -g '*.go' -g '!*_test.go' internal cmd | xargs cat | wc -l)"
echo "Go test lines:     $(rg --files -g '*_test.go' internal cmd | xargs cat | wc -l)"
echo "Go test functions: $(rg -c '^func Test' --glob '*_test.go' internal cmd | awk -F: '{s+=$2} END{print s}')"
echo "testscript files:  $(rg --files -g '*.txtar' | wc -l)"
echo "web test cases:    $(rg -c '^\s*(it|test)\(' -g '*.test.ts*' web/src | awk -F: '{s+=$2} END{print s}')"
echo "internal packages: $(rg --files -g '*.go' -g '!*_test.go' internal | xargs -n1 dirname | sort -u | wc -l)"
echo "MCP tools:         $(rg -o 'Name:\s*"orch_[a-z_]+"' -N --no-filename internal/mcp | tr -d ' \t' | sort -u | wc -l)"
echo "review rules:      $(rg -c '^[0-9]+\. \*\*' .github/review/CHECKLIST.md)"
echo "go.mod direct requires: $(awk '/^require \(/{f++} f==1&&/^\t/{n++} /^\)/{if(f==1)f++} END{print n}' go.mod)"

echo "## commands (top-level, from the built binary)"
B="$OUT/orch_$(go env GOOS)_$(go env GOARCH)"
echo "visible: $("$B" --help | awk '/Available Commands/{f=1;next} /^$/{f=0} f{print $1}' | rg -v '^(help|completion)$' | wc -l) (excluding help, completion)"
echo "hidden:  $(rg -c 'Hidden:\s*true' internal/cli | awk -F: '{s+=$2} END{print s}')"

echo "## startup (released linux/amd64 binary, ORCH_NO_UPDATE_CHECK=1)"
REL="$OUT/rel/orch_${TAG}_linux_amd64/orch"
ORCH_NO_UPDATE_CHECK=1 "$REL" init "$OUT/p" --template python-api >/dev/null 2>&1
python3 - "$REL" "$OUT/p" <<'EOF'
import os, statistics, subprocess, sys, time
b, cwd = sys.argv[1], sys.argv[2]
env = dict(os.environ, ORCH_NO_UPDATE_CHECK="1")
def m(args, n=50):
    for _ in range(5):
        subprocess.run([b] + args, cwd=cwd, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    ts = []
    for _ in range(n):
        t = time.perf_counter()
        subprocess.run([b] + args, cwd=cwd, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        ts.append((time.perf_counter() - t) * 1000)
    ts.sort()
    return f"median {statistics.median(ts):.1f} ms, p95 {ts[int(n * .95) - 1]:.1f} ms (n={n})"
for a in (["--version"], ["validate"], ["tasks"]):
    print(f"orch {' '.join(a)}: {m(a)}")
EOF
