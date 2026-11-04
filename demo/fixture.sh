#!/bin/sh
# Builds the demo's repository and gr database, apart from yours.
#
#   fixture.sh <dir>
#   fixture.sh <dir> fix
#
# Creates <dir>/repo, a branch "feature" of two commits on main, and <dir>/env.sh, which points gr,
# git, and the shell at <dir> alone, and gives the shell fix, which runs this with fix. With fix, commits the fix an agent makes for the review. Dates
# are fixed, so every run makes the same commits.
set -eu

dir=${1:?dir}

tick=0
commit() {
	tick=$((tick + 1))
	date="2026-01-01T00:00:${tick}Z"
	git -C "$repo" add -A
	GIT_AUTHOR_DATE=$date GIT_COMMITTER_DATE=$date git -C "$repo" commit -q -m "$1"
}

if [ "${2:-}" = fix ]; then
	. "$dir/env.sh"
	repo=$dir/repo
	tick=10
	python3 - "$repo/stats.py" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace("def mean(xs):\n", "def mean(xs):\n    if not xs:\n        return 0.0\n")
open(p, "w").write(s)
PY
	GIT_AUTHOR_NAME=Claude GIT_AUTHOR_EMAIL=noreply@anthropic.com commit "Return 0.0 from mean for an empty list"
	# demo.tape types the fix's hash, as an agent would.
	got=$(git -C "$repo" rev-parse --short HEAD)
	if [ "$got" != a360752 ]; then
		echo "the fix is $got, not a360752: update demo.tape and this check" >&2
		exit 1
	fi
	exit
fi

rm -rf "$dir"
mkdir -p "$dir"
dir=$(cd "$dir" && pwd)
repo=$dir/repo

cat >"$dir/env.sh" <<EOF
export GR_DB="$dir/gr.db"
export GR_CONFIG="$dir/config.toml"
export GR_GLYPHS=unicode
export TZ=UTC
export XDG_STATE_HOME="$dir/state"
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_NOSYSTEM=1
export PATH="$PWD/bin:\$PATH"
unset GR_AUTHOR CLAUDECODE
fix() { sh "$(cd "$(dirname "$0")" && pwd)/fixture.sh" "$dir" fix; }
EOF
printf "[ui]\nsidebar_width = 36\n" >"$dir/config.toml"
. "$dir/env.sh"

git init -q -b main "$repo"
git -C "$repo" config user.name "Pat Reviewer"
git -C "$repo" config user.email pat@example.com
cat >"$repo/README.md" <<'EOF'
# stats

Small statistics helpers.
EOF
commit "Initial commit"

git -C "$repo" checkout -q -b feature
cat >"$repo/stats.py" <<'EOF'
"""Small statistics helpers."""


def mean(xs):
    total = 0
    for x in xs:
        total += x
    return total / len(xs)


def median(xs):
    s = sorted(xs)
    n = len(s)
    mid = n // 2
    if n % 2:
        return s[mid]
    return (s[mid - 1] + s[mid]) / 2
EOF
commit "Add mean and median"
cat >"$repo/report.py" <<'EOF'
"""Print a summary of a list of numbers."""

from stats import mean, median


def summary(xs):
    return f"mean={mean(xs)} median={median(xs)}"


if __name__ == "__main__":
    print(summary([1, 2, 3, 4]))
EOF
commit "Add a summary report"
