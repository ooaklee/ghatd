"""Shared profile/path plumbing; fixture semantics stay in the suites."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

PANEL_IDS = {
    "requests": 2, "errors": 3, "database_total": 5, "request_rate": 6,
    "latency": 7, "database_rate": 9, "traces": 10, "full_logs": 11,
    "error_ratio": 12, "freshness": 13, "age": 14, "database_latency": 18,
    "database_errors": 19, "outbound_latency": 20, "outbound_errors": 21,
    "runtime_memory": 22, "runtime_allocations": 23, "runtime_goroutines": 24,
    "cache_rate": 25, "cache_ratio": 26, "outbound_rate": 33,
    "request_summary": 34, "browser_intake": 35, "worker_rate": 36, "worker_latency": 37,
}
TOOL_ENV = {"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "ASDF_HELM_VERSION"}


def resolve(root, relative):
    candidate = Path(relative)
    if candidate.is_absolute() or ".." in candidate.parts:
        raise ValueError("profile paths must stay inside their declared root")
    root = Path(root).resolve()
    result = (root / candidate).resolve()
    if root != result and root not in result.parents:
        raise ValueError("profile path resolves outside its declared root")
    return result


def environment(**extra):
    result = {key: value for key, value in os.environ.items() if key in TOOL_ENV}
    result.update(extra)
    return result


def options():
    parser = argparse.ArgumentParser(description="Verify host observability assets using shared fixtures")
    parser.add_argument("--root", required=True)
    parser.add_argument("--profile", required=True)
    parser.add_argument("--foundation-dir", type=Path)
    args = parser.parse_args()
    profile = json.loads(Path(args.profile).read_text())
    if profile.get("version") != 1:
        raise ValueError("unsupported verification profile version")
    return Path(args.root).resolve(), profile, args.foundation_dir


def run(main):
    try:
        main()
    except (AssertionError, RuntimeError, KeyError, IndexError, TypeError, ValueError, OSError, subprocess.SubprocessError):
        print("Verification failed; supplied values and tool diagnostics withheld.", file=sys.stderr)
        sys.exit(1)


def panels(profile):
    configured = profile["dashboards"].get("panels", {})
    if not set(configured) <= set(PANEL_IDS):
        raise ValueError("unknown dashboard panel role")
    result = {**PANEL_IDS, **configured}
    if any(type(value) is not int or value <= 0 for value in result.values()) or len(set(result.values())) != len(result):
        raise ValueError("dashboard panel IDs must be distinct positive integers")
    return result


def application_names(profile, name=None):
    name = name or profile["service"]["name"]
    helm = profile["helm"]
    names = [name]
    if helm["migrator"]:
        names.append(name + "-migration")
    if helm["sidekick"]:
        names.append(name + "-sidekick")
    names.extend(name + "-" + worker["suffix"] for worker in helm["workers"])
    return names


def deployment_count(profile):
    return 1 + len(profile["helm"]["workers"])


def chart_copy(root, directory, profile):
    import shutil
    relative = profile["helm"]["chart_dir"]
    target = resolve(directory, relative)
    target.parent.mkdir(parents=True, exist_ok=True)
    source = resolve(root, relative)
    for child in source.rglob("*"):
        if child.is_symlink():
            resolve(root, child.relative_to(Path(root).resolve()))
    shutil.copytree(source, target)
    return target
