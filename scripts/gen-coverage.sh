#!/bin/bash
# Per-test coverage generation: 為每一個 Test* 各跑一次 go test -coverprofile=
# 並把輸出轉成 LCOV（TN= 為該 test 名）合併成一份 cover.lcov，最後印出檔案路徑。
#
# 預設覆蓋全 repo（TARGETS=./...）。覆寫 TARGETS 環境變數可縮減範圍：
#   TARGETS="./internal/report/..." bash scripts/gen-coverage.sh
#
# Output:
#   /tmp/rg-cov/all.lcov
set -euo pipefail
cd "$(dirname "$0")/.."

TARGETS=${TARGETS:-"./..."}
OUTDIR=/tmp/rg-cov
LCOV=$OUTDIR/all.lcov

rm -rf "$OUTDIR"
mkdir -p "$OUTDIR"
: > "$LCOV"

# build cover2lcov if missing
if [ ! -x ./bin/cover2lcov ]; then
  go build -o ./bin/cover2lcov ./cmd/cover2lcov
fi

# 把 ./... 之類的 pattern 展開成具體 package 列表，避免內層 go test -run
# 在每個 package 都重跑同名 test
PACKAGES=$(go list "$TARGETS")

for pkg in $PACKAGES; do
  tests=$(go test -list '^Test' "$pkg" 2>/dev/null | grep '^Test' || true)
  for test in $tests; do
    safe=$(echo "$pkg-$test" | tr '/.' '--')
    out="$OUTDIR/${safe}.out"
    # -timeout=30s 防個別 test 卡住整個流程
    if go test -run "^${test}\$" -timeout=30s -coverprofile="$out" "$pkg" >/dev/null 2>&1; then
      ./bin/cover2lcov -tn="$test" "$out" >> "$LCOV" || true
    fi
  done
done

echo "$LCOV"
