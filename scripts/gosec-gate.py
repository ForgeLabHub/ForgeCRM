#!/usr/bin/env python3
"""Fail a gosec run on findings that are not in the accepted baseline.

The security job runs gosec with a severity/confidence gate and this script turns
its JSON report into a pass/fail. A plain `gosec ./...` gate cannot be switched on
for a repository that already has findings: it would either fail every build from
the moment it lands, or have to stay green behind `|| true`, which is the same as
having no gate. The baseline breaks that deadlock. Each entry records a finding
that has been read and accepted, with a reason; the gate fails only on a finding
that is not in the list, so a new HIGH is caught on the commit that introduces it.

Fingerprints are (rule, file, flagged source line) rather than (rule, file, line
number), so inserting a line above an accepted finding does not resurrect it as a
false positive, while a real change to the flagged code does.

Usage: gosec-gate.py REPORT.json [backend-root]
Exit: 0 when every finding is accepted (or none), 1 on any new finding.
"""
import json
import os
import sys


def relative(path: str, root: str) -> str:
    """Path as it reads in the baseline, i.e. relative to the backend module."""
    path = os.path.normpath(path)
    root = os.path.normpath(root)
    try:
        rel = os.path.relpath(path, root)
    except ValueError:
        return path
    return path if rel.startswith("..") else rel


def flagged_line(code: str, line) -> str:
    """The source line gosec flagged, without the "NNN: " prefix it adds.

    The `code` field holds a window of three lines (the flagged one in the
    middle), so the entry whose number matches the finding is picked rather than
    the last line in the window.
    """
    if not code:
        return ""
    want = str(line)
    fallback = ""
    for raw in code.strip().splitlines():
        number, _, text = raw.partition(": ")
        if number.strip() == want:
            return text.strip()
        fallback = text.strip()
    return fallback


def fingerprint(issue: dict, root: str) -> str:
    return "|".join(
        (
            issue.get("rule_id", ""),
            relative(issue.get("file", ""), root),
            flagged_line(issue.get("code", ""), issue.get("line")),
        )
    )


def main() -> int:
    if len(sys.argv) < 2:
        print("usage: gosec-gate.py REPORT.json [backend-root]", file=sys.stderr)
        return 2
    report_path = sys.argv[1]
    root = sys.argv[2] if len(sys.argv) > 2 else os.getcwd()

    with open(report_path) as handle:
        report = json.load(handle)

    baseline_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "gosec-baseline.json")
    with open(baseline_path) as handle:
        baseline = json.load(handle)
    accepted = {entry["fingerprint"]: entry.get("reason", "") for entry in baseline["accepted"]}

    issues = report.get("Issues") or []
    seen = set()
    new = []
    for issue in issues:
        fp = fingerprint(issue, root)
        seen.add(fp)
        if fp not in accepted:
            new.append((issue, fp))

    for issue, _ in new:
        print(
            "NEW finding: {} {}:{} [{}]".format(
                issue.get("rule_id"),
                relative(issue.get("file", ""), root),
                issue.get("line"),
                issue.get("details"),
            )
        )
        print("  {}".format(flagged_line(issue.get("code", ""), issue.get("line"))))

    for fp in sorted(set(accepted) - seen):
        # Not a failure: an accepted finding that is gone means someone fixed it,
        # which is the point. It is printed so the entry can be dropped from the
        # baseline in the same change, keeping the list honest.
        print("accepted finding no longer reported (drop it from the baseline): {}".format(fp))

    print("gosec-gate: {} finding(s), {} accepted, {} new".format(len(issues), len(seen & set(accepted)), len(new)))
    return 1 if new else 0


if __name__ == "__main__":
    sys.exit(main())
