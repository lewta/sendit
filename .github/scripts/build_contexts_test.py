"""Use real go list selection: identical package/module lists are not equivalence."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parent


class BuildContextsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / "go.mod").write_text("module example.org/fixture\n\ngo 1.26.10\n")
        (self.root / "common.go").write_text("package fixture\nconst Common = 1\n")
        for platform in ("linux", "darwin", "windows", "freebsd"):
            (self.root / ("platform_" + platform + ".go")).write_text("package fixture\nconst Platform = \"" + platform + "\"\n")
        (self.root / "integration_test.go").write_text("//go:build integration\n\npackage fixture\nimport \"testing\"\nfunc TestIntegration(t *testing.T) {}\n")
        subprocess.run(["git", "init", "-q", str(self.root)], check=True)
        subprocess.run(["git", "-C", str(self.root), "add", "."], check=True)
        subprocess.run(["git", "-C", str(self.root), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.org",
                        "commit", "-qm", "fixture"], check=True)
        self.output = self.root / "contexts.json"

    def command(self, *args):
        return subprocess.run(["bash", str(SCRIPTS / "verify-build-contexts.sh"), *map(str, args)], capture_output=True, text=True)

    def test_retains_every_release_target_and_test_context(self):
        result = self.command("generate", self.root, self.output)
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads(self.output.read_text())
        contexts = {c["id"]: c for c in manifest["contexts"]}
        self.assertEqual(set(contexts), {"linux-amd64", "linux-arm64", "linux-arm7", "linux-386",
                                       "darwin-amd64", "darwin-arm64", "windows-amd64", "windows-arm64",
                                       "freebsd-amd64", "linux-amd64-integration", "linux-amd64-default"})
        self.assertEqual(contexts["linux-amd64-default"]["cgo_enabled"], "1")
        self.assertEqual(contexts["linux-amd64"]["cgo_enabled"], "0")
        linux, darwin = contexts["linux-amd64"], contexts["darwin-amd64"]
        self.assertEqual(linux["packages"], darwin["packages"])
        self.assertEqual(linux["modules"], darwin["modules"])
        self.assertNotEqual(linux["inputs"], darwin["inputs"])
        self.assertNotEqual(linux["input_digest"], darwin["input_digest"])
        integration = contexts["linux-amd64-integration"]
        self.assertTrue(integration["tests"])
        self.assertEqual(integration["cgo_enabled"], "1")
        self.assertEqual(integration["tags"], ["integration"])
        self.assertTrue(any(i["file"].endswith("integration_test.go") for i in integration["inputs"]))
        self.assertNotIn(str(self.root), self.output.read_text())
        self.assertEqual(self.command("verify", self.output).returncode, 0)
        for broken in ([], manifest["contexts"][:-1], manifest["contexts"] + manifest["contexts"][:1]):
            self.output.write_text(json.dumps({"contexts": broken}))
            self.assertNotEqual(self.command("verify", self.output).returncode, 0)

    def test_go_list_failure_produces_no_success_manifest(self):
        (self.root / "common.go").write_text('package fixture\nimport _ "missing.invalid/dependency"\n')
        self.assertNotEqual(self.command("generate", self.root, self.output).returncode, 0)
        self.assertFalse(self.output.exists())


if __name__ == "__main__":
    unittest.main()
