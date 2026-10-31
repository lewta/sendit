"""Offline protocol and scanner-process boundary regressions."""
import copy
import hashlib
import importlib.util
import json
import os
import re
from pathlib import Path
import subprocess
import tempfile
import unittest
import sys

SCRIPTS = Path(__file__).resolve().parent
SHA = "a" * 40
CTX = {"id": "linux-amd64", "goos": "linux", "goarch": "amd64", "goarm": "",
       "tags": [], "tests": False, "cgo_enabled": "0", "go_version": "go1.26.10",
       "commit": SHA, "roots": ["example.org/fixture"],
       "packages": ["example.org/fixture", "net/http"],
       "package_modules": {"example.org/fixture": {"path": "example.org/fixture"}, "net/http": {"path": "stdlib", "version": "v1.26.10"}},
       "modules": [{"path": "example.org/fixture"},
       {"path": "stdlib", "version": "v1.26.10"}],
       "inputs": [{"package": "example.org/fixture", "kind": "GoFiles", "file": "main.go", "sha256": "b" * 64}],
       "input_digest": "c" * 64}
CONFIG = {"protocol_version": "v1.0.0", "scanner_name": "govulncheck",
          "scanner_version": "v1.9.0", "go_version": "go1.26.10",
          "db": "https://vuln.go.dev", "db_last_modified": "2026-10-27T12:00:00Z",
          "scan_mode": "source", "scan_level": "symbol"}
META = {"source": {"path": "go.mod", "type": "lockfile"},
        "modules": [{"path": "example.org/fixture"}, {"path": "stdlib", "version": "v1.26.10"}]}


def load_evidence():
    spec = importlib.util.spec_from_file_location("security_evidence", SCRIPTS / "security-evidence.py")
    evidence = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(evidence)
    return evidence


def osv_fixture(findings=False):
    package = {"package": {"name": "stdlib", "version": "1.26.10", "ecosystem": "Go"}}
    if findings:
        package["vulnerabilities"] = [{"id": "GO-2026-6505", "summary": "token=fixture-secret"}]
    return {"results": [{"source": {"path": "go.mod", "type": "lockfile"}, "packages": [package]}],
            "experimental_config": {"licenses": {"summary": False, "allowlist": None}}}


class EvaluatorTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.messages = [{"config": copy.deepcopy(CONFIG)}, {"SBOM": {
            "go_version": "go1.26.10", "roots": CTX["roots"], "modules": CTX["modules"]}}]

    def run_gate(self, messages=None, completion=True, status=0, mutate=None, raw=None):
        context = self.root / "context.json"
        context.write_text(json.dumps(CTX))
        result = self.root / "result.json"
        result.write_text(raw if raw is not None else "\n".join(json.dumps(m) for m in (messages or self.messages)))
        sidecar = {"context": copy.deepcopy(CTX), "completed": True, "exit_code": status,
                   "result_sha256": hashlib.sha256(result.read_bytes()).hexdigest(),
                   "started_at": "2026-10-28T00:00:00Z", "finished_at": "2026-10-28T00:01:00Z"}
        if mutate:
            mutate(sidecar)
        done = self.root / "completion.json"
        if completion:
            done.write_text(json.dumps(sidecar))
        return subprocess.run(["bash", str(SCRIPTS / "evaluate-govulncheck.sh"),
                               str(result), str(done), str(context)], capture_output=True, text=True)

    def test_complete_clean_source_analysis_passes(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def finding(self, frame):
        frame = dict(frame)
        if frame.get("module") == "stdlib":
            frame.setdefault("version", "v1.26.10")
        return self.messages + [{"osv": {"id": "GO-2099-0001"}},
                                {"finding": {"osv": "GO-2099-0001", "trace": [frame]}}]

    def test_malformed_levels_types_or_unselected_trace_coordinates_block(self):
        valid = {"module": "stdlib", "version": "v1.26.10"}
        traces = [[valid, valid], [dict(valid, receiver="Client")], [dict(valid, position={"line": 1})],
                  [dict(valid, module="not.in.context")], [dict(valid, version="v1.26.9")],
                  [dict(valid, package="unexpected/package")], [dict(valid, package=[])],
                  [dict(valid, function=False)], [dict(valid, receiver=[])],
                  [dict(valid, package="net/http", function="Serve"), valid],
                  [dict(valid, package="net/http", function="Serve", position={"line": "1"})]]
        for trace in traces:
            messages = self.finding(valid)
            messages[-1]["finding"]["trace"] = trace
            with self.subTest(trace=trace):
                self.assertNotEqual(self.run_gate(messages).returncode, 0)

    def test_reachable_symbol_blocks_even_with_exit_zero(self):
        result = self.run_gate(self.finding({"module": "stdlib", "package": "net/http", "function": "Serve"}))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("reachable", result.stderr)

    def test_module_and_package_matches_remain_conditional(self):
        for frame in ({"module": "stdlib"}, {"module": "stdlib", "package": "net/http"}):
            with self.subTest(frame=frame):
                result = self.run_gate(self.finding(frame))
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("conditional", result.stdout)

    def test_malformed_truncated_empty_or_trailing_junk_blocks(self):
        for raw in ("", "{", "{}", "{\"config\":", "\n".join(json.dumps(m) for m in self.messages) + "\n{", "NaN"):
            with self.subTest(raw=raw):
                self.assertNotEqual(self.run_gate(raw=raw).returncode, 0)

    def test_non_json_whitespace_and_duplicate_keys_block(self):
        valid = "\n".join(json.dumps(m) for m in self.messages)
        for raw in (valid + "\u00a0", valid.replace('"scan_mode": "source"', '"scan_mode": "binary", "scan_mode": "source"')):
            with self.subTest(raw=raw):
                self.assertNotEqual(self.run_gate(raw=raw).returncode, 0)

    def test_eof_valid_prefix_still_needs_successful_completion(self):
        self.assertNotEqual(self.run_gate(completion=False).returncode, 0)
        self.assertNotEqual(self.run_gate(status=2).returncode, 0)
        self.assertNotEqual(self.run_gate(mutate=lambda s: s.update(completed=False)).returncode, 0)

    def test_error_unknown_or_malformed_finding_blocks(self):
        for entry in ({"error": "database unavailable"}, {"finding": {"osv": "GO-2099-0001"}},
                      {"finding": {"osv": "GO-2099-0001", "trace": [{"module": "stdlib"}]}}):
            with self.subTest(entry=entry):
                self.assertNotEqual(self.run_gate(self.messages + [entry]).returncode, 0)

    def test_config_provenance_mode_level_and_sbom_are_required(self):
        for key, value in (("scanner_version", "v0.0.0"), ("scan_mode", "binary"),
                           ("scan_level", "package"), ("db", ""), ("db_last_modified", "invalid"),
                           ("go_version", "go1.27.2"), ("protocol_version", "v9")):
            messages = copy.deepcopy(self.messages)
            messages[0]["config"][key] = value
            with self.subTest(key=key):
                self.assertNotEqual(self.run_gate(messages).returncode, 0)
        for messages in (self.messages[:1], self.messages[1:], self.messages + self.messages[:1]):
            self.assertNotEqual(self.run_gate(messages).returncode, 0)
        for key, value in (("roots", ["wrong/root"]), ("modules", []), ("go_version", "go1.27.2")):
            messages = copy.deepcopy(self.messages)
            messages[1]["SBOM"][key] = value
            self.assertNotEqual(self.run_gate(messages).returncode, 0)

    def test_completion_is_bound_to_bytes_context_and_time(self):
        for mutation in (lambda s: s.update(result_sha256="d" * 64),
                         lambda s: s["context"].update(goos="darwin"),
                         lambda s: s["context"].update(commit="d" * 40),
                         lambda s: s.update(finished_at="2026-10-27T00:00:00Z")):
            self.assertNotEqual(self.run_gate(mutate=mutation).returncode, 0)


class WorkflowPolicyTest(unittest.TestCase):
    def test_context_table_covers_the_release_configuration(self):
        release = (SCRIPTS.parent.parent / ".goreleaser.yaml").read_text()
        builds = release.split("builds:\n", 1)[1].split("\nnotarize:", 1)[0]
        # Restricted policy read: unfamiliar/multiple build schemas require review.
        self.assertEqual(len(re.findall(r"^    goos:", builds, re.M)), 1)
        axes = {}
        for key in ("goos", "goarch", "goarm"):
            block = re.search(r"^    " + key + r":\n((?:      - .*\n)+)", builds, re.M)
            self.assertIsNotNone(block)
            axes[key] = [line.strip()[2:].strip('"') for line in block[1].splitlines()]
        ignored = set(re.findall(r"- goos: (\w+)\n\s+goarch: [\"]?(\w+)", builds))
        actual = {(goos, arch) for goos in axes["goos"] for arch in axes["goarch"]} - ignored
        self.assertEqual(actual, {("linux", "amd64"), ("linux", "arm64"), ("linux", "arm"), ("linux", "386"),
                                  ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64"),
                                  ("windows", "arm64"), ("freebsd", "amd64")})
        self.assertEqual(axes["goarm"], ["7"])
        self.assertIn("- CGO_ENABLED=0", builds)

    def test_pinned_source_evidence_is_an_existing_required_job(self):
        workflow = (SCRIPTS.parent / "workflows/security.yml").read_text()
        self.assertNotIn("govulncheck@latest", workflow)
        self.assertIn("govulncheck@v1.9.0", workflow)
        self.assertIn("google/osv-scanner-action/osv-scanner-action@9a96bc4d2b09685b6142bcfe25a3dc17cb2d7347", workflow)
        self.assertNotIn("go install github.com/google/osv-scanner", workflow)
        self.assertIn("--no-call-analysis=go", workflow)
        self.assertIn("--all-packages", workflow)
        for flag in ("--lockfile=go.mod", "--config=osv-scanner.toml", "--format=json",
                     "--output-file=.vulnerability-raw/osv.json"):
            self.assertIn(flag, workflow)
        self.assertNotIn("--no-resolve", workflow)
        self.assertIn("steps.osv.outcome", workflow)
        self.assertIn("run-vulnerability-scans.sh", workflow)
        self.assertIn("if: always()", workflow)
        self.assertIn("retention-days: 14", workflow)
        self.assertEqual(workflow.count("continue-on-error: true"), 1)
        self.assertNotIn("secrets.", workflow)
        for line in workflow.splitlines():
            if "uses:" in line:
                self.assertRegex(line, r"@[0-9a-f]{40}(?:\s|$)")
        makefile = (SCRIPTS.parent.parent / "Makefile").read_text()
        self.assertRegex(makefile, r"verify:.*vulnerability-check")
        ci = (SCRIPTS.parent / "workflows/ci.yml").read_text()
        self.assertIn("make vulnerability-check", ci)


class ScannerBoundaryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / "go.mod").write_text("module example.org/fixture\n\ngo 1.26.10\n")
        (self.root / "main.go").write_text("package fixture\nconst X = 1\n")
        (self.root / "osv-scanner.toml").write_text('[[IgnoredVulns]]\nid = "GO-2026-5932"\nignoreUntil = 2026-12-06\nreason = "Package closure checked"\n')
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        subprocess.run(["git", "-C", str(self.root), "add", "."], check=True)
        subprocess.run(["git", "-C", str(self.root), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "-qm", "fixture"], check=True)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        fake = self.bin / "govulncheck"
        fake.write_text("#!" + sys.executable + "\n" + '''import json, os, pathlib, sys
out = pathlib.Path(os.environ["FAKE_EVIDENCE"])
contexts = json.loads((out / "contexts.json").read_text())["contexts"]
ctx = next(c for c in contexts if c["goos"] == os.environ["GOOS"] and c["goarch"] == os.environ["GOARCH"] and c["cgo_enabled"] == os.environ["CGO_ENABLED"] and c["tests"] == ("-test" in sys.argv))
assert "-json" in sys.argv and "-mode=source" in sys.argv and "-scan=symbol" in sys.argv and sys.argv[-1] == "./..."
assert ("-tags=integration" in sys.argv) == ctx["tests"]
assert not (out / ctx["id"] / "completion.json").exists(), "completion before process return"
print(json.dumps({"config": {"protocol_version": "v1.0.0", "scanner_name": "govulncheck", "scanner_version": "v1.9.0", "go_version": "go1.26.10", "db": "https://vuln.go.dev", "db_last_modified": "2026-10-27T12:00:00Z", "scan_mode": "source", "scan_level": "symbol", "diagnostic": "token=fixture-secret /Users/private/build"}}))
sbom = {"go_version": ctx["go_version"], "roots": ctx["roots"], "modules": ctx["modules"]}
if os.environ.get("FAKE_FAILURE") == "inject":
 field, value = os.environ["INJECT_FIELD"], os.environ["INJECT_VALUE"]
 if field == "sbom-root": sbom["roots"] = [value]
 if field == "sbom-module": sbom["modules"] = [{"path": value}]
 if field == "sbom-version": sbom["modules"] = [{"path": "stdlib", "version": value}]
print(json.dumps({"SBOM": sbom}))
print(json.dumps({"progress": {"message": "token=fixture-secret /Users/private/build"}}))
print("token=fixture-secret /Users/private/build", file=sys.stderr)
if os.environ.get("FAKE_FAILURE") == "database": sys.exit(2)
if os.environ.get("FAKE_FAILURE") == "truncated": print("{")
if os.environ.get("FAKE_FAILURE") == "reachable":
 print(json.dumps({"osv": {"id": "GO-2099-0001", "summary": "token=fixture-secret"}}))
 print(json.dumps({"finding": {"osv": "GO-2099-0001", "trace": [{"module": "example.org/fixture", "package": "example.org/fixture", "function": "Entry", "position": {"filename": "/Users/private/build.go", "line": 1}}]}}))
if os.environ.get("FAKE_FAILURE") == "inject":
 field = os.environ["INJECT_FIELD"]
 value = os.environ["INJECT_VALUE"]
 data = {"osv": "GO-2099-0001", "fixed_version": "v1.0.0", "trace": [{"module": "example.org/fixture", "package": "example.org/fixture", "function": "Entry", "receiver": "Client"}]}
 if field.startswith("sbom-"):
  sbom = {"go_version": ctx["go_version"], "roots": ctx["roots"], "modules": ctx["modules"]}
  if field == "sbom-root": sbom["roots"] = [value]
  if field == "sbom-module": sbom["modules"] = [{"path": value}]
  if field == "sbom-version": sbom["modules"] = [{"path": "stdlib", "version": value}]
  pass
 else:
  if field == "fixed_version": data[field] = value
  elif field == "osv": data[field] = value
  else: data["trace"][0][field] = value
  print(json.dumps({"osv": {"id": "GO-2099-0001"}}))
  print(json.dumps({"finding": data}))
''')
        fake.chmod(0o755)
        osv = self.bin / "osv-scanner"
        osv.write_text("#!" + sys.executable + "\n" + '''import json, os, sys
assert sys.argv[1:3] == ["scan", "source"]
assert "--no-call-analysis=go" in sys.argv
assert "--format=json" in sys.argv
if os.environ.get("FAKE_FAILURE") == "osv-error": sys.exit(2)
data = {"experimental_config": {"licenses": {"summary": False, "allowlist": None}}, "results": [{"source": {"path": "go.mod", "type": "lockfile"}, "packages": [{"package": {"name": "stdlib", "version": "1.26.10", "ecosystem": "Go"}, "vulnerabilities": [{"id": "GO-2026-6505", "summary": "token=fixture-secret"}]}]}]}
if os.environ.get("FAKE_FAILURE") == "osv-inject":
 field, value = os.environ["INJECT_OSV_FIELD"], os.environ["INJECT_VALUE"]
 if field == "source": data["results"][0]["source"]["path"] = value
 elif field == "id": data["results"][0]["packages"][0]["vulnerabilities"][0]["id"] = value
 else: data["results"][0]["packages"][0]["package"][field] = value
print(json.dumps(data))
sys.exit(2 if os.environ.get("FAKE_FAILURE") == "osv-error" else 1)
''')
        osv.chmod(0o755)
        self.output = self.root / "evidence"

    def run_scans(self, failure="", **extra):
        env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                   FAKE_EVIDENCE=str(self.output), FAKE_FAILURE=failure)
        env.update(extra)
        # The pinned action's process boundary: only completed scans produce JSON.
        osv = subprocess.run([str(self.bin / "osv-scanner"), "scan", "source", "--no-call-analysis=go", "--format=json"], env=env, capture_output=True)
        raw = self.root / "osv-raw.json"
        raw.write_bytes(osv.stdout)
        return subprocess.run(["bash", str(SCRIPTS / "run-vulnerability-scans.sh"), str(self.root), str(self.output), str(raw),
                               "success" if osv.returncode == 0 else "failure"], env=env, capture_output=True, text=True)

    def test_wrapper_records_return_status_then_evaluates_and_sanitizes(self):
        result = self.run_scans()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(list(self.output.glob("*/completion.json"))), 11)
        for path in self.output.rglob("*.json"):
            content = path.read_text()
            self.assertNotIn("fixture-secret", content, str(path))
            self.assertNotIn("/Users/private", content, str(path))
        osv = json.loads((self.output / "osv-metadata.json").read_text())
        self.assertEqual(osv["classification"], "conditional metadata; no reachability claim")
        self.assertIn("GO-2026-6505", json.dumps(osv))

    def test_wrapper_blocks_errors_truncation_reachable_and_osv_failure(self):
        for failure in ("database", "truncated", "reachable", "osv-error"):
            with self.subTest(failure=failure):
                self.output = self.root / failure
                result = self.run_scans(failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertTrue((self.output / "summary.json").exists(), result.stderr)
                self.assertEqual(len(list(self.output.glob("*/completion.json"))), 11)
                if failure == "reachable":
                    retained = json.loads((self.output / "linux-amd64" / "evaluation.json").read_text())
                    self.assertEqual(retained["reachable"], ["GO-2099-0001"])
                    self.assertTrue((self.output / "linux-amd64" / "govulncheck.json").exists())

    def test_invalid_source_fields_never_enter_failed_artifacts(self):
        for value in ("C:/Users/private/token-secret", "prefix /Users/private/token-secret", "token-secret"):
            for field in ("sbom-root", "sbom-module", "sbom-version", "module", "version", "package", "function", "receiver", "fixed_version", "osv"):
                with self.subTest(value=value, field=field):
                    self.output = self.root / ("injection-" + str(abs(hash((field, value)))))
                    result = self.run_scans("inject", INJECT_FIELD=field, INJECT_VALUE=value)
                    self.assertNotEqual(result.returncode, 0)
                    for artifact in self.output.rglob("*.json"):
                        self.assertNotIn(value, artifact.read_text(), str(artifact))

    def test_invalid_osv_fields_never_enter_failed_artifacts(self):
        for value in ("C:/Users/private/token-secret", "prefix /Users/private/token-secret", "token-secret"):
            for field in ("name", "version", "ecosystem", "source", "id"):
                with self.subTest(value=value, field=field):
                    self.output = self.root / ("osv-injection-" + str(abs(hash((field, value)))))
                    result = self.run_scans("osv-inject", INJECT_OSV_FIELD=field, INJECT_VALUE=value)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse((self.output / "osv-metadata.json").exists())
                    for artifact in self.output.rglob("*.json"):
                        self.assertNotIn(value, artifact.read_text(), str(artifact))

    def test_exception_expiry_blocks(self):
        (self.root / "osv-scanner.toml").write_text('[[IgnoredVulns]]\nid = "GO-2026-5932"\nignoreUntil = 2020-01-01\nreason = "expired"\n')
        result = self.run_scans()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("expired", result.stderr)

    def test_exception_blocks_any_affected_package_in_any_context(self):
        for package in ("golang.org/x/crypto/openpgp", "golang.org/x/crypto/openpgp/armor"):
            with self.subTest(package=package):
                with self.assertRaisesRegex(ValueError, "affected package"):
                    load_evidence().check_exception(self.root, {"contexts": [{"id": "windows-arm64", "packages": [package]}]})


class MetadataValidationTest(unittest.TestCase):
    def evaluate(self, data, outcome="failure"):
        return load_evidence().osv_metadata(json.dumps(data).encode(), {"action_outcome": outcome, "metadata_context": META})

    def test_go_lockfile_clean_and_finding_results_remain_conditional(self):
        for finding in (False, True):
            result = self.evaluate(osv_fixture(finding), "failure" if finding else "success")
            self.assertEqual(result["classification"], "conditional metadata; no reachability claim")

    def test_failure_outcome_without_a_finding_is_not_complete(self):
        with self.assertRaises(ValueError):
            self.evaluate(osv_fixture(False), "failure")

    def test_error_incomplete_unknown_source_and_ecosystem_block(self):
        for key, value in (("error", "database failed"), ("incomplete", True), ("unexpected", {})):
            data = osv_fixture()
            data[key] = value
            with self.subTest(key=key):
                with self.assertRaises(ValueError):
                    self.evaluate(data)
        for field, value in (("path", "other/go.mod"), ("type", "sbom")):
            data = osv_fixture()
            data["results"][0]["source"][field] = value
            with self.assertRaises(ValueError):
                self.evaluate(data)
        data = osv_fixture()
        data["results"][0]["packages"][0]["package"]["ecosystem"] = "Other"
        with self.assertRaises(ValueError):
            self.evaluate(data)

    def test_osv_field_substitutions_and_unknown_coordinates_block(self):
        for value in ("C:/Users/private/token-secret", "prefix /Users/private/token-secret", "token-secret"):
            for field in ("name", "version", "ecosystem", "source", "id"):
                data = osv_fixture(True)
                if field == "source":
                    data["results"][0]["source"]["path"] = value
                elif field == "id":
                    data["results"][0]["packages"][0]["vulnerabilities"][0]["id"] = value
                else:
                    data["results"][0]["packages"][0]["package"][field] = value
                with self.subTest(field=field, value=value):
                    with self.assertRaises(ValueError):
                        self.evaluate(data)
        for field, value in (("name", "example.org/unselected"), ("version", "9.9.9")):
            data = osv_fixture()
            data["results"][0]["packages"][0]["package"][field] = value
            with self.assertRaises(ValueError):
                self.evaluate(data)


class ExceptionSyntaxTest(unittest.TestCase):
    def test_only_one_known_simple_exception_block_is_accepted(self):
        parser = load_evidence().parse_exception
        good = '[[IgnoredVulns]]\nid = "GO-2026-5932"\nignoreUntil = 2026-12-06\nreason = "Closure evidence"\n'
        self.assertEqual(parser(good)["ignoreUntil"], "2026-12-06")
        for text in (good + good, good + 'id = "GO-2026-5932"\n', good.replace('"GO-2026-5932"', "'GO-2026-5932'"),
                     good.replace("ignoreUntil =", '"ignoreUntil" ='), good.replace("2026-12-06", '"2026-12-06"'),
                     good.replace("reason =", "unknown ="), good.replace('"Closure evidence"', '"""Closure evidence"""'),
                     good.replace("Closure evidence", "bad\x00reason"), good.replace("id =", "id\u00a0="),
                     good.replace("2026-12-06", "2026-02-30")):
            with self.subTest(text=text):
                with self.assertRaises(ValueError):
                    parser(text)

    def test_system_python_can_load_the_entrypoint(self):
        result = subprocess.run(["/usr/bin/python3", "-B", str(SCRIPTS / "security-evidence.py"), "contexts", "verify", "/nonexistent-context-fixture"], capture_output=True, text=True)
        self.assertNotIn("Traceback", result.stderr)
        self.assertNotIn("tomllib", result.stderr)

    def test_osv_missing_truncated_empty_or_nonexecuted_result_blocks(self):
        spec = importlib.util.spec_from_file_location("security_evidence", SCRIPTS / "security-evidence.py")
        evidence = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(evidence)
        for raw, outcome in ((b"", "failure"), (b'{"results":', "failure"), (b'{"results":[]}', "success"),
                             (b'{"results":[{"packages":[]}]}', "success"),
                             (b'{"results":[{"packages":[]}]}', "skipped")):
            with self.subTest(raw=raw, outcome=outcome):
                with self.assertRaises(ValueError):
                    evidence.osv_metadata(raw, {"action_outcome": outcome, "metadata_context": META})


if __name__ == "__main__":
    unittest.main()
