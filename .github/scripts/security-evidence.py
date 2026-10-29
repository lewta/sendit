#!/usr/bin/env python3
"""Fail-closed evidence for pinned source/symbol govulncheck (Python 3.9+).

Protocol: golang/vuln v1.9.0 internal/govulncheck/govulncheck.go.
JSON findings do not set the scanner exit code and there is no terminal record.
Only our parent process can attest that the scanner returned successfully.
"""
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import subprocess
import sys
import tempfile

if sys.version_info < (3, 9):
    print("Blocking evidence: Python 3.9 or newer is required", file=sys.stderr)
    sys.exit(1)

MAX_BYTES = 64 * 1024 * 1024
TOOL = "v1.9.0"
OSV_TOOL = "v2.7.0"
GO_VERSION = "go1.26.10"
DATABASE = "https://vuln.go.dev"
# Retain all release targets. No package/module-list equivalence collapse.
TARGETS = [("linux", "amd64", ""), ("linux", "arm64", ""), ("linux", "arm", "7"),
           ("linux", "386", ""), ("darwin", "amd64", ""), ("darwin", "arm64", ""),
           ("windows", "amd64", ""), ("windows", "arm64", ""), ("freebsd", "amd64", "")]
FILE_FIELDS = ("GoFiles", "CgoFiles", "CFiles", "CXXFiles", "MFiles", "HFiles", "FFiles",
               "SFiles", "SysoFiles", "EmbedFiles")


class EvidenceError(ValueError):
    """Fixed, safe validation messages that may be retained or printed."""


def require(ok, message):
    if not ok:
        raise EvidenceError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def write_json(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON key")
        result[key] = value
    return result


def reject_constant(_):
    raise ValueError("non-JSON constant")


DECODER = json.JSONDecoder(object_pairs_hook=no_duplicates, parse_constant=reject_constant)


def stream(data):
    require(len(data) <= MAX_BYTES, "evidence exceeds size limit")
    text = data.decode("utf-8")
    values = []
    offset = 0
    while offset < len(text):
        if text[offset] in " \t\r\n":
            offset += 1
            continue
        value, offset = DECODER.raw_decode(text, offset)
        require(isinstance(value, dict), "JSON record must be an object")
        values.append(value)
    require(values, "empty JSON stream")
    return values


def read_json(path):
    values = stream(Path(path).read_bytes())
    require(len(values) == 1, "expected one JSON object")
    return values[0]


def timestamp(value):
    require(isinstance(value, str) and value.endswith("Z"), "missing/invalid timestamp")
    return dt.datetime.fromisoformat(value.replace("Z", "+00:00"))


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")


def target_contexts():
    contexts = []
    for goos, arch, arm in TARGETS:
        contexts.append({"id": goos + "-" + ("arm7" if arm else arch), "goos": goos,
                         "goarch": arch, "goarm": arm, "tags": [], "tests": False, "cgo_enabled": "0"})
    # The pre-existing Ubuntu ./... job enables CGo by default. Preserve its
    # selected program in addition to the CGo-free release contexts.
    contexts.append(dict(contexts[0], id="linux-amd64-default", cgo_enabled="1"))
    contexts.append(dict(contexts[0], id="linux-amd64-integration", cgo_enabled="1", tags=["integration"], tests=True))
    return contexts


def build_env(context):
    env = dict(os.environ, GOOS=context["goos"], GOARCH=context["goarch"], GOARM=context["goarm"],
               CGO_ENABLED=context["cgo_enabled"], GOTOOLCHAIN="local", GOENV="off", GOWORK="off", GOFLAGS="-mod=readonly",
               GOEXPERIMENT="", GOAMD64="v1", GO386="sse2")
    env.pop("GOVERSION", None)
    return env


def go_output(root, env, *args):
    # Never copy stderr to artifacts or logs: it can contain paths/credentials.
    result = subprocess.run([os.environ.get("GO", "go"), *args], cwd=root, env=env, capture_output=True)
    require(result.returncode == 0, "Go context enumeration failed (go " + args[0] + ")")
    return result.stdout


def package_path(value):
    return value.split(" [", 1)[0]


def modules_key(values):
    return sorted({(m["path"], m.get("version", "")) for m in values})


def go_path(value):
    require(isinstance(value, str) and len(value) <= 512 and bool(value), "invalid Go path")
    require(all(re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.~-]*", part) and part not in (".", "..")
                and not part.endswith(".") for part in value.split("/")), "invalid Go path")
    return value


