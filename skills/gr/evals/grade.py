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
        # The feature branch's head before the run: new commits are counted from it, wherever the
        # agent started.
        head = os.path.join(path, "fixture-feature")
        if not os.path.exists(head):
            head = os.path.join(path, "fixture-head")
        with open(head) as f:
            self.fixture_head = f.read().strip()
        main = os.path.join(path, "fixture-main")
        self.fixture_main = open(main).read().strip() if os.path.exists(main) else None
        log = os.path.join(path, "gr.log")
        self.gr_log = open(log).read().splitlines() if os.path.exists(log) else None

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

    def on_feature(self, code):
        """Runs code against the feature branch's files, wherever the agent left the checkout."""
        tmp = run(["mktemp", "-d"], self.repo).stdout.strip()
        files = self.git("ls-tree", "--name-only", "feature").split()
        for f in files:
            with open(os.path.join(tmp, f), "w") as out:
                out.write(self.git("show", f"feature:{f}"))
        return run([sys.executable, "-c", code], tmp, check=False)

    def feature_file(self, path):
        return self.git("show", f"feature:{path}")

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


# Flags of gr's that take a value, to find a command's subcommand in the log.
VALUED = {"-C", "-c", "--change", "--db", "--author", "--base", "-m", "-F", "-p", "-l", "--commit",
          "--state", "--side", "--thread", "--review"}


def subcommand(line):
    args = line.split()
    i = 0
    while i < len(args):
        if args[i] in VALUED:
            i += 2
        elif args[i].startswith("-"):
            i += 1
        else:
            return args[i]
    return ""


def grade_wasted_calls(results, r):
    """Checks the agent did not fetch a thread by ref after listing the threads, which already
    showed each in full."""
    if r.gr_log is None:
        return
    cmds = [subcommand(l) for l in r.gr_log]
    after = cmds[cmds.index("threads") + 1:] if "threads" in cmds else []
    wasted = [l for l, c in zip(r.gr_log[len(cmds) - len(after):], after)
              if c == "thread" and "--help" not in l.split()]
    expect(results, "No thread is fetched by ref after the thread list showed it",
           "threads" in cmds and not wasted, f"gr commands: {[c for c in cmds]}; fetched after listing: {wasted}")
    # Listing again before acting on anything fetches the same threads twice, as an agent does
    # when the list ran past what its tool showed and it lists again to read it in parts.
    writes = {"address", "reply", "edit", "resolve", "reopen", "delete", "comment", "submit"}
    first = next((i for i, c in enumerate(cmds) if c in writes), len(cmds))
    lists = [l for l, c in zip(r.gr_log[:first], cmds[:first]) if c == "threads" and "--help" not in l.split()]
    expect(results, "The threads are listed once before acting on them", len(lists) == 1,
           f"thread lists before the first write: {lists}")


def grade_fixes(results, r, refs, fixture, new):
    """Checks refs are addressed, each with a reply citing one of new's commits."""
    got = {}
    for ref in refs:
        t = r.threads.get(ref)
        n = fixture.get(ref, (1, "open"))[0]
        got[ref] = (state(r, ref), any(cited(c["body"], new) for c in comments(t)[n:]))
    names = f"Thread {refs[0]} is" if len(refs) == 1 else f"Threads {', '.join(refs)} are"
    expect(results, f"{names} addressed with a reply citing the fixing commit",
           all(st == "addressed" and c for st, c in got.values()),
           f"state and cites: {got}; commits: {[s[:7] for s in new]}")


def grade_untouched(results, r, fixture):
    got = {ref: (len(comments(r.threads.get(ref))), state(r, ref)) for ref in fixture}
    expect(results, f"Threads {', '.join(fixture)} are untouched", got == fixture, f"got {got}, want {fixture}")


def grade_committed(results, r, new):
    expect(results, "Every fix is committed on feature", r.clean() and new,
           f"clean: {r.clean()}, new commits on feature: {len(new)}")


def grade_outdated(r):
    res = []
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, {"1": (1, "open"), "2": (1, "open")}, 1)
    # Thread 1's ask was met by the fixture's last commit, before the agent: the reply should say so.
    grade_fixes(res, r, ["1"], {}, [r.fixture_head])
    grade_fixes(res, r, ["2"], {}, new)
    stats = r.feature_file("stats.py")
    checks = stats.count("if xs else") + stats.count("if not xs")
    p = r.on_feature("from stats import mean; print(repr(mean([])))")
    expect(res, "The empty-list fix already there is kept, not done again",
           checks == 1 and p.stdout.strip() == "0.0", f"empty-list checks: {checks}, mean([]) -> {p.stdout.strip()!r}")
    expect(res, "tmp is renamed to total", "tmp" not in stats and "total" in stats, f"tmp left: {'tmp' in stats}")
    grade_committed(res, r, new)
    grade_wasted_calls(res, r)
    return res


