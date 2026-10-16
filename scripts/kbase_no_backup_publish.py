"""Reviewed server/Web-only no-backup publication for the Health login change.

Run from a root-owned clean source checkout after unprivileged build/tests.
The existing worker protocol and data layout must remain unchanged. All source,
artifact, CI and live revision bindings are checked before stopping the server.
Failure retains the release lease; this entry never retries or rolls back.
"""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import time
import urllib.request

BASE = 'dad9515ad4558b587529a53fdd4d09f3b2aa5374'
PRODUCTION = Path('/opt/dedao-kbase')
LEASE = Path('/var/lock/health-app-release')
STATE = Path('/var/lib/dedao-kbase-release-state')
API = 'https://api.github.com/repos/itsoso/dedao-kbase'
ALLOWED = {'backend/app/health_login.go', 'backend/app/health_login_test.go', 'backend/app/kbase_http.go',
           'cmd/kbase-server/main.go', 'deploy/nginx/browser-session-proxy-smoke.sh',
           'deploy/nginx/kbase.executor.life.conf', 'deploy/nginx/kbase.locations.conf.template',
           'docs/_generated/system-map.json', 'docs/plans/health-browser-login.md', 'frontend-web/app.js',
           'frontend-web/scripts/browser-cookie-session-smoke.mjs', 'frontend-web/scripts/browser-session-settings-smoke.mjs',
           'frontend-web/scripts/kbase-token-header-smoke.mjs', 'scripts/kbase_no_backup_publish.py',
           'scripts/test_kbase_no_backup_publish.py', 'README.md', '.github/workflows/kbase-build-gates.yml'}
ENV = {'PATH': '/usr/bin:/bin', 'HOME': '/root', 'LC_ALL': 'C', 'GIT_CONFIG_NOSYSTEM': '1',
       'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_SYSTEM': '/dev/null', 'GIT_NO_REPLACE_OBJECTS': '1'}


class PublishError(Exception):
    pass


def run(*args, cwd=None):
    try:
        return subprocess.run(args, cwd=cwd, env=ENV, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, check=True, timeout=90, text=True).stdout.strip()
    except (OSError, subprocess.SubprocessError):
        raise PublishError('fixed publication command failed') from None


def secure(path, directory=False):
    for item in [*reversed(path.parents), path]:
        info = item.lstat()
        folder = item != path or directory
        if (info.st_uid != 0 or info.st_mode & 0o022 or
                not (stat.S_ISDIR(info.st_mode) if folder else stat.S_ISREG(info.st_mode)) or
                (not folder and info.st_nlink != 1)):
            raise PublishError('untrusted publication path')
    return info