def go_version(value, optional=False, osv=False):
    require(isinstance(value, str), "invalid Go module version")
    if optional and value == "":
        return value
    pattern = r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+incompatible)?"
    normalized = "v" + value if osv and not value.startswith("v") else value
    require(re.fullmatch(pattern, normalized), "invalid Go module version")
    return normalized


def advisory_id(value, source=False):
    pattern = r"GO-[0-9]{4}-[0-9]+" if source else r"(?:GO-[0-9]{4}-[0-9]+|CVE-[0-9]{4}-[0-9]{4,}|GHSA-[a-z0-9]{4}(?:-[a-z0-9]{4}){2})"
    require(isinstance(value, str) and re.fullmatch(pattern, value), "invalid advisory ID")
    return value


def module_index(modules):
    require(isinstance(modules, list) and modules, "missing trusted modules")
    result = {}
    for module in modules:
        require(isinstance(module, dict) and set(module) <= {"path", "version"}, "invalid module record")
        path = go_path(module.get("path"))
        version = go_version(module.get("version", ""), optional=True)
        require(path not in result or result[path] == version, "conflicting module coordinates")
        result[path] = version
    return result


def context_index(context):
    modules = module_index(context["modules"])
    packages = context["package_modules"]
    require(isinstance(packages, dict) and set(packages) == set(context["packages"]), "package context mismatch")
    for package, module in packages.items():
        go_path(package)
        require(isinstance(module, dict), "invalid package module context")
        path = go_path(module.get("path"))
        version = go_version(module.get("version", ""), optional=True)
        require(path in modules and modules[path] == version, "package module context mismatch")
    return modules, packages


def symbol(value, receiver=False):
    # Go identifiers plus compiler-generated closure/init/bound suffixes.
    # Receivers may have one pointer prefix. No paths, spaces, colons or prose.
    require(isinstance(value, str) and 0 < len(value) <= 512, "invalid Go symbol")
    name = value[1:] if receiver and value.startswith("*") else value
    parts = re.split(r"[.$#]", name)
    require(all(part and (part.isidentifier() or part.isdecimal()) for part in parts)
            and parts[0].isidentifier(), "invalid Go symbol")
    return value


def validate_trace(finding, modules, packages):
    require(isinstance(finding, dict) and set(finding) <= {"osv", "fixed_version", "trace"}, "invalid finding fields")
    advisory_id(finding.get("osv"), source=True)
    if "fixed_version" in finding:
        go_version(finding["fixed_version"], optional=True)
    trace = finding.get("trace")
    require(isinstance(trace, list) and trace, "incomplete finding trace")
    symbol_level = any(isinstance(frame, dict) and "function" in frame for frame in trace)
    require(symbol_level or len(trace) == 1, "conditional finding must have one frame")
    for frame in trace:
        require(isinstance(frame, dict) and set(frame) <= {"module", "version", "package", "function", "receiver", "position"}, "invalid trace fields")
        module = go_path(frame.get("module"))
        version = go_version(frame.get("version", ""), optional=True)
        require(module in modules and version == modules[module], "finding module/version outside selected context")
        if "package" in frame:
            package = go_path(frame["package"])
            require(package in packages and packages[package]["path"] == module and packages[package].get("version", "") == version,
                    "finding package outside selected context")
        if symbol_level:
            require("package" in frame and "function" in frame, "incomplete symbol trace")
            symbol(frame["function"])
            if "receiver" in frame:
                symbol(frame["receiver"], receiver=True)
            if "position" in frame:
                position = frame["position"]
                require(isinstance(position, dict) and set(position) <= {"filename", "offset", "line", "column"}, "invalid trace position")
                require("filename" not in position or isinstance(position["filename"], str), "invalid trace filename")
                require(all(type(value) is int and value >= 0 for key, value in position.items() if key != "filename"), "invalid trace position")
        else:
            require(not ({"function", "receiver", "position"} & set(frame)), "unexpected conditional trace structure")