def grade_narrowed(r):
    res = []
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, {"1": (2, "open"), "2": (1, "open")}, 1)
    grade_fixes(res, r, ["1"], {"1": (2, "open")}, new)
    grade_fixes(res, r, ["2"], {}, new)
    p = r.on_feature("from stats import mean\ntry:\n    print(repr(mean([])))\nexcept ValueError as e:\n    print('ValueError', e)")
    expect(res, "mean([]) raises ValueError, as the later reply asked, not 0.0",
           p.stdout.startswith("ValueError"), f"stdout {p.stdout.strip()!r} stderr {p.stderr.strip()[-200:]!r}")
    grade_committed(res, r, new)
    grade_wasted_calls(res, r)
    return res


def grade_off_branch(r):
    res = []
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, {"1": (1, "open"), "2": (1, "open")}, 1)
    main = r.git("rev-parse", "main").strip()
    expect(res, "main is unchanged", main == r.fixture_main, f"main {main[:7]}, was {r.fixture_main[:7]}")
    grade_fixes(res, r, ["1", "2"], {}, new)
    p = r.on_feature("from stats import mean; print(repr(mean([])))")
    stats = r.feature_file("stats.py")
    expect(res, "feature has both fixes", p.stdout.strip() == "0.0" and "tmp" not in stats,
           f"mean([]) -> {p.stdout.strip()!r}, tmp left: {'tmp' in stats}")
    grade_committed(res, r, new)
    grade_wasted_calls(res, r)
    return res


def grade_mixed(r):
    res = []
    fixture = {"1": (2, "addressed"), "2": (2, "resolved"), "3": (2, "open"), "r1": (2, "addressed")}
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, dict(fixture, **{"4": (1, "open"), "5": (1, "open")}), 2)
    grade_fixes(res, r, ["4", "5"], {}, new)
    grade_untouched(res, r, fixture)
    p = r.on_feature("import stats\nprint(bool(stats.median.__doc__))\n"
                     "print('empty' in (stats.__doc__ or '').lower())")
    out = p.stdout.split()
    expect(res, "median has a docstring, and the module's says what an empty list gives",
           out == ["True", "True"], f"median docstring, module mentions empty: {out}, stderr {p.stderr.strip()[-200:]!r}")
    grade_committed(res, r, new)
    grade_wasted_calls(res, r)
    return res


def grade_overlap(r):
    res = []
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, {"1": (1, "open"), "2": (1, "open")}, 1)
    grade_fixes(res, r, ["1", "2"], {}, new)
    p = r.on_feature("import inspect, stats\nsrc = inspect.getsource(stats.mean)\n"
                     "print('sum(' in src and 'for ' not in src)\nprint(repr(stats.mean([])))")
    out = p.stdout.split()
    expect(res, "mean uses sum and returns 0.0 for an empty list: both asks are met",
           out == ["True", "0.0"], f"uses sum, mean([]): {out}, stderr {p.stderr.strip()[-200:]!r}")
    grade_committed(res, r, new)
    grade_wasted_calls(res, r)
    return res


def grade_big(r):
    res = []
    refs = [str(i) for i in range(1, 13)]
    fixture = {ref: (1, "open") for ref in refs}
    fixture.update({"6": (2, "open"), "11": (2, "open"), "12": (2, "open"), "r1": (1, "open")})
    new = r.commits_since(r.fixture_head)
    grade_identity(res, r, fixture, 1)
    grade_fixes(res, r, refs, fixture, new)
    p = r.on_feature("""import inspect, stats, more
checks = {
    'tmp renamed': 'tmp' not in inspect.getsource(stats.mean),
    'docstrings': all(f.__doc__ for f in [stats.mean, stats.median, more.variance, more.stdev, more.mode]),
    'value_range': hasattr(more, 'value_range') and not hasattr(more, 'span'),
    'clamp low, high': list(inspect.signature(more.clamp).parameters) == ['x', 'low', 'high'],
    'normalize low, high': 'lo,' not in inspect.getsource(more.normalize),
    'module docstring': 'empty' in (more.__doc__ or '').lower(),
    'Counter': 'Counter' in inspect.getsource(more.mode),
    'mu': 'mu' in inspect.getsource(more.variance),
}
print([k for k, ok in checks.items() if not ok])""")
    expect(res, "Every one of the twelve asks is met", p.stdout.strip() == "[]",
           f"unmet: {p.stdout.strip()} {p.stderr.strip()[-200:]!r}")
    grade_committed(res, r, new)
    grade_wasted_calls(res, r)
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
    "outdated": grade_outdated,
    "narrowed": grade_narrowed,
    "off-branch": grade_off_branch,
    "mixed": grade_mixed,
    "overlap": grade_overlap,
    "big": grade_big,
    "huge": grade_big,
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
