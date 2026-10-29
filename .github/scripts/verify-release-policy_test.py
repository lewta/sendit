"""Secretless workflow policy gate; parse jobs to check trust/privilege ordering."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class ReleasePolicy(unittest.TestCase):
    def test_historical_identity_retired_and_no_automatic_privilege_trigger(self):
        self.assertFalse((ROOT / '.github/workflows/release.yml').exists())
        for name in ('release-production', 'release-aur', 'release-taps'):
            text = (ROOT / f'.github/workflows/{name}.yml').read_text()
            self.assertNotRegex(text, r'(?m)^  (push|workflow_run):')
            self.assertIn('workflow_dispatch:', text)
            self.assertIn('release-production', text)
            self.assertIn('needs: authorize', text)

    def test_control_validation_precedes_candidate_and_credentials(self):
        text = (ROOT / '.github/workflows/release-authorize.yml').read_text()
        self.assertIn('contents: read', text)
        self.assertNotIn('secrets.', text)
        self.assertIn('git fetch --force --prune origin', text)
        self.assertLess(text.index('test "$REPOSITORY" = lewta/sendit'), text.index('actions/checkout@'))
        self.assertLess(text.index('test "$DISPATCH_REF" = refs/heads/main'), text.index('actions/checkout@'))
        self.assertLess(text.index('test "$REF_PROTECTED" = true'), text.index('actions/checkout@'))
        self.assertIn('WORKFLOW_SHA: ${{ github.workflow_sha }}', text)
        self.assertLess(text.index('git fetch'), text.index('validate-release-input.sh'))
        for name in ('release-production', 'release-aur'):
            text = (ROOT / f'.github/workflows/{name}.yml').read_text()
            self.assertIn('ref: ${{ needs.authorize.outputs.sha }}', text)
            self.assertIn('ref: ${{ needs.authorize.outputs.control_sha }}', text)
            self.assertLess(text.index('needs: authorize'), text.index('actions/setup-go@'))
            self.assertLess(text.index('ensure-tag' if name == 'release-production' else 'recovery'), text.index('actions/setup-go@'))

    def test_taps_checks_identity_and_checksums_before_token(self):
        text = (ROOT / '.github/workflows/release-taps.yml').read_text()
        self.assertIn('workflow_call:', text)
        self.assertIn('sha:', text)
        self.assertLess(text.index('validate-release-input.sh recovery'), text.index('create-github-app-token@'))
        self.assertLess(text.index('validate-release-input.sh checksums'), text.index('create-github-app-token@'))
        self.assertNotIn('get_sha()', text)

    def test_exact_tool_pins_and_env_only_shell_expressions(self):
        uses = 0
        for path in (ROOT / '.github/workflows').glob('*.yml'):
            text = path.read_text()
            for block in re.split(r'\n      - ', text):
                if 'goreleaser/goreleaser-action@' in block:
                    uses += 1
                    self.assertIn('goreleaser-action@a4f02ab5355a589de60d917a659ce617ed19b3de', block)
                    self.assertIn('version: v2.19.0', block)
            if path.stem.startswith('release-'):
                lines = text.splitlines()
                for index, line in enumerate(lines):
                    if re.match(r'\s+run: [|>]', line):
                        indent = len(line) - len(line.lstrip())
                        for shell in lines[index + 1:]:
                            if shell.strip() and len(shell) - len(shell.lstrip()) <= indent:
                                break
                            self.assertNotIn('${{', shell, f'{path}: shell expression injection')
        self.assertEqual(uses, 5)


if __name__ == '__main__':
    unittest.main()