def generate_contexts(root, output):
    root = Path(root).resolve()
    require(not Path(output).exists(), "refuse stale context manifest")
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    require(re.fullmatch(r"[a-f0-9]{40}", commit), "invalid source commit")
    contexts = []
    for context in target_contexts():
        env = build_env(context)
        version = go_output(root, env, "env", "GOVERSION").decode().strip()
        require(version == GO_VERSION, "Go 1.26.10 required for advisory evidence")
        args = ["list", "-deps", "-json"]
        if context["tests"]:
            args += ["-test", "-tags=integration"]
        packages = stream(go_output(root, env, *args, "./..."))
        roots, names, inputs, package_modules = set(), set(), [], {}
        modules = {("stdlib", "v1.26.10")}
        for pkg in packages:
            require(not pkg.get("Error") and not pkg.get("DepsErrors") and not pkg.get("Incomplete"), "incomplete package closure")
            name = package_path(pkg["ImportPath"])
            names.add(name)
            if not pkg.get("DepOnly", False):
                roots.add(name)
            module = pkg.get("Module")
            if module:
                module = module.get("Replace") or module
                require(not module["Path"].startswith(("/", ".")), "local module replacements require explicit review")
                modules.add((module["Path"], module.get("Version", "")))
            coordinate = {"path": module["Path"], "version": module.get("Version", "")} if module else {"path": "stdlib", "version": "v1.26.10"}
            package_modules[name] = coordinate
            fields = FILE_FIELDS + (("TestGoFiles", "XTestGoFiles", "TestEmbedFiles", "XTestEmbedFiles") if context["tests"] else ())
            for field in fields:
                for filename in pkg.get(field, []):
                    path = Path(pkg["Dir"]) / filename
                    if Path(filename).is_absolute():
                        # go list -test emits synthetic test-main source in the
                        # build cache. Hash the selected bytes without its path.
                        require(context["tests"] and name.endswith(".test") and field == "GoFiles", "unexpected absolute source input")
                        filename = "__generated_testmain.go"
                    require(".." not in Path(filename).parts, "unsafe selected source path")
                    inputs.append({"package": name, "kind": field, "file": filename,
                                   "sha256": digest(path.read_bytes())})
            if module:
                for filename in ("go.mod", "go.sum"):
                    path = Path(module["Dir"]) / filename
                    if path.is_file():
                        inputs.append({"package": module["Path"], "kind": "ModuleInput", "file": filename,
                                       "sha256": digest(path.read_bytes())})
        inputs = sorted({canonical(i): i for i in inputs}.values(), key=canonical)
        require(roots and inputs, "empty selected source context")
        context.update(go_version=version, commit=commit, roots=sorted(roots), packages=sorted(names), package_modules=package_modules,
                       modules=[dict(path=p, **({"version": v} if v else {})) for p, v in sorted(modules)],
                       inputs=inputs, input_digest=digest(canonical(inputs)))
        contexts.append(context)
    graph = stream(go_output(root, build_env(contexts[0]), "list", "-m", "-json", "all"))
    metadata_modules = [{"path": "stdlib", "version": "v1.26.10"}]
    for module in graph:
        require(not module.get("Replace"), "OSV replacement metadata requires review")
        metadata_modules.append({"path": module["Path"], "version": module.get("Version", "")})
    module_index(metadata_modules)
    manifest = {"schema": 1, "equivalence": "none; every release target and integration context retained", "contexts": contexts,
                "metadata_context": {"source": {"path": "go.mod", "type": "lockfile"}, "modules": metadata_modules,
                                     "go_mod_sha256": digest((root / "go.mod").read_bytes())}}
    verify_contexts(manifest)
    write_json(output, manifest)
    return manifest


