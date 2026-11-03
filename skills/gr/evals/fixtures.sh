#!/usr/bin/env bash
# Builds a scratch repository and gr database for one eval scenario.
#
#   fixtures.sh <scenario> <dir>
#
# Creates <dir>/repo, <dir>/gr.db, <dir>/fixture-head, the commit the agent starts from, and
# <dir>/bin/gr, which runs gr against <dir>/gr.db and logs each command to <dir>/gr.log for grading.
# The human is the repository's git identity, "Pat Reviewer"; an agent's earlier replies are by
# "Claude".
#
# Scenarios:
#   address       a submitted review with three line threads and a summary ask
#   second-round  address after a round of fixes, with one fixed thread reopened
#   resolve       address after a round of fixes, ready for the human's final pass
#   review        an unreviewed branch with two off-by-one bugs and a vague commit message
#   outdated      address, after a later commit already fixed one thread's ask
#   narrowed      address, with one ask changed by the reviewer's later reply
#   off-branch    address, with the repository checked out on main
#   mixed         address, with new open threads among addressed and resolved ones
#   overlap       address two threads on the same lines, where fixing one moves the other
#   big           address a long review: twelve threads with long comments, some with replies
#   huge          big, with comments long enough that the thread list overflows a tool's output
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
	rm -rf "$repo" "$GR_DB" "$dir/gr.log"
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

# small submits a review of the feature branch with two line threads: 1 for an empty list, 2 to
# rename tmp.
small() {
	local c1
	c1=$(sha HEAD~1)
	grr comment --commit "$c1" -p stats.py -l 8 \
		-m "This raises ZeroDivisionError for an empty list. Return 0.0 for an empty list instead."
	grr comment --commit "$c1" -p stats.py -l 5-7 -m "Rename \`tmp\` to \`total\`."
	grr submit --request-changes -m ""
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
outdated)
	init
	feature
	# Threads on the whole diff, which follow its lines as it changes.
	grr comment -p stats.py -l 8 \
		-m "This raises ZeroDivisionError for an empty list. Return 0.0 for an empty list instead."
	grr comment -p stats.py -l 5-7 -m "Rename \`tmp\` to \`total\`."
	grr submit --request-changes -m ""
	# The author fixes thread 1's ask before anyone replies, on its own line, so the thread is
	# outdated.
	python3 - "$repo/stats.py" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace("    return tmp / len(xs)\n", "    return tmp / len(xs) if xs else 0.0\n")
open(p, "w").write(s)
PY
	commit "Handle an empty list in mean"
	;;
narrowed)
	init
	feature
	small
	grr reply 1 -m "On second thought, 0.0 hides an empty input. Raise ValueError(\"mean of an empty list\") instead."
	;;
off-branch)
	init
	feature
	small
	git -C "$repo" checkout -q main
	;;
mixed)
	init
	feature
	reviewed
	fixed
	grr resolve 2
	c1=$(sha "$(git -C "$repo" rev-list --reverse main..feature | head -1)")
	grr comment --commit "$c1" -p stats.py -l 1 -m "Say what each helper returns for an empty list in the module docstring."
	grr comment --commit "$c1" -p stats.py -l 11 -m "Add a docstring to \`median\`."
	grr submit -m ""
	;;
big | huge)
	init
	feature
	cat >"$repo/more.py" <<'PY'
"""More statistics helpers."""


def variance(xs):
    m = sum(xs) / len(xs)
    return sum((x - m) ** 2 for x in xs) / len(xs)


def stdev(xs):
    return variance(xs) ** 0.5


def mode(xs):
    counts = {}
    for x in xs:
        counts[x] = counts.get(x, 0) + 1
    return max(counts, key=counts.get)


def span(xs):
    return max(xs) - min(xs)


def clamp(x, lo, hi):
    return max(lo, min(x, hi))


def normalize(xs):
    lo, hi = min(xs), max(xs)
    return [(x - lo) / (hi - lo) for x in xs]
