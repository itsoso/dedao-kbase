#!/usr/bin/env python3

import json
import sys


class SmokeBindingError(ValueError):
    pass


def _require(condition, code):
    if not condition:
        raise SmokeBindingError(code)


def _assert_exact_run_binding(run, preflight_id, package_id, package_version, mode):
    _require(isinstance(run, dict), "run_missing")
    _require(run.get("preflight_id") == preflight_id, "preflight_mismatch")
    _require(run.get("package_id") == package_id, "package_mismatch")
    _require(run.get("package_version") == package_version, "version_mismatch")
    _require(run.get("mode") == mode, "mode_mismatch")


def assert_run_binding(payload, preflight_id, package_id, package_version, mode):
    _require(isinstance(payload, dict), "payload_missing")
    _require(payload.get("created") is True, "run_not_created")
    run = payload.get("run")
    _assert_exact_run_binding(run, preflight_id, package_id, package_version, mode)
    run_id = run.get("run_id")
    _require(isinstance(run_id, str) and bool(run_id), "run_id_missing")
    return run_id


def assert_run_detail_binding(payload, preflight_id, package_id, package_version, mode):
    _require(isinstance(payload, dict), "payload_missing")
    run = payload.get("run")
    _assert_exact_run_binding(run, preflight_id, package_id, package_version, mode)
    _require(run.get("status") == "completed", "run_not_completed")


def main(argv):
    if len(argv) != 7 or argv[1] not in ("create", "detail"):
        return 2
    action, path, preflight_id, package_id, package_version, mode = argv[1:]
    try:
        with open(path, encoding="utf-8") as source:
            payload = json.load(source)
        if action == "create":
            print(assert_run_binding(payload, preflight_id, package_id, package_version, mode))
        else:
            assert_run_detail_binding(payload, preflight_id, package_id, package_version, mode)
    except (AssertionError, KeyError, OSError, TypeError, ValueError, json.JSONDecodeError):
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