def verify_contexts(manifest):
    contexts = manifest.get("contexts", [])
    require(isinstance(contexts, list) and len(contexts) == len(target_contexts()), "incomplete context matrix")
    require({c["id"] for c in contexts} == {c["id"] for c in target_contexts()}, "missing or duplicate context")
    commit = contexts[0]["commit"]
    for context, expected in zip(sorted(contexts, key=lambda c: c["id"]), sorted(target_contexts(), key=lambda c: c["id"])):
        require(all(context[k] == v for k, v in expected.items()), "context build flags mismatch")
        require(context["commit"] == commit and re.fullmatch(r"[a-f0-9]{40}", commit), "context commit mismatch")
        require(context["go_version"] == GO_VERSION, "context toolchain mismatch")
        require(context["roots"] and context["packages"] and context["modules"] and context["inputs"], "empty context inputs")
        context_index(context)
        require(digest(canonical(context["inputs"])) == context["input_digest"], "context input digest mismatch")


def evaluate(raw, completion, context, allow_reachable=False):
    records = stream(raw)
    require(completion.get("completed") is True and type(completion.get("exit_code")) is int and completion["exit_code"] == 0,
            "scanner did not complete successfully")
    require(completion.get("context") == context, "completion context mismatch")
    require(completion.get("result_sha256") == digest(raw), "completion bytes mismatch")
    require(timestamp(completion.get("started_at")) <= timestamp(completion.get("finished_at")), "completion time mismatch")
    require(set(records[0]) == {"config"}, "config must be first")
    configs = [r["config"] for r in records if "config" in r]
    sboms = [r["SBOM"] for r in records if "SBOM" in r]
    require(len(configs) == 1 and len(sboms) == 1, "expected exactly one config and SBOM")
    config = configs[0]
    require(isinstance(config, dict), "invalid config record")
    expected = dict(protocol_version="v1.0.0", scanner_name="govulncheck", scanner_version=TOOL,
                    go_version=context["go_version"], db=DATABASE, scan_mode="source", scan_level="symbol")
    require(all(config.get(k) == v for k, v in expected.items()), "unexpected scanner config/provenance")
    timestamp(config.get("db_last_modified"))
    sbom = sboms[0]
    require(isinstance(sbom, dict) and set(sbom) <= {"go_version", "roots", "modules"}, "invalid SBOM record")
    modules, packages = context_index(context)
    require(isinstance(sbom.get("roots"), list), "invalid SBOM roots")
    for root in sbom["roots"]:
        go_path(root)
    module_index(sbom.get("modules"))
    require(sbom.get("go_version") == context["go_version"], "SBOM Go context mismatch")
    require(set(sbom.get("roots", [])) == set(context["roots"]), "SBOM roots context mismatch")
    require(modules_key(sbom.get("modules", [])) == modules_key(context["modules"]), "SBOM modules context mismatch")
    known = set()
    findings = []
    for record in records:
        require(len(record) == 1 and next(iter(record)) in {"config", "SBOM", "progress", "osv", "finding"}, "unknown/error scanner record")
        if "osv" in record:
            require(isinstance(record["osv"], dict), "invalid advisory record")
            identifier = advisory_id(record["osv"].get("id"), source=True)
            known.add(identifier)
        if "finding" in record:
            finding = record["finding"]
            validate_trace(finding, modules, packages)
            findings.append(finding)
    require(all(f["osv"] in known for f in findings), "finding without advisory record")
    reachable = sorted({f["osv"] for f in findings if any(t.get("function") for t in f["trace"])})
    conditional = sorted({f["osv"] for f in findings} - set(reachable))
    require(allow_reachable or not reachable, "reachable symbol findings block: " + ", ".join(reachable))
    return {"status": "blocking" if reachable else "complete", "conditional": conditional, "reachable": reachable,
            "database": dict(expected, db_last_modified=config["db_last_modified"])}


