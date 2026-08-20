#!/usr/bin/env python3

import json
import os
import pathlib
import subprocess
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

from research_agent_smoke_assertions import SmokeBindingError, assert_run_binding, assert_run_detail_binding


EXPECTED = {
    "preflight_id": "research-preflight-synthetic",
    "package_id": "synthetic-agent",
    "package_version": "1.0.0",
    "mode": "quick",
}


def create_payload():
    return {"created": True, "run": {"run_id": "research-run-synthetic", **EXPECTED}}


def detail_payload():
    return {"run": {"run_id": "research-run-synthetic", "status": "completed", **EXPECTED}}


if assert_run_binding(create_payload(), **EXPECTED) != "research-run-synthetic":
    raise AssertionError("valid create binding did not return the Run ID")
assert_run_detail_binding(detail_payload(), **EXPECTED)

for field in ("preflight_id", "package_id", "package_version", "mode"):
    mutated_create = create_payload()
    mutated_create["run"][field] = "mutated"
    try:
        assert_run_binding(mutated_create, **EXPECTED)
    except SmokeBindingError:
        pass
    else:
        raise AssertionError(f"create binding accepted mutated {field}")

    mutated_detail = detail_payload()
    mutated_detail["run"][field] = "mutated"
    try:
        assert_run_detail_binding(mutated_detail, **EXPECTED)
    except SmokeBindingError:
        pass
    else:
        raise AssertionError(f"detail binding accepted mutated {field}")

module_path = pathlib.Path(__file__).resolve().parent / "research_agent_smoke_assertions.py"
optimized_commands = [
    ([sys.executable], {**os.environ, "PYTHONOPTIMIZE": "1"}),
    (["python3", "-O"], dict(os.environ)),
]
with tempfile.TemporaryDirectory(prefix="research-agent-binding-") as temp_root:
    for command, environment in optimized_commands:
        for action, factory in (("create", create_payload), ("detail", detail_payload)):
            for field in ("preflight_id", "package_id", "package_version", "mode"):
                payload = factory()
                payload["run"][field] = "mutated"
                payload_path = pathlib.Path(temp_root) / f"{action}-{field}.json"
                payload_path.write_text(json.dumps(payload, separators=(",", ":")), encoding="utf-8")
                result = subprocess.run(
                    [
                        *command,
                        str(module_path),
                        action,
                        str(payload_path),
                        EXPECTED["preflight_id"],
                        EXPECTED["package_id"],
                        EXPECTED["package_version"],
                        EXPECTED["mode"],
                    ],
                    env=environment,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    check=False,
                )
                if result.returncode == 0:
                    raise AssertionError(
                        f"optimized {action} binding accepted mutated {field}: {command}"
                    )

print("research agent smoke binding mutation test passed")
