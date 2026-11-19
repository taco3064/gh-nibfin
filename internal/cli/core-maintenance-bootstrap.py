"""Extend the normal archive installer at its fixed maintenance boundary."""
import json
import subprocess

MAINTENANCE = pathlib.Path('/usr/local/libexec/nibfin/core-rollout')
BOOTSTRAP = MAINTENANCE / 'bootstrap'
BOOTSTRAP_PIN = MAINTENANCE / 'bootstrap-source-pin.json'
CONFIG = pathlib.Path('/etc/nibfin')
CAPABILITY = 'core-native-maintenance-v1'


def owned_regular(path):
    trusted(path.parent)
    info = path.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1
            or info.st_mode & 0o022):
        raise ValueError('Untrusted maintained source')


def maintained_object(value):
    if not isinstance(value, dict):
        raise ValueError('Invalid maintained JSON object')
    return value


def verify_bootstrap(root, pin):
    pin = maintained_object(pin)
    trusted(root)
    manifest_path = root / 'nibfin-manifest.json'
    owned_regular(manifest_path)
    manifest = maintained_object(json.loads(manifest_path.read_text()))
    canonical = json.dumps(manifest, sort_keys=True, separators=(',', ':')).encode()
    if (pin.get('repository') != 'taco3064/nibfin-core'
            or manifest.get('repository') != pin['repository']
            or manifest.get('commit') != pin.get('commit')
            or manifest.get('schemaVersion') != 1
            or hashlib.sha256(canonical).hexdigest() != pin.get('manifestHash')):
        raise ValueError('Maintained source identity differs')
    expected = {'nibfin-manifest.json'}
    for item in manifest['files']:
        name = item['path']
        if (name in expected or not re.fullmatch(r'[A-Za-z0-9._/-]+', name)
                or any(part in ('', '.', '..', '.git') for part in name.split('/'))):
            raise ValueError('Invalid maintained manifest')
        expected.add(name)
        path = root / name
        owned_regular(path)
        info = path.stat()
        mode = '100755' if info.st_mode & 0o111 else '100644'
        if (item['mode'] not in ('100644', '100755')
                or mode != item['mode']
                or info.st_size != item['size']
                or hashlib.sha256(path.read_bytes()).hexdigest() != item['sha256']):
            raise ValueError('Maintained file differs')
    actual = set()
    for directory, names, files in os.walk(root, followlinks=False):
        trusted(pathlib.Path(directory))
        for name in names:
            trusted(pathlib.Path(directory) / name)
        actual.update(str((pathlib.Path(directory) / name).relative_to(root))
                      for name in files)
    if actual != expected:
        raise ValueError('Unexpected maintained source')
    capability = maintained_object(json.loads(
        (root / 'src/core-maintenance-installation.json').read_text()))
    if capability.get('schemaVersion') != 1 or capability.get('kind') != CAPABILITY:
        raise ValueError('Unsupported maintenance capability')
    version = (root / '.node-version').read_text().strip()
    platform = maintained_object(json.loads(
        (root / 'src/shared-platform.json').read_text()))
    if (platform.get('nodeVersion') != version
            or not re.fullmatch(r'\d+\.\d+\.\d+', version)):
        raise ValueError('Invalid maintained runtime declaration')
    targets = [item for item in maintained_object(platform['targets']).values()
               if os.uname().machine in item['observations']]
    if len(targets) != 1 or targets[0]['artifactArch'] not in ('x64', 'arm64'):
        raise ValueError('Unsupported maintained runtime')
    target = targets[0]
    node = pathlib.Path('/opt/nibfin/tools') / (
        'node-v' + version + '-linux-' + target['artifactArch']) / 'bin/node'
    trusted(node.parent)
    verify_node(node, target['nodeSha256'], target['elfMachine'])
    return node


