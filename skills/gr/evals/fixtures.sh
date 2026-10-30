#!/usr/bin/env bash
# Builds a scratch repository and gr database for one eval scenario.
#
#   fixtures.sh <scenario> <dir>
#
# Creates <dir>/repo, <dir>/gr.db, and <dir>/fixture-head, the commit the agent starts from. Run gr
# against it with GR_DB=<dir>/gr.db. The human is the repository's git identity, "Pat Reviewer"; an
# agent's earlier replies are by "Claude".
#
# Scenarios:
#   address       a submitted review with three line threads and a summary ask
#   second-round  address after a round of fixes, with one fixed thread reopened
#   resolve       address after a round of fixes, ready for the human's final pass
#   review        an unreviewed branch with two off-by-one bugs and a vague commit message
set -euo pipefail

scenario=${1:?scenario}
dir=${2:?dir}
gr=${GR_BIN:-gr}

mkdir -p "$dir"
dir=$(cd "$dir" && pwd)
repo=$dir/repo
export GR_DB=$dir/gr.db
unset GR_AUTHOR

tick=0
commit() {
	tick=$((tick + 1))
	local date="2026-01-01T00:00:${tick}Z"
	git -C "$repo" add -A
	GIT_AUTHOR_DATE=$date GIT_COMMITTER_DATE=$date git -C "$repo" commit -q -m "$1"
}

sha() {
	git -C "$repo" rev-parse --short "$1"
}

grr() {
	"$gr" -C "$repo" "$@" >/dev/null
}

init() {
	rm -rf "$repo" "$GR_DB"
	git init -q -b main "$repo"
	git -C "$repo" config user.name "Pat Reviewer"
	git -C "$repo" config user.email pat@example.com
	cat >"$repo/README.md" <<'EOF'
# stats

Small statistics helpers.
EOF
	commit "Initial commit"
}

feature() {
	git -C "$repo" checkout -q -b feature
	cat >"$repo/stats.py" <<'EOF'
"""Small statistics helpers."""


def mean(xs):
    tmp = 0
    for x in xs:
        tmp += x
    return tmp / len(xs)


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
    if xs is None:
        return "no data"
    return f"mean={mean(xs)} median={median(xs)}"


if __name__ == "__main__":
    print(summary([1, 2, 3, 4]))
    print(summary(None))
EOF
	commit "Add a summary report"
}

# reviewed submits the human's review of the feature branch: threads 1 to 3 and summary r1.
reviewed() {
	local c1 c2
	c1=$(sha HEAD~1)
	c2=$(sha HEAD)
	grr comment --commit "$c1" -p stats.py -l 8 \
		-m "This raises ZeroDivisionError for an empty list. Return 0.0 for an empty list instead."
	grr comment --commit "$c1" -p stats.py -l 5-7 -m "Rename \`tmp\` to \`total\`."
	grr comment --commit "$c2" -p report.py -l 7-8 -m "Drop the None check, nothing passes None here."
	grr submit --request-changes -m "A few inline asks. Also add tests for \`median\`, there are none."
}

# fixed is a round of fixes by an agent: threads 1, 2, and r1 addressed, a question on 3.
fixed() {
	python3 - "$repo/stats.py" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace("    tmp = 0\n    for x in xs:\n        tmp += x\n    return tmp / len(xs)\n",
              "    if not xs:\n        return 0.0\n    total = 0\n    for x in xs:\n        total += x\n    return total / len(xs)\n")
open(p, "w").write(s)
EOF
	commit "Return 0.0 from mean for an empty list"
	local f1
	f1=$(sha HEAD)
	cat >"$repo/test_stats.py" <<'EOF'
import unittest

from stats import median


class MedianTest(unittest.TestCase):
    def test_odd(self):
        self.assertEqual(median([3, 1, 2]), 2)

    def test_even(self):
        self.assertEqual(median([4, 1, 3, 2]), 2.5)


if __name__ == "__main__":
    unittest.main()
EOF
	commit "Add median tests"
	local f2
	f2=$(sha HEAD)
	GR_AUTHOR=Claude grr address 1 -m "Fixed in $f1: \`mean\` returns 0.0 for an empty list."
	GR_AUTHOR=Claude grr address 2 -m "Fixed in $f1: renamed \`tmp\` to \`total\`."
	GR_AUTHOR=Claude grr address r1 -m "Fixed in $f2: added \`test_stats.py\` with odd and even cases for \`median\`."
	GR_AUTHOR=Claude grr reply 3 -m "The \`__main__\` block calls \`summary(None)\` on report.py line 14. Drop that call along with the check, or keep both?"
}

case $scenario in
address)
	init
	feature
	reviewed
	;;
second-round)
	init
	feature
	reviewed
	fixed
	grr reopen 1 -m "Returning 0.0 hides an empty input. Raise ValueError in \`mean\` instead, and have \`summary\` return \"no data\" for an empty list."
	;;
resolve)
	init
	feature
	reviewed
	fixed
	;;
review)
	init
	cat >"$repo/stats.py" <<'EOF'
"""Small statistics helpers."""


def mean(xs):
    return sum(xs) / len(xs)
EOF
	commit "Add mean"
	git -C "$repo" checkout -q -b feature
	cat >>"$repo/stats.py" <<'EOF'


def last(xs):
    """Return the last item."""
    return xs[len(xs)]


def window(xs, n):
    """Return every run of n consecutive items."""
    return [xs[i:i + n] for i in range(len(xs) - n)]
EOF
	commit "update stuff"
	;;
*)
	echo "unknown scenario: $scenario" >&2
	exit 2
	;;
esac

git -C "$repo" rev-parse HEAD >"$dir/fixture-head"