def sanitize(records, context):
    # Caller must evaluate the raw stream first. Only validated identifiers and
    # trusted context coordinates are copied; diagnostics and paths are dropped.
    clean = []
    for record in records:
        if "config" in record:
            config = record["config"]
            # Only validated fixed config fields can reach retained artifacts.
            expected = {"protocol_version": "v1.0.0", "scanner_name": "govulncheck", "scanner_version": TOOL,
                        "go_version": GO_VERSION, "db": DATABASE, "scan_mode": "source", "scan_level": "symbol"}
            require(all(config.get(k) == v for k, v in expected.items()), "unexpected config during sanitization")
            timestamp(config.get("db_last_modified"))
            clean.append({"config": dict(expected, db_last_modified=config["db_last_modified"])})
        elif "SBOM" in record:
            sbom = record["SBOM"]
            clean.append({"SBOM": {"go_version": context["go_version"], "roots": context["roots"], "modules": context["modules"]}})
        elif "progress" in record:
            continue  # Free text can contain local paths or credentials.
        elif "osv" in record:
            clean.append({"osv": {"id": advisory_id(record["osv"]["id"], source=True)}})
        elif "finding" in record:
            finding = record["finding"]
            frames = []
            for frame in finding["trace"]:
                # Position filenames and OSV prose are unnecessary to the gate;
                # retain module/package/symbol traces, never arbitrary paths.
                frames.append({k: v for k, v in frame.items()
                               if k in ("module", "version", "package", "function", "receiver")})
            clean.append({"finding": {"osv": finding["osv"], "trace": frames,
                                       "fixed_version": finding.get("fixed_version", "")}})
        else:
            raise ValueError("unknown scanner record during sanitization")
    return clean


def run_process(command, root, env):
    # Raw streams are bounded on disk and removed with the temporary directory.
    # The retained artifact directory never contains raw stderr/stdout.
    started = now()
    with tempfile.TemporaryDirectory(prefix="sendit-scan-") as tmp:
        with open(Path(tmp) / "stdout", "w+b") as stdout, open(Path(tmp) / "stderr", "w+b") as stderr:
            process = subprocess.run(command, cwd=root, env=env, stdout=stdout, stderr=stderr, timeout=1800,
                                     preexec_fn=lambda: resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_BYTES, MAX_BYTES)))
            finished = now()
            stdout.seek(0, 2)
            size = stdout.tell()
            require(size <= MAX_BYTES, "scanner output exceeds size limit")
            stdout.seek(0)
            raw = stdout.read()
            stderr.seek(0, 2)
            error_bytes = stderr.tell()
    # Only after subprocess.run returns can completion be written.
    return raw, {"completed": True, "exit_code": process.returncode, "started_at": started,
                 "finished_at": finished, "result_sha256": digest(raw), "stderr_bytes": error_bytes}


def parse_exception(text):
    # Intentionally not a TOML implementation: one known exception, simple
    # unescaped double-quoted strings, bare ISO date, no alternate syntax.
    result = {}
    header = False
    for raw in text.split("\n"):
        line = raw.rstrip("\r").strip(" \t")
        require(all(ord(char) >= 32 or char == "\t" for char in line), "invalid exception control character")
        if not line or line.startswith("#"):
            continue
        if line == "[[IgnoredVulns]]":
            require(not header, "duplicate exception block")
            header = True
            continue
        require(header, "missing exception block")
        match = re.fullmatch(r'(id|ignoreUntil|reason)[ \t]*=[ \t]*("[^"\\\r\n]*"|[0-9]{4}-[0-9]{2}-[0-9]{2})', line)
        require(match is not None, "unsupported exception syntax")
        key, value = match.groups()
        require(key not in result, "duplicate exception field")
        require((key == "ignoreUntil") != value.startswith('"'), "invalid exception field syntax")
        result[key] = value.strip('"')
    require(set(result) == {"id", "ignoreUntil", "reason"} and result["id"] == "GO-2026-5932" and result["reason"], "invalid known exception")
    dt.date.fromisoformat(result["ignoreUntil"])
    return result


def check_exception(root, manifest):
    config = parse_exception((Path(root) / "osv-scanner.toml").read_text())
    expiry = dt.date.fromisoformat(config["ignoreUntil"])
    require(dt.datetime.now(dt.timezone.utc).date() < expiry,
            "GO-2026-5932 exception expired")
    require(expiry <= dt.date(2026, 12, 6), "exception renewal requires package-closure review")
    for context in manifest["contexts"]:
        require(not any(p == "golang.org/x/crypto/openpgp" or p.startswith("golang.org/x/crypto/openpgp/")
                        for p in context["packages"]), "GO-2026-5932 affected package entered source closure")
    return {"id": "GO-2026-5932", "expires": expiry.isoformat(), "affected_packages_absent": True,
            "contexts": [c["id"] for c in manifest["contexts"]]}