def publish_bootstrap(archive, pin):
    pin = maintained_object(pin)
    for path in (pathlib.Path('/usr/local/libexec'), MAINTENANCE.parent, MAINTENANCE):
        trusted(path.parent)
        path.mkdir(mode=0o755, exist_ok=True)
        trusted(path)
    if not archive or hashlib.sha256(archive).hexdigest() != pin.get('archiveHash'):
        raise ValueError('Verified Core bootstrap archive required')
    stage = pathlib.Path(tempfile.mkdtemp(prefix='.bootstrap-', dir=MAINTENANCE))
    candidate = stage / 'candidate'
    retained = stage / 'retained-bootstrap'
    try:
        candidate.mkdir(mode=0o700)
        extract_core(archive, candidate)
        verify_bootstrap(candidate, pin)
        if os.path.lexists(BOOTSTRAP):
            os.rename(BOOTSTRAP, retained)
            sync_bootstrap_directory(stage)
            sync_bootstrap_directory(MAINTENANCE)
        publish_bootstrap_pin(pin)
        os.rename(candidate, BOOTSTRAP)
        sync_bootstrap_directory(MAINTENANCE)
    finally:
        if os.path.lexists(retained):
            if candidate.exists():
                shutil.rmtree(candidate)
        elif stage.exists():
            shutil.rmtree(stage)


def sync_bootstrap_directory(path):
    directory = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def publish_bootstrap_pin(pin):
    if os.path.lexists(BOOTSTRAP_PIN):
        owned_regular(BOOTSTRAP_PIN)
    descriptor, temporary = tempfile.mkstemp(prefix='.bootstrap-pin-', dir=MAINTENANCE)
    try:
        with os.fdopen(descriptor, 'w') as output:
            json.dump(pin, output, separators=(',', ':'))
            output.flush()
            os.fsync(output.fileno())
        os.chmod(temporary, 0o644)
        os.replace(temporary, BOOTSTRAP_PIN)
        sync_bootstrap_directory(MAINTENANCE)
    finally:
        if os.path.lexists(temporary):
            os.unlink(temporary)


def retained_bootstrap_paths():
    retained = []
    try:
        trusted(MAINTENANCE)
        for stage in MAINTENANCE.glob('.bootstrap-*'):
            try:
                info = stage.lstat()
                if (not stat.S_ISDIR(info.st_mode) or info.st_uid != 0
                        or info.st_mode & 0o077):
                    continue
                path = stage / 'retained-bootstrap'
                info = path.lstat()
                if not stat.S_ISLNK(info.st_mode):
                    retained.append(str(path))
            except OSError:
                continue
    except (OSError, ValueError):
        pass
    return retained


def bootstrap_maintenance(archive, pin_json):
    trusted(CONFIG)
    handle = os.open(CONFIG, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    if handle != 3:
        os.dup2(handle, 3)
        os.close(handle)
        handle = 3
    try:
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if os.path.lexists(BOOTSTRAP):
            pin = None
            if os.path.lexists(BOOTSTRAP_PIN):
                owned_regular(BOOTSTRAP_PIN)
                try:
                    observed = json.loads(BOOTSTRAP_PIN.read_text())
                    verify_bootstrap(BOOTSTRAP, observed)
                    pin = observed
                except (ValueError, KeyError, TypeError):
                    pass
            if pin is None:
                # Unknown old bytes do not establish their own source identity.
                # A separately verified requested archive supplies the normal
                # replacement, retaining those bytes without executing them.
                if not archive:
                    print(json.dumps({'status': 'missing'}))
                    return
                pin = json.loads(pin_json)
                publish_bootstrap(archive, pin)
        elif archive:
            pin = json.loads(pin_json)
            publish_bootstrap(archive, pin)
        else:
            print(json.dumps({'status': 'missing'}))
            return
        node = verify_bootstrap(BOOTSTRAP, pin)
        entry = BOOTSTRAP / 'scripts/core-maintenance-bootstrap.mjs'
        environment = {'PATH': '/usr/bin:/bin', 'LANG': 'C',
                       'NIBFIN_CORE_GATE_FD': str(handle)}
        result = subprocess.run([str(node), str(entry)], env=environment,
                                pass_fds=(handle,), stdout=subprocess.PIPE)
        output = result.stdout
        retained = retained_bootstrap_paths()
        if retained:
            try:
                receipt = json.loads(output)
                previous = receipt.get('recoveryRetained', [])
                if isinstance(previous, list) and all(
                        isinstance(path, str) for path in previous):
                    merged = list(dict.fromkeys([*previous, *retained]))
                    receipt['recoveryRetained'] = merged
                    output = json.dumps(receipt, separators=(',', ':')).encode() + b'\n'
            except (ValueError, TypeError, AttributeError):
                pass
        sys.stdout.buffer.write(output)
        sys.stdout.buffer.flush()
        raise SystemExit(result.returncode)
    finally:
        os.close(handle)
