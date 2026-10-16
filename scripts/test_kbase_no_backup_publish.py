import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('publisher', Path(__file__).with_name('kbase_no_backup_publish.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class PublisherTests(unittest.TestCase):
    def test_ci_requires_current_exact_successful_main_push(self):
        revision = 'a' * 40
        good = dict(head_sha=revision, head_branch='main', event='push', status='completed', conclusion='success', id=1,
                    path='.github/workflows/kbase-build-gates.yml', repository={'full_name': 'itsoso/dedao-kbase'})
        m.check_ci(revision, revision, [good])
        for field, value in [('head_sha', 'b' * 40), ('event', 'pull_request'), ('conclusion', 'failure'), ('status', 'queued')]:
            with self.assertRaises(m.PublishError):
                m.check_ci(revision, revision, [{**good, field: value}])
        with self.assertRaises(m.PublishError):
            m.check_ci(revision, 'b' * 40, [good])
        with self.assertRaises(m.PublishError):
            m.check_ci(revision, revision, [])

    def test_web_inventory_rejects_links(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'index.html').write_text('test')
            first = m.digest_tree(root, require_root=False)
            (root / 'index.html').write_text('changed')
            self.assertNotEqual(first, m.digest_tree(root, require_root=False))
            (root / 'link').symlink_to('/etc/passwd')
            with self.assertRaises(m.PublishError):
                m.digest_tree(root, require_root=False)

    def test_unknown_prior_operation_never_retried(self):
        for state in ('STARTED', 'NEEDS_OPERATOR', 'SUCCEEDED'):
            with self.assertRaises(m.PublishError):
                m.reject_previous({'state': state})

    def test_proxy_patch_preserves_existing_bytes_and_rejects_repeat(self):
        original = b'location / { proxy_set_header X-KBase-Browser-Session "existing"; }\n'
        updated = m.proxy_patch(original)
        self.assertTrue(updated.startswith(original))
        self.assertIn(b'limit_req zone=kbase_health_login burst=5 nodelay;', updated)
        self.assertIn(b'proxy_set_header Authorization "";', updated[len(original):])
        with self.assertRaises(m.PublishError):
            m.proxy_patch(updated)


if __name__ == '__main__':
    unittest.main()