def osv_metadata(raw, completion):
    require(completion.get("action_outcome") in ("success", "failure"), "OSV action did not run")
    metadata = completion.get("metadata_context")
    require(isinstance(metadata, dict) and metadata.get("source") == {"path": "go.mod", "type": "lockfile"}, "missing expected OSV context")
    expected = module_index(metadata.get("modules"))
    data = stream(raw)
    require(len(data) == 1 and isinstance(data[0].get("results"), list) and data[0]["results"], "incomplete OSV metadata")
    require(set(data[0]) <= {"results", "experimental_config", "experimental_generic_findings", "license_summary", "image_metadata"}, "unexpected OSV result fields")
    analysis = data[0].get("experimental_config")
    require(isinstance(analysis, dict) and set(analysis) == {"licenses"}, "unexpected OSV analysis configuration")
    licenses = analysis["licenses"]
    require(isinstance(licenses, dict) and set(licenses) == {"summary", "allowlist"} and licenses["summary"] is False
            and licenses["allowlist"] in (None, []), "unexpected OSV license configuration")
    require(not data[0].get("experimental_generic_findings") and not data[0].get("image_metadata") and not data[0].get("license_summary"), "unexpected OSV analysis result")
    findings = []
    for result in data[0]["results"]:
        require(isinstance(result, dict) and set(result) <= {"source", "packages", "experimental_pes"} and not result.get("experimental_pes"), "unexpected OSV package source")
        source = result.get("source")
        require(isinstance(source, dict) and set(source) == {"path", "type"} and source["type"] == "lockfile"
                and source["path"] in ("go.mod", "./go.mod", "/github/workspace/go.mod"), "OSV source outside expected Go lockfile")
        require(isinstance(result.get("packages"), list) and result["packages"], "incomplete OSV package metadata")
        for package in result["packages"]:
            require(isinstance(package, dict) and set(package) <= {"package", "dependency_groups", "vulnerabilities", "groups", "licenses", "license_violations"}, "unexpected OSV package record")
            require(not package.get("license_violations"), "unexpected OSV license analysis")
            coordinate = package["package"]
            require(isinstance(coordinate, dict) and set(coordinate) <= {"name", "version", "ecosystem", "purl", "commit", "deprecated"}, "unexpected OSV coordinate fields")
            require(coordinate.get("ecosystem") == "Go", "unexpected OSV ecosystem")
            name = go_path(coordinate.get("name"))
            version = go_version(coordinate.get("version"), optional=True, osv=True)
            require(name in expected and version == expected[name], "OSV coordinate outside expected module graph")
            vulnerabilities = package.get("vulnerabilities", [])
            require(isinstance(vulnerabilities, list), "invalid OSV advisories")
            identifiers = []
            for vulnerability in vulnerabilities:
                require(isinstance(vulnerability, dict) and set(vulnerability) <= {
                    "id", "schema_version", "modified", "published", "withdrawn", "aliases", "related", "summary", "details", "affected", "references", "severity", "credits", "database_specific"}, "unexpected OSV advisory fields")
                identifiers.append(advisory_id(vulnerability.get("id")))
            groups = package.get("groups", [])
            require(isinstance(groups, list), "invalid OSV groups")
            for group in groups:
                require(isinstance(group, dict) and set(group) <= {"ids", "aliases", "experimental_analysis", "max_severity"}
                        and not group.get("experimental_analysis"), "unexpected OSV reachability analysis")
            findings.append({"package": {"name": name, "version": version, "ecosystem": "Go"}, "advisories": sorted(set(identifiers))})
    require(completion["action_outcome"] != "failure" or any(p["advisories"] for p in findings), "OSV failure without completed finding evidence")
    return {"scanner": "osv-scanner", "version": OSV_TOOL, "database": "https://api.osv.dev",
            "classification": "conditional metadata; no reachability claim", "packages": findings,
            "completion": completion}