def digest_file(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def digest_tree(root, require_root=True):
    digest, pending, count = hashlib.sha256(), [root], 0
    while pending:
        path = pending.pop()
        info = path.lstat()
        count += 1
        if (count > 10000 or info.st_dev != root.lstat().st_dev or path.is_mount()
                or (require_root and (info.st_uid != 0 or info.st_mode & 0o022))):
            raise PublishError('unsafe artifact inventory')
        digest.update((str(path.relative_to(root)) + '\0').encode())
        if stat.S_ISDIR(info.st_mode):
            pending.extend(sorted(path.iterdir(), reverse=True))
        elif stat.S_ISREG(info.st_mode) and info.st_nlink == 1:
            digest.update(digest_file(path).encode())
        else:
            raise PublishError('unsupported artifact object')
    return digest.hexdigest()


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        raise PublishError('unexpected remote redirect')


def get(url):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(urllib.request.Request(url, headers={'Accept': 'application/vnd.github+json', 'User-Agent': 'kbase-publisher'}), timeout=20) as response:
        raw = response.read(2_000_001)
        if len(raw) > 2_000_000:
            raise PublishError('remote response exceeds bound')
        return json.loads(raw)


def check_ci(revision, current, runs):
    if revision != current or not runs or any(
            row.get('head_sha') != revision or row.get('head_branch') != 'main' or row.get('event') != 'push'
            or row.get('status') != 'completed' or row.get('conclusion') != 'success'
            or row.get('path') != '.github/workflows/kbase-build-gates.yml'
            or row.get('repository', {}).get('full_name') != 'itsoso/dedao-kbase' for row in runs):
        raise PublishError('exact current-main CI required')


def reject_previous(value):
    if value is not None:
        raise PublishError('original publication already consumed; reviewed recovery required')


def proxy_patch(raw):
    if b'/browser/health/start' in raw or b'kbase_health_login' in raw:
        raise PublishError('proxy authentication change already present')
    return raw + b'''\nlocation = /browser/health/start {
    limit_req zone=kbase_health_login burst=5 nodelay;
    limit_req_status 429;
    proxy_pass http://127.0.0.1:8719;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Authorization "";
    proxy_set_header Proxy-Authorization "";
    proxy_set_header X-KBase-Browser-Session "";
    proxy_set_header Connection "";
}
'''


def sync(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def write(path, raw, mode=0o600):
    secure(path.parent, directory=True)
    temporary = path.with_name('.' + path.name + '.candidate')
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    sync(path.parent)


def execute(args):
    source = Path(__file__).absolute().parent.parent
    secure(source, directory=True)
    for path in source.rglob('*'):
        if '.git' not in path.relative_to(source).parts and 'node_modules' not in path.relative_to(source).parts and 'dist' not in path.relative_to(source).parts:
            secure(path, directory=path.is_dir())
    if run('/usr/bin/git', 'rev-parse', 'HEAD', cwd=source) != args.revision or run('/usr/bin/git', 'status', '--porcelain', '--untracked-files=no', cwd=source):
        raise PublishError('clean exact source required')
    changed = set(run('/usr/bin/git', 'diff', '--name-only', BASE, args.revision, cwd=source).splitlines())
    if not changed <= ALLOWED:
        raise PublishError('server-only compatibility scope exceeded')
    runs = get(API + '/actions/workflows/kbase-build-gates.yml/runs?event=push&head_sha=' + args.revision + '&per_page=100')
    if runs.get('total_count', 101) > 100:
        raise PublishError('CI history exceeds bound')
    check_ci(args.revision, get(API + '/git/ref/heads/main')['object']['sha'], runs['workflow_runs'])
    binary = source.parent / 'kbase-server'
    web = source.parent / 'frontend-web'
    secure(binary)
    if digest_file(binary) != args.binary_sha256 or digest_tree(web) != args.web_sha256:
        raise PublishError('candidate checksum differs')
    if digest_tree(web) != digest_tree(source / 'frontend-web'):
        raise PublishError('Web artifact differs from reviewed source')
    health = get('https://kbase.executor.life/health')
    if health.get('revision') != BASE or health.get('ok') is not True:
        raise PublishError('fixed compatible production baseline required')
    secure(PRODUCTION, directory=True)
    secure(PRODUCTION / 'bin', directory=True)
    if binary.stat().st_dev != (PRODUCTION / 'bin').stat().st_dev or web.stat().st_dev != PRODUCTION.stat().st_dev:
        raise PublishError('same-filesystem candidate required')
    if run('/usr/bin/systemctl', 'show', 'dedao-kbase', '--property=KillMode', '--value') != 'control-group':
        raise PublishError('server process containment differs')
    if run('/usr/bin/systemctl', 'show', 'dedao-kbase', '--property=WorkingDirectory', '--value') != str(PRODUCTION):
        raise PublishError('server working directory differs')
    locations = Path('/etc/dedao-kbase/kbase.locations.conf')
    zone = Path('/etc/nginx/conf.d/kbase-health-login-limit.conf')
    secure(locations)
    original_proxy = locations.read_bytes()
    candidate_proxy = proxy_patch(original_proxy)
    if zone.exists():
        raise PublishError('rate zone already exists')
    server_config = Path('/etc/nginx/conf.d/kbase.executor.life.conf')
    secure(server_config)
    if server_config.read_bytes().count(b'include /etc/dedao-kbase/kbase.locations.conf;') != 1:
        raise PublishError('proxy include differs')
    journal_path = STATE / ('no-backup-' + args.revision + '.json')
    secure(STATE, directory=True)
    reject_previous(json.loads(journal_path.read_bytes()) if os.path.lexists(journal_path) else None)
    if LEASE.exists():
        raise PublishError('another release owns the business lease')
    journal = dict(state='READY', revision=args.revision, previous_revision=BASE, publication_policy='no-backup-v1',
                   binary_sha256=args.binary_sha256, web_sha256=args.web_sha256, ci_run_ids=[r['id'] for r in runs['workflow_runs']],
                   proxy_before=hashlib.sha256(original_proxy).hexdigest())
    if not args.apply:
        return journal
    journal['state'] = 'STARTED'
    write(journal_path, json.dumps(journal).encode())
    LEASE.mkdir(mode=0o700)
    for name, value in {'token': os.urandom(32).hex(), 'label': 'kbase-no-backup', 'stage': str(journal_path), 'started_at': str(int(time.time()))}.items():
        write(LEASE / name, (value + '\n').encode())
    sync(LEASE.parent)
    try:
        run('/usr/bin/systemctl', 'stop', 'dedao-kbase')
        if run('/usr/bin/systemctl', 'show', 'dedao-kbase', '--property=MainPID', '--value') != '0':
            raise PublishError('server still running')
        group = Path('/sys/fs/cgroup/system.slice/dedao-kbase.service')
        if group.exists() and any(p.read_text().strip() for p in group.rglob('cgroup.procs')):
            raise PublishError('server descendants remain')
        live_web = PRODUCTION / 'frontend-web'
        digest_tree(live_web, require_root=False)
        if not shutil.rmtree.avoids_symlink_attacks:
            raise PublishError('safe deletion unavailable')
        shutil.rmtree(live_web)
        os.replace(web, live_web)
        sync(PRODUCTION)
        os.chmod(binary, 0o755)
        os.replace(binary, PRODUCTION / 'bin/kbase-server')
        sync(PRODUCTION / 'bin')
        journal['state'] = 'INSTALLED'
        write(journal_path, json.dumps(journal).encode())
        if digest_file(PRODUCTION / 'bin/kbase-server') != args.binary_sha256 or digest_tree(live_web) != args.web_sha256:
            raise PublishError('installed artifact differs')
        run('/usr/bin/systemctl', 'start', 'dedao-kbase')
        for attempt in range(20):
            try:
                healthy = all(get(url).get('revision') == args.revision for url in ('http://127.0.0.1:8719/health', 'https://kbase.executor.life/health'))
            except (OSError, ValueError):
                healthy = False
            if healthy:
                break
            time.sleep(1)
        else:
            raise PublishError('installed revision not healthy')
        pid = int(run('/usr/bin/systemctl', 'show', 'dedao-kbase', '--property=MainPID', '--value'))
        if pid <= 1 or digest_file(Path(f'/proc/{pid}/exe')) != args.binary_sha256:
            raise PublishError('running executable differs')
        if get('https://kbase.executor.life/browser/health/config') != {'enabled': False}:
            raise PublishError('SSO must remain disabled during code publication')
        if locations.read_bytes() != original_proxy or zone.exists():
            raise PublishError('proxy configuration changed')
        write(zone, b'limit_req_zone $binary_remote_addr zone=kbase_health_login:1m rate=6r/m;\n', 0o644)
        write(locations, candidate_proxy, stat.S_IMODE(locations.stat().st_mode))
        run('/usr/sbin/nginx', '-t')
        run('/usr/bin/systemctl', 'reload', 'nginx')
        journal['state'] = 'SUCCEEDED'
        write(journal_path, json.dumps(journal).encode())
        for name in ('token', 'label', 'stage', 'started_at'):
            (LEASE / name).unlink()
        LEASE.rmdir()
        sync(LEASE.parent)
        return journal
    except BaseException:
        journal['state'] = 'NEEDS_OPERATOR'
        write(journal_path, json.dumps(journal).encode())
        raise


def main():
    parser = argparse.ArgumentParser(allow_abbrev=False)
    parser.add_argument('--revision', required=True)
    parser.add_argument('--binary-sha256', required=True)
    parser.add_argument('--web-sha256', required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    try:
        if not sys.flags.isolated or not sys.flags.no_site or not sys.flags.dont_write_bytecode or os.geteuid() != 0:
            raise PublishError('isolated root operator required')
        if not re.fullmatch('[a-f0-9]{40}', args.revision) or any(not re.fullmatch('[a-f0-9]{64}', value) for value in (args.binary_sha256, args.web_sha256)):
            raise PublishError('exact artifact bindings required')
        os.umask(0o077)
        os.environ.clear()
        os.environ.update(ENV)
        lock = Path('/var/lock/kbase-no-backup.lock')
        fd = os.open(lock, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'r+b') as stream:
            secure(lock)
            fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            print(json.dumps(execute(args)))
        return 0
    except Exception:
        print('KBase publication rejected; protected journal retained when started', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
