#!/usr/bin/env python3
"""Grades one eval run of the gr skills.

    grade.py <scenario> <fixture-dir> [<run-dir>]

The fixture directory holds repo/ and gr.db, as fixtures.sh made them, after the agent ran. Writes
<run-dir>/grading.json, and the final threads and log to <run-dir>/outputs/state.txt for reading.
The run directory defaults to the fixture directory.
"""

import json
import os
import re
import subprocess
import sys

HUMAN = "Pat Reviewer"


def run(args, cwd, env=None, check=True):
    p = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if check and p.returncode != 0:
        raise RuntimeError(f"{args}: {p.stderr}")
    return p


class Run:
    def __init__(self, path):
        self.path = path
        self.repo = os.path.join(path, "repo")
        self.env = dict(os.environ, GR_DB=os.path.join(path, "gr.db"))
        self.env.pop("GR_AUTHOR", None)
        changes = self.gr("change", "list", "--json")
        self.threads, self.change = {}, {"reviews": []}
        if changes:
            self.threads = {t["ref"]: t for t in self.gr("threads", "--json", "-c", "1")}
            self.change = self.gr("change", "show", "--json", "-c", "1")
        with open(os.path.join(path, "fixture-head")) as f:
            self.fixture_head = f.read().strip()

    def gr(self, *args):
        return json.loads(run(["gr", "-C", self.repo, *args], self.repo, self.env).stdout)

    def git(self, *args):
        return run(["git", *args], self.repo).stdout

    def py(self, code):
        return run([sys.executable, "-c", code], self.repo, check=False)

    def commits_since(self, rev):
        return self.git("rev-list", f"{rev}..feature").split()

    def file(self, path):
        with open(os.path.join(self.repo, path)) as f:
            return f.read()

    def clean(self):
        return self.git("status", "--porcelain") == ""


def comments(t):
    return t["comments"] if t else []


def agent_comments(t):
    """The comments after the reviewer's first, whoever wrote them: the identity check covers who."""
    return comments(t)[1:]


def cited(body, shas):
    """Reports whether body names one of shas by a prefix of at least 7 characters."""
    return any(any(s.startswith(w) for s in shas) for w in re.findall(r"\b[0-9a-f]{7,40}\b", body))


def state(r, ref):
    t = r.threads.get(ref)
    return t["state"] if t else "missing"


def expect(results, text, passed, evidence):
    results.append({"text": text, "passed": bool(passed), "evidence": evidence})


def thread_counts(r):
    return {ref: (t["state"], len(t["comments"])) for ref, t in sorted(r.threads.items())}


def grade_identity(results, r, fixture, fixture_reviews):
    """Checks everything written after the fixture is by someone other than the human. fixture maps
    each thread's ref to its comment count and state before the run."""
    authors = []
    for ref, t in r.threads.items():
        n, st = fixture.get(ref, (0, None))
        authors += [c["author"]["name"] for c in t["comments"][n:]]
        if t["state"] != st and "state_by" in t:
            authors.append(f"{t['state_by']['name']} (set {ref} {t['state']})")
    for rv in r.change["reviews"]:
        if rv["seq"] > fixture_reviews:
            authors.append(rv["author"]["name"])
    bad = [a for a in authors if a.startswith(HUMAN)]
    expect(results, "Everything the agent wrote is under its own name, not the human's git identity",
           authors and not bad, f"authors of new comments, states, and reviews: {authors}")


def grade_address(r):
    res = []
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, {ref: (1, "open") for ref in ["1", "2", "3", "r1"]}, 1)
    replied = {ref: len(agent_comments(r.threads.get(ref))) for ref in ["1", "2", "3", "r1"]}
    expect(res, "Every thread, including summary r1, has a reply", all(replied.values()),
           f"agent replies per thread: {replied}")
    states = {ref: state(r, ref) for ref in ["1", "2", "r1"]}
    expect(res, "Threads 1, 2, and r1 are addressed, not resolved",
           all(s == "addressed" for s in states.values()), f"states: {states}")
    cites = {ref: any(cited(c["body"], new) for c in agent_comments(r.threads.get(ref)))
             for ref in ["1", "2", "r1"]}
    expect(res, "Replies on 1, 2, and r1 cite a new commit's sha", all(cites.values()),
           f"cites a new commit: {cites}; new commits: {[s[:7] for s in new]}")
    keeps = "if xs is None" in r.file("report.py")
    expect(res, "Thread 3, which conflicts with the code, stays open with a reply and the check kept",
           state(r, "3") == "open" and replied["3"] and keeps,
           f"state {state(r, '3')}, agent replies {replied['3']}, None check kept: {keeps}")
    p = r.py("from stats import mean; print(repr(mean([])))")
    expect(res, "mean([]) returns 0.0", p.stdout.strip() == "0.0", f"stdout {p.stdout.strip()!r} stderr {p.stderr.strip()[-200:]!r}")
    stats = r.file("stats.py")
    expect(res, "tmp is renamed to total", "tmp" not in stats and "total" in stats, "stats.py has tmp: " + str("tmp" in stats))
    tests = [f for f in os.listdir(r.repo) if f.startswith("test") and f.endswith(".py")]
    has_median = any("median" in r.file(f) for f in tests)
    p = run([sys.executable, "-m", "unittest", "discover", "-q"], r.repo, check=False)
    expect(res, "Tests for median exist and pass", has_median and p.returncode == 0,
           f"test files {tests}, unittest exit {p.returncode}: {p.stderr.strip()[-200:]!r}")
    expect(res, "Every fix is committed", r.clean() and new, f"clean: {r.clean()}, new commits: {len(new)}")
    return res