def scan(root, output, osv_result, osv_outcome):
    root, output = Path(root).resolve(), Path(output).resolve()
    require(not output.exists(), "refuse stale evidence directory")
    output.mkdir(parents=True)
    manifest = generate_contexts(root, output / "contexts.json")
    exception = check_exception(root, manifest)
    write_json(output / "exception.json", exception)
    statuses = []
    for context in manifest["contexts"]:
        dest = output / context["id"]
        dest.mkdir()
        write_json(dest / "context.json", context)
        command = ["govulncheck", "-json", "-mode=source", "-scan=symbol", "-db=" + DATABASE]
        if context["tests"]:
            command += ["-test", "-tags=integration"]
        command += ["./..."]
        try:
            raw, completion = run_process(command, root, build_env(context))
            completion["context"] = context
            # Keep original process/byte evidence even if parsing fails.
            completion["raw_result_sha256"] = completion["result_sha256"]
            write_json(dest / "completion.json", completion)
            # Structural/context validation precedes every retained source byte.
            # Valid reachable findings are retained and still block the gate.
            result = evaluate(raw, completion, context, allow_reachable=True)
            clean = sanitize(stream(raw), context)
            clean_bytes = b"\n".join(canonical(r) for r in clean) + b"\n"
            (dest / "govulncheck.json").write_bytes(clean_bytes)
            completion["result_sha256"] = digest(clean_bytes)
            write_json(dest / "completion.json", completion)
            evaluate(clean_bytes, completion, context, allow_reachable=True)
            write_json(dest / "evaluation.json", result)
            statuses.append({"context": context["id"], "status": result["status"]})
        except (ValueError, KeyError, TypeError, AttributeError, OSError, subprocess.SubprocessError) as error:
            reason = str(error) if isinstance(error, EvidenceError) else type(error).__name__
            write_json(dest / "evaluation.json", {"status": "blocking", "reason": reason})
            statuses.append({"context": context["id"], "status": "blocking", "reason": reason})
    try:
        require(osv_outcome in ("success", "failure"), "OSV action did not run")
        require(Path(osv_result).stat().st_size <= MAX_BYTES, "OSV metadata exceeds size limit")
        raw = Path(osv_result).read_bytes()
        # The pinned action returns after its scanner process. The upstream
        # source command writes JSON only on a complete clean/metadata-finding
        # result; errors return before writing JSON. Preserve its actual outcome.
        completion = {"action_outcome": osv_outcome, "result_sha256": digest(raw), "observed_at": now(),
                      "commit": manifest["contexts"][0]["commit"],
                      "action": "0a65f82af05cd58cf587827b0bb0489548c18346", "metadata_context": manifest["metadata_context"]}
        write_json(output / "osv-completion.json", completion)
        write_json(output / "osv-metadata.json", osv_metadata(raw, completion))
        osv_status = "complete"
    except (ValueError, KeyError, TypeError, AttributeError, OSError):
        osv_status = "blocking"
    verify_contexts(manifest)
    write_json(output / "summary.json", {"contexts": statuses, "osv_metadata": osv_status})
    require(len(statuses) == len(target_contexts()) and all(s["status"] == "complete" for s in statuses) and osv_status == "complete",
            "vulnerability evidence incomplete or reachable findings; inspect sanitized artifacts")
    print("All 11 source contexts complete; conditional metadata retained")


def main():
    command, *args = sys.argv[1:]
    if command == "evaluate":
        raw, completion, context = args
        print(json.dumps(evaluate(Path(raw).read_bytes(), read_json(completion), read_json(context))))
    elif command == "contexts":
        mode, *rest = args
        if mode == "generate":
            generate_contexts(*rest)
        elif mode == "verify":
            verify_contexts(read_json(rest[0]))
        else:
            raise ValueError("unknown context operation")
    elif command == "scan":
        scan(*args)
    else:
        raise ValueError("unknown operation")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, AttributeError, OSError, subprocess.SubprocessError) as error:
        # Exceptions can include raw paths; emit only our fixed validation messages.
        print("Blocking evidence: " + (str(error) if isinstance(error, EvidenceError) else type(error).__name__), file=sys.stderr)
        sys.exit(1)
