"""Secretless workflow policy gate; parse jobs to check trust/privilege ordering."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]


class ReleasePolicy(unittest.TestCase):
    def prepare_candidate_job(self):
        text = (ROOT / '.github/workflows/release-production.yml').read_text()
        return text.split('\n  prepare-candidate:\n', 1)[1].split('\n  release:\n', 1)[0]

    def production_release_job(self):
        text = (ROOT / '.github/workflows/release-production.yml').read_text()
        return text.split('\n  release:\n', 1)[1].split('\n  verify-macos:\n', 1)[0]

    def test_historical_identity_retired_and_no_automatic_privilege_trigger(self):
        self.assertFalse((ROOT / '.github/workflows/release.yml').exists())
        self.assertFalse((ROOT / '.github/workflows/release-app-auth-check.yml').exists())
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
        production = self.production_release_job()
        self.assertIn('ref: ${{ needs.authorize.outputs.sha }}', production)
        self.assertIn('ref: ${{ needs.authorize.outputs.control_sha }}', production)
        self.assertIn('needs: [authorize, prepare-candidate]', production)
        self.assertLess(production.index('ensure-tag'), production.index('actions/setup-go@'))

        aur = (ROOT / '.github/workflows/release-aur.yml').read_text()
        self.assertIn('ref: ${{ needs.authorize.outputs.sha }}', aur)
        self.assertIn('ref: ${{ needs.authorize.outputs.control_sha }}', aur)
        self.assertLess(aur.index('needs: authorize'), aur.index('actions/setup-go@'))
        self.assertLess(aur.index('recovery'), aur.index('actions/setup-go@'))

    def test_taps_checks_identity_and_checksums_before_token(self):
        text = (ROOT / '.github/workflows/release-taps.yml').read_text()
        self.assertIn('workflow_call:', text)
        self.assertIn('sha:', text)
        self.assertLess(text.index('validate-release-input.sh recovery'), text.index('create-github-app-token@'))
        self.assertLess(text.index('validate-release-input.sh checksums'), text.index('create-github-app-token@'))
        self.assertNotIn('get_sha()', text)

    def test_production_uses_environment_gated_repository_scoped_release_app(self):
        text = self.production_release_job()
        job_header = text.split('\n    steps:\n', 1)[0]
        self.assertIn('environment: release-production', text)
        self.assertIn('needs: [authorize, prepare-candidate]', job_header)
        self.assertIn('contents: read', job_header)
        self.assertNotIn('contents: write', job_header)
        self.assertIn('id-token: write', job_header)
        self.assertIn('attestations: write', job_header)
        self.assertNotIn('${{ github.token }}', text)
        self.assertNotIn('${{ secrets.GITHUB_TOKEN }}', text)
        self.assertEqual(text.count('persist-credentials: false'), 2)

        action = 'actions/create-github-app-token@64ab729a6294000f5aa4a4bc0b73d1f32bc45510'
        self.assertIn(action, text)
        self.assertEqual(text.count(action), 2)
        self.assertIn('id: release-tag-token', text)
        self.assertIn('id: release-publish-token', text)
        self.assertEqual(text.count('client-id: ${{ vars.RELEASE_APP_CLIENT_ID }}'), 2)
        self.assertEqual(text.count('private-key: ${{ secrets.RELEASE_APP_PRIVATE_KEY }}'), 2)
        self.assertEqual(text.count('owner: lewta'), 2)
        self.assertEqual(text.count('repositories: sendit'), 2)
        self.assertEqual(text.count('permission-contents: write'), 2)
        self.assertEqual(text.count('permission-workflows: write'), 2)
        self.assertEqual(text.count('skip-token-revoke: false'), 2)
        self.assertNotIn('permission-administration:', text)
        self.assertNotIn('permission-actions:', text)
        self.assertLess(text.index('environment: release-production'), text.index(action))
        self.assertLess(text.index('ref: ${{ needs.authorize.outputs.control_sha }}'), text.index(action))
        self.assertLess(text.index('Revalidate current source before publication credentials'), text.index('id: release-tag-token'))
        self.assertIn('validate-release-input.sh validate', text)
        self.assertLess(text.index('id: release-tag-token'), text.index('validate-release-input.sh ensure-tag'))
        self.assertLess(text.index('id: release-publish-token'), text.rindex('validate-release-input.sh ensure-tag'))
        self.assertIn('args: release --config ../control/.goreleaser.yaml --clean --skip=before,homebrew,scoop', text)
        self.assertNotIn('go run ./cmd/sendit', text)
        self.assertIn('actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333', text)
        self.assertIn('name: release-completions-${{ needs.authorize.outputs.sha }}', text)

        for name in ('release-aur', 'release-taps'):
            other = (ROOT / f'.github/workflows/{name}.yml').read_text()
            self.assertNotIn('RELEASE_APP_CLIENT_ID', other)
            self.assertNotIn('RELEASE_APP_PRIVATE_KEY', other)

    def test_candidate_code_runs_only_in_credential_free_preparation_job(self):
        text = self.prepare_candidate_job()
        job_header = text.split('\n    steps:\n', 1)[0]
        self.assertIn('needs: authorize', job_header)
        self.assertIn('permissions:\n      contents: read', job_header)
        self.assertNotIn('environment:', job_header)
        self.assertNotIn('RELEASE_APP_CLIENT_ID', text)
        self.assertNotIn('RELEASE_APP_PRIVATE_KEY', text)
        self.assertNotIn('${{ secrets.', text)
        self.assertIn('Generate completions without publication credentials', text)
        self.assertIn('go run ./cmd/sendit completion bash', text)
        self.assertIn('go run ./cmd/sendit completion zsh', text)
        self.assertIn('go run ./cmd/sendit completion fish', text)
        self.assertIn('args: check --config ../control/.goreleaser.yaml', text)
        self.assertIn('actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9', text)
        self.assertIn('name: release-completions-${{ needs.authorize.outputs.sha }}', text)
        self.assertIn('if-no-files-found: error', text)
        self.assertIn('retention-days: 1', text)

    def test_production_publication_sinks_use_only_release_app_token(self):
        text = self.production_release_job()
        tag_token = '${{ steps.release-tag-token.outputs.token }}'
        publish_token = '${{ steps.release-publish-token.outputs.token }}'
        self.assertEqual(text.count(tag_token), 1)
        self.assertEqual(text.count(publish_token), 2)
        self.assertEqual(text.count(f'GH_TOKEN: {tag_token}'), 1)
        self.assertEqual(text.count(f'GH_TOKEN: {publish_token}'), 1)
        self.assertEqual(text.count(f'GITHUB_TOKEN: {publish_token}'), 1)
        self.assertLess(text.index('id: release-tag-token'), text.index(f'GH_TOKEN: {tag_token}'))
        self.assertLess(text.index('id: release-publish-token'), text.index(f'GH_TOKEN: {publish_token}'))
        self.assertLess(text.index(f'GH_TOKEN: {publish_token}'), text.index(f'GITHUB_TOKEN: {publish_token}'))

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
