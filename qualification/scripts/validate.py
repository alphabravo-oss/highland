#!/usr/bin/env python3
"""Validate qualification/profiles.yaml against schema field rules (stdlib-friendly).

Does not require jsonschema or PyYAML. Uses a small YAML subset loader that
covers the Highland profiles.yaml layout. When PyYAML is available it is used.
"""
from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ALLOWED_GATES = {"pr", "nightly", "preview", "production"}
ALLOWED_PROVIDERS = {
    "generic-csi",
    "longhorn",
    "rook-ceph",
    "openebs",
    "linstor",
    "highland",
}
ALLOWED_ENV = {"kind", "hosted-runner", "self-hosted-lab", "disposable-vm"}
ASSERTION_RE = re.compile(r"^QA-C4\.(1[0-4]|[1-9])$")
ID_RE = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
TIMEOUT_RE = re.compile(r"^[0-9]+(s|m|h)$")

REQUIRED_PROFILE_IDS = {
    "generic-csi-kind",
    "longhorn-current",
    "longhorn-previous",
    "rook-ceph-current",
    "rook-ceph-previous",
    "openebs-hostpath",
    "openebs-mayastor",
    "linstor-single-node",
    "linstor-drbd",
    "ha-multi-replica",
}


def load_yaml(path: Path):
    text = path.read_text(encoding="utf-8")
    try:
        import yaml  # type: ignore

        return yaml.safe_load(text)
    except ImportError:
        return load_profiles_subset(text)


def load_profiles_subset(text: str):
    """Minimal parser for profiles.yaml without PyYAML."""
    data: dict = {"profiles": []}
    cur: dict | None = None
    in_assertions = False
    in_version = False

    for raw in text.splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        line = raw.rstrip()
        indent = len(raw) - len(raw.lstrip(" "))

        if line.startswith("schemaVersion:"):
            data["schemaVersion"] = int(line.split(":", 1)[1].strip())
            continue
        if line.startswith("profiles:"):
            continue
        if line.startswith("  - id:"):
            if cur:
                data["profiles"].append(cur)
            cur = {"id": line.split(":", 1)[1].strip().strip('"')}
            in_assertions = False
            in_version = False
            continue
        if cur is None:
            continue
        if indent == 4 and line.strip().startswith("- ") and in_assertions:
            cur.setdefault("requiredAssertions", []).append(
                line.strip()[2:].strip().strip('"')
            )
            continue
        if indent == 4 and ":" in line and not line.strip().startswith("- "):
            key, val = line.strip().split(":", 1)
            key = key.strip()
            val = val.strip().strip('"')
            in_assertions = key == "requiredAssertions"
            in_version = key == "versionSelectors"
            if in_assertions or in_version:
                if in_assertions:
                    cur["requiredAssertions"] = []
                if in_version:
                    cur["versionSelectors"] = {}
                continue
            if val.lower() in ("true", "false"):
                cur[key] = val.lower() == "true"
            elif val.isdigit():
                cur[key] = int(val)
            else:
                cur[key] = val
            continue
        if indent == 6 and in_version and ":" in line:
            k, v = line.strip().split(":", 1)
            cur.setdefault("versionSelectors", {})[k.strip()] = v.strip().strip('"')
            continue
    if cur:
        data["profiles"].append(cur)
    return data


def validate_profiles(data: dict) -> list[str]:
    errors: list[str] = []
    if data.get("schemaVersion") != 1:
        errors.append(f"schemaVersion must be 1, got {data.get('schemaVersion')!r}")
    profiles = data.get("profiles") or []
    if not profiles:
        errors.append("profiles list is empty")
        return errors

    seen: set[str] = set()
    for i, p in enumerate(profiles):
        if not isinstance(p, dict):
            errors.append(f"profiles[{i}] is not an object")
            continue
        pid = str(p.get("id", ""))
        prefix = f"profile {pid or i}"
        if not ID_RE.match(pid):
            errors.append(f"{prefix}: invalid id")
        if pid in seen:
            errors.append(f"{prefix}: duplicate id")
        seen.add(pid)
        if p.get("provider") not in ALLOWED_PROVIDERS:
            errors.append(f"{prefix}: invalid provider {p.get('provider')!r}")
        if p.get("gate") not in ALLOWED_GATES:
            errors.append(f"{prefix}: invalid gate {p.get('gate')!r}")
        if p.get("environmentClass") not in ALLOWED_ENV:
            errors.append(
                f"{prefix}: invalid environmentClass {p.get('environmentClass')!r}"
            )
        assertions = p.get("requiredAssertions") or []
        if not assertions:
            errors.append(f"{prefix}: requiredAssertions must be non-empty")
        for a in assertions:
            if not ASSERTION_RE.match(str(a)):
                errors.append(f"{prefix}: invalid assertion {a!r}")
        timeout = str(p.get("timeout", ""))
        if not TIMEOUT_RE.match(timeout):
            errors.append(f"{prefix}: invalid timeout {timeout!r}")
        if "cleanupRequired" not in p:
            errors.append(f"{prefix}: cleanupRequired is required")
        elif not isinstance(p.get("cleanupRequired"), bool):
            # subset parser may leave string; accept "true"/"false" already coerced
            errors.append(f"{prefix}: cleanupRequired must be boolean")

    missing = sorted(REQUIRED_PROFILE_IDS - seen)
    if missing:
        errors.append(f"missing required profile ids: {', '.join(missing)}")
    return errors


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    profiles_path = root / "qualification" / "profiles.yaml"
    schema_path = root / "qualification" / "schema.json"
    results_schema_path = root / "qualification" / "results.schema.json"

    if not profiles_path.is_file():
        print(f"ERROR: missing {profiles_path}", file=sys.stderr)
        return 1
    if not schema_path.is_file():
        print(f"ERROR: missing {schema_path}", file=sys.stderr)
        return 1
    if not results_schema_path.is_file():
        print(f"ERROR: missing {results_schema_path}", file=sys.stderr)
        return 1

    # Ensure schema files are valid JSON.
    for p in (schema_path, results_schema_path):
        try:
            json.loads(p.read_text(encoding="utf-8"))
        except json.JSONDecodeError as e:
            print(f"ERROR: invalid JSON in {p}: {e}", file=sys.stderr)
            return 1

    data = load_yaml(profiles_path)
    if not isinstance(data, dict):
        print("ERROR: profiles.yaml did not parse to an object", file=sys.stderr)
        return 1

    errors = validate_profiles(data)
    print(f"profiles: {profiles_path}")
    print(f"  count={len(data.get('profiles') or [])}")
    if errors:
        print("ERRORS:", file=sys.stderr)
        for e in errors:
            print(f"  - {e}", file=sys.stderr)
        return 1
    print("OK: profiles.yaml field validation passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