def grade_second_round(r):
    res = []
    fixture = {"1": (3, "open"), "2": (2, "addressed"), "3": (2, "open"), "r1": (2, "addressed")}
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, fixture, 1)
    newer = comments(r.threads.get("1"))[3:]
    expect(res, "Reopened thread 1 is addressed again with a reply citing a new commit",
           state(r, "1") == "addressed" and any(cited(c["body"], new) for c in newer),
           f"state {state(r, '1')}, new comments {[c['body'] for c in newer]}, new commits {[s[:7] for s in new]}")
    untouched = {ref: (len(comments(r.threads.get(ref))), state(r, ref)) for ref in ["2", "3", "r1"]}
    want = {ref: fixture[ref] for ref in ["2", "3", "r1"]}
    expect(res, "Threads 2, 3, and r1 are untouched", untouched == want, f"got {untouched}, want {want}")
    p = r.py("from stats import mean\ntry:\n    mean([])\nexcept ValueError:\n    print('ValueError')")
    expect(res, "mean([]) raises ValueError", p.stdout.strip() == "ValueError", f"stdout {p.stdout.strip()!r} stderr {p.stderr.strip()[-200:]!r}")
    p = r.py("from report import summary; print(summary([]))")
    expect(res, "summary([]) returns \"no data\"", p.stdout.strip() == "no data", f"stdout {p.stdout.strip()!r} stderr {p.stderr.strip()[-200:]!r}")
    expect(res, "Every fix is committed", r.clean() and new, f"clean: {r.clean()}, new commits: {len(new)}")
    return res


def grade_resolve(r):
    res = []
    grade_identity(res, r, {"1": (2, "addressed"), "2": (2, "addressed"), "3": (2, "open"), "r1": (2, "addressed")}, 1)
    states = {ref: state(r, ref) for ref in ["1", "2", "r1"]}
    expect(res, "Addressed threads 1, 2, and r1 are resolved", all(s == "resolved" for s in states.values()),
           f"states: {states}")
    t3 = (state(r, "3"), len(comments(r.threads.get("3"))))
    expect(res, "Open thread 3 is untouched", t3 == ("open", 2), f"state and comments: {t3}")
    return res


def covers(t, path, line):
    loc = t["location"]
    return loc.get("path") == path and loc.get("start_line", 0) <= line <= loc.get("end_line", 0)


def grade_review(r):
    res = []
    grade_identity(res, r, {}, 0)
    reviews = r.change["reviews"]
    submitted = [rv for rv in reviews if rv["state"] == "submitted"]
    expect(res, "A review is submitted, with no draft left",
           submitted and len(submitted) == len(reviews), f"reviews: {[(rv['state'], rv['verdict']) for rv in reviews]}")
    expect(res, "The verdict requests changes", any(rv["verdict"] == "request_changes" for rv in submitted),
           f"verdicts: {[rv['verdict'] for rv in submitted]}")
    threads = list(r.threads.values())
    for line, what in [(10, "last's xs[len(xs)]"), (15, "window's range(len(xs) - n)")]:
        hits = [t["ref"] for t in threads if covers(t, "stats.py", line)]
        expect(res, f"A comment is on stats.py line {line}, {what}", hits, f"threads covering it: {hits}")
    msg = [t["ref"] for t in threads if t["location"].get("path") == "/COMMIT_MSG"]
    expect(res, "A comment is on the commit message", msg, f"commit message threads: {msg}")
    return res


def write_state(r, out):
    os.makedirs(os.path.join(out, "outputs"), exist_ok=True)
    threads = run(["gr", "-C", r.repo, "threads", "-c", "1"], r.repo, r.env, check=False).stdout
    log = r.git("log", "--format=%h %s", "main..feature")
    diff = r.git("diff", "--stat", "main", "feature")
    with open(os.path.join(out, "outputs", "state.txt"), "w") as f:
        f.write(f"# Threads\n\n{threads}\n# Commits on feature\n\n{log}\n# Diff stat\n\n{diff}")


GRADERS = {
    "address": grade_address,
    "second-round": grade_second_round,
    "resolve": grade_resolve,
    "review": grade_review,
}


def main():
    scenario, path = sys.argv[1], os.path.abspath(sys.argv[2])
    out = os.path.abspath(sys.argv[3]) if len(sys.argv) > 3 else path
    r = Run(path)
    res = GRADERS[scenario](r)
    write_state(r, out)
    passed = sum(e["passed"] for e in res)
    grading = {
        "expectations": res,
        "summary": {"passed": passed, "failed": len(res) - passed, "total": len(res),
                    "pass_rate": round(passed / len(res), 2)},
    }
    with open(os.path.join(out, "grading.json"), "w") as f:
        json.dump(grading, f, indent=2)
    print(f"{scenario}: {passed}/{len(res)}")


if __name__ == "__main__":
    main()
