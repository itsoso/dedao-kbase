import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace
import json

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


class TransactionTests(unittest.TestCase):
    def scenario(self, fault):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / 'candidate/source'
            (source / 'scripts').mkdir(parents=True)
            (source / '.git').mkdir()
            (source / 'frontend-web').mkdir()
            (source / 'frontend-web/index.html').write_text('new')
            (source / 'frontend-web/app.js').write_text('new app')
            web = source.parent / 'frontend-web'
            web.mkdir()
            (web / 'index.html').write_text('new')
            (web / 'app.js').write_text('new app')
            binary = source.parent / 'kbase-server'
            binary.write_bytes(b'new-binary')
            production = root / 'live'
            (production / 'bin').mkdir(parents=True)
            (production / 'bin/kbase-server').write_bytes(b'old-binary')
            (production / 'frontend-web').mkdir()
            (production / 'frontend-web/index.html').write_text('old')
            (production / 'book_knowledge').mkdir()
            (production / 'book_knowledge/preserve').write_bytes(b'private-data')
            state = root / 'state'
            state.mkdir()
            proxy = root / 'locations'
            proxy.write_bytes(b'location / { proxy_pass http://127.0.0.1:8719; }')
            server = root / 'server'
            server.write_bytes(b'include /etc/dedao-kbase/kbase.locations.conf;')
            zone, lease = root / 'zone', root / 'lease'
            revision = 'a' * 40
            args = SimpleNamespace(revision=revision, binary_sha256=m.digest_file(binary),
                                   web_sha256=m.digest_tree(web, require_root=False), apply=True)
            commands, running, deletes = [], {'new': False}, []
            good = dict(head_sha=revision, head_branch='main', event='push', status='completed', conclusion='success', id=1,
                        path='.github/workflows/kbase-build-gates.yml', repository={'full_name': 'itsoso/dedao-kbase'})
            def get(url):
                if '/git/ref/' in url:
                    return {'object': {'sha': revision}}
                if '/actions/' in url:
                    return {'total_count': 1, 'workflow_runs': [good]}
                if url.endswith('/config'):
                    return {'enabled': False}
                return {'ok': True, 'revision': revision if running['new'] else m.BASE}
            def run(*command, **kwargs):
                commands.append(command)
                if command[0] == '/usr/bin/git':
                    if 'rev-parse' in command:
                        return revision
                    return ''
                if '--build-info' in command:
                    return json.dumps({'revision': 'bad' if fault == 'revision' else revision,
                                       'vcs.revision': revision, 'vcs.modified': 'false'})
                if '--property=WorkingDirectory' in command:
                    return str(production)
                if '--property=KillMode' in command:
                    return 'control-group'
                if '--property=MainPID' in command:
                    return '123' if running['new'] else '0'
                if 'stop' in command and fault == 'stop':
                    raise m.PublishError('injected stop failure')
                if 'start' in command:
                    if fault == 'start':
                        raise m.PublishError('injected start failure')
                    running['new'] = True
                if command[0] == '/usr/sbin/nginx' and fault == 'nginx':
                    raise m.PublishError('injected nginx failure')
                if 'reload' in command and fault == 'reload':
                    raise m.PublishError('injected reload failure')
                return ''
            real_remove, real_replace, real_digest = m.shutil.rmtree, m.os.replace, m.digest_file
            def remove(path, **kwargs):
                deletes.append(path)
                real_remove(path, **kwargs)
                if fault == 'remove':
                    raise m.PublishError('injected after removal')
            def replace(old, new):
                real_replace(old, new)
                if old == binary and fault == 'replace':
                    raise m.PublishError('injected after binary replacement')
            def digest(path):
                return args.binary_sha256 if str(path).startswith('/proc/') else real_digest(path)
            with patch.multiple(m, __file__=str(source / 'scripts/kbase_no_backup_publish.py'),
                                PRODUCTION=production, STATE=state, LEASE=lease, PROXY_LOCATIONS=proxy,
                                PROXY_ZONE=zone, PROXY_SERVER=server, CGROUP=root / 'cgroup'), \
                 patch.object(m, 'reject_mounts'), \
                 patch.object(m, 'secure', side_effect=lambda path, directory=False: path.lstat()), \
                 patch.object(m, 'run', side_effect=run), patch.object(m, 'get', side_effect=get), \
                 patch.object(m, 'fetch', side_effect=lambda url: b'wrong' if fault == 'web' else (production / 'frontend-web' / url.rsplit('/', 1)[1]).read_bytes()), \
                 patch.object(m, 'digest_file', side_effect=digest), \
                 patch.object(m.shutil, 'rmtree', side_effect=remove) as remover, \
                 patch.object(m.os, 'replace', side_effect=replace):
                remover.avoids_symlink_attacks = True
                real_tree = m.digest_tree
                with patch.object(m, 'digest_tree', side_effect=lambda path, **kwargs: real_tree(path, require_root=False)):
                    if fault:
                        with self.assertRaises(m.PublishError):
                            m.execute(args)
                        if fault == 'revision':
                            self.assertFalse(lease.exists())
                            self.assertFalse(list(state.iterdir()))
                        else:
                            self.assertTrue(lease.exists())
                            journal = json.loads(next(state.iterdir()).read_text())
                            self.assertEqual(journal['state'], 'NEEDS_OPERATOR')
                        if fault in ('revision', 'stop'):
                            self.assertEqual(deletes, [])
                            self.assertEqual((production / 'bin/kbase-server').read_bytes(), b'old-binary')
                    else:
                        self.assertEqual(m.execute(args)['state'], 'SUCCEEDED')
                        self.assertFalse(lease.exists())
                    self.assertEqual((production / 'book_knowledge/preserve').read_bytes(), b'private-data')
                    self.assertFalse(any('worker' in str(command) for command in commands))
                    self.assertFalse(any(item.name.startswith(('backup-', 'previous-')) for item in root.rglob('*')))

    def test_successful_transaction(self):
        self.scenario(None)

    def test_failures_keep_original_intent_and_lease(self):
        for fault in ('revision', 'stop', 'remove', 'replace', 'start', 'nginx', 'reload', 'web'):
            with self.subTest(fault=fault):
                self.scenario(fault)


if __name__ == '__main__':
    unittest.main()