PY
	commit "Add more helpers"
	c1= c3=
	c1=$(sha HEAD~2)
	c3=$(sha HEAD)
	# Long comments, as a careful reviewer writes them, so the thread list is long.
	why="I would rather the helpers in this module all behave the same way at their edges, so that a caller who learns one of them can predict the others, and so that the tests can share fixtures. When we discussed this module last week we agreed that surprising edge behavior is worse than a slightly longer function, and that each helper should say in its docstring what it does with an input it cannot handle."
	if [ "$scenario" = huge ]; then
		# Longer still, as a review that argues each point: the list then runs past what a
		# tool shows of a command's output.
		why="$why

$why Naming matters for the same reason: a name that reads like something else costs every reader a second look, and the rename is cheaper now than after more callers depend on it.

$why"
	fi
	grr comment --commit "$c1" -p stats.py -l 5-7 -m "Rename \`tmp\` to \`total\`.

$why"
	grr comment --commit "$c1" -p stats.py -l 4 -m "Add a docstring to \`mean\` that says it raises ZeroDivisionError for an empty list.

$why"
	grr comment --commit "$c1" -p stats.py -l 11 -m "Add a docstring to \`median\` that says it raises IndexError for an empty list.

$why"
	grr comment --commit "$c3" -p more.py -l 4 -m "Add a docstring to \`variance\`: it is the population variance, not the sample variance.

$why"
	grr comment --commit "$c3" -p more.py -l 9 -m "Add a docstring to \`stdev\`: the population standard deviation.

$why"
	grr comment --commit "$c3" -p more.py -l 13 -m "Add a docstring to \`mode\` that says ties go to the value seen first.

$why"
	grr comment --commit "$c3" -p more.py -l 20 -m "Rename \`span\` to \`value_range\`; \`span\` reads like a time span.

$why"
	grr comment --commit "$c3" -p more.py -l 24 -m "Rename \`clamp\`'s parameters \`lo\` and \`hi\` to \`low\` and \`high\`.

$why"
	grr comment --commit "$c3" -p more.py -l 28-30 -m "Rename \`normalize\`'s locals \`lo\` and \`hi\` to \`low\` and \`high\`, as in \`clamp\`.

$why"
	grr comment --commit "$c3" -p more.py -l 1 -m "Expand the module docstring: say these helpers take a non-empty list of numbers.

$why"
	grr comment --commit "$c3" -p more.py -l 14-16 -m "Use \`collections.Counter\` in \`mode\` instead of the dict.

$why"
	grr comment --commit "$c3" -p more.py -l 5 -m "Use \`statistics\`-style naming: rename the local \`m\` to \`mu\`.

$why"
	grr submit --request-changes -m "Mostly naming and docstrings, nothing structural. Each thread says what I want.

$why"
	# Replies on a few threads, so some carry more than one comment.
	grr reply 6 -m "To be clear: first seen in the list, not the smallest value. Counter.most_common keeps insertion order for ties, so that works."
	grr reply 11 -m "Keep the function signature as it is; only the body changes."
	grr reply 12 -m "Only in \`variance\`; \`stdev\` has no such local."
	;;
overlap)
	init
	feature
	# Threads on the whole diff, so fixing the first leaves the second outdated.
	grr comment -p stats.py -l 5-8 -m "Use \`sum(xs)\` instead of the loop."
	grr comment -p stats.py -l 8 \
		-m "This raises ZeroDivisionError for an empty list. Return 0.0 for an empty list instead."
	grr submit --request-changes -m ""
	;;
*)
	echo "unknown scenario: $scenario" >&2
	exit 2
	;;
esac

git -C "$repo" rev-parse HEAD >"$dir/fixture-head"
git -C "$repo" rev-parse main >"$dir/fixture-main"
git -C "$repo" rev-parse feature >"$dir/fixture-feature"

# bin/gr is the gr the agent runs: gr on this test's database, logging every command for grading.
mkdir -p "$dir/bin"
real=$(command -v "$gr")
cat >"$dir/bin/gr" <<SHIM
#!/bin/sh
printf '%s\n' "\$*" >>"$dir/gr.log"
GR_DB="$dir/gr.db" exec "$real" "\$@"
SHIM
chmod +x "$dir/bin/gr"
