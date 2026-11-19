"""Exercise the embedded installer with eligible repository-local host fixtures."""
import hashlib
import io
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tarfile


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


wrong_objects = {'null': None, 'array': [], 'string': 'invalid', 'number': 17,
                 'bool': True}


installer, directory, node_source, scenario = sys.argv[1:]
root = pathlib.Path(directory)
config = root / 'config'
config.mkdir()
maintenance = root / 'maintenance'
maintenance.mkdir()
tools = root / 'tools'
version = subprocess.check_output([node_source, '--version']).decode().strip()[1:]
arch = 'x64' if os.uname().machine == 'x86_64' else 'arm64'
target = tools / ('node-v' + version + '-linux-' + arch) / 'bin'
target.mkdir(parents=True)
node = target / 'node'
shutil.copyfile(node_source, node)
node.chmod(0o755)
node_hash = hashlib.sha256(node.read_bytes()).hexdigest()
capability = {'schemaVersion': 1, 'kind': 'core-native-maintenance-v1'}
platform = {'nodeVersion': version, 'targets': {'native': {
    'observations': [os.uname().machine], 'artifactArch': arch,
    'nodeSha256': node_hash, 'elfMachine': 62 if arch == 'x64' else 183}}}


def bootstrap_package(commit):
    core = {'repository': 'taco3064/nibfin-core', 'commit': commit * 40}
    receipt = {'schemaVersion': 1, 'status': 'installed',
               'capability': capability['kind'], 'source': core,
               'recoveryRetained': [str(maintenance / 'producer-owned-retained')]}
    if scenario == 'node-pending' and commit == 'c':
        receipt.update(status='pending', code='BOOTSTRAP_PENDING',
                       githubApi={'kind': 'http', 'httpStatus': 422})
    entry = """import fs from 'node:fs';
import {spawnSync} from 'node:child_process';
const held = fs.fstatSync(3);
const config = fs.statSync(CONFIG);
if (held.dev !== config.dev || held.ino !== config.ino) process.exit(11);
const inherited = spawnSync('/usr/bin/python3', ['-I', '-c',
  'import fcntl;fcntl.flock(3,fcntl.LOCK_EX|fcntl.LOCK_NB)'],
  {stdio:['ignore','pipe','pipe',3]});
if (inherited.status !== 0) process.exit(12);
fs.appendFileSync(EXECUTED, 'executed\\n');
const receipt = RECEIPT;
receipt.source = JSON.parse(fs.readFileSync(PIN));
console.log(JSON.stringify(receipt));
""".replace('CONFIG', json.dumps(str(config)))
    entry = entry.replace('EXECUTED', json.dumps(str(root / ('executed-' + commit))))
    entry = entry.replace('RECEIPT', json.dumps(receipt))
    pin = json.dumps(str(maintenance / 'bootstrap-source-pin.json'))
    entry = entry.replace('PIN', pin)
    if scenario == 'node-pending' and commit == 'c':
        entry += 'process.exit(2);\n'
    if scenario == 'node-malformed' and commit == 'c':
        entry = entry.replace('console.log(JSON.stringify(receipt));',
                              "console.log('not a receipt');process.exit(1);")
    files = {'.node-version': (version + '\n').encode(),
             'src/shared-platform.json': canonical(platform),
             'src/core-maintenance-installation.json': canonical(capability),
             'scripts/core-maintenance-bootstrap.mjs': entry.encode()}
    if commit == 'c' and scenario.startswith('invalid-'):
        boundary, kind = scenario.removeprefix('invalid-').rsplit('-', 1)
        wrong = wrong_objects[kind]
        if boundary == 'capability':
            files['src/core-maintenance-installation.json'] = canonical(wrong)
        elif boundary == 'platform':
            files['src/shared-platform.json'] = canonical(wrong)
        elif boundary == 'targets':
            files['src/shared-platform.json'] = canonical({**platform, 'targets': wrong})
    manifest = {'schemaVersion': 1, **core, 'files': [
        {'path': name, 'mode': '100644', 'size': len(data),
         'sha256': hashlib.sha256(data).hexdigest()} for name, data in files.items()]}
    files['nibfin-manifest.json'] = canonical(manifest)
    core['manifestHash'] = hashlib.sha256(canonical(manifest)).hexdigest()
    archive = io.BytesIO()
    with tarfile.open(fileobj=archive, mode='w:gz') as package:
        for name, data in files.items():
            item = tarfile.TarInfo(name)
            item.mode, item.size = 0o644, len(data)
            package.addfile(item, io.BytesIO(data))
    archive = archive.getvalue()
    core['archiveHash'] = hashlib.sha256(archive).hexdigest()
    return core, archive


namespace = {'__name__': 'controlled_installer', 'TOOLS': tools}
source = pathlib.Path(installer).read_text().replace(
    "pathlib.Path('/opt/nibfin/tools')", 'TOOLS')
source = source.replace("pathlib.Path('/usr/local/libexec')",
                        "MAINTENANCE.parent / 'libexec'")
exec(compile(source, installer, 'exec'), namespace)
namespace.update(MAINTENANCE=maintenance, BOOTSTRAP=maintenance / 'bootstrap',
                 BOOTSTRAP_PIN=maintenance / 'bootstrap-source-pin.json', CONFIG=config)
core, archive = bootstrap_package('b')
namespace['publish_bootstrap'](archive, core)
bootstrap, pin_path = namespace['BOOTSTRAP'], namespace['BOOTSTRAP_PIN']
original = (bootstrap / 'nibfin-manifest.json').read_bytes()
request, requested_archive = core, b''
if scenario == 'newer-reuse':
    request, requested_archive = bootstrap_package('a')
elif scenario in ('lost-pin', 'missing-probe', 'different-target', 'foreign-archive',
                  'bad-archive', 'node-pending', 'node-malformed') or scenario.startswith(
                      'checkpoint-'):
    pin_path.unlink()
    if scenario != 'missing-probe':
        request, requested_archive = bootstrap_package(
            'b' if scenario == 'lost-pin' else 'c')
    else:
        request = {}
elif scenario == 'foreign-pin':
    pin_path.write_text(json.dumps({**core, 'commit': 'c' * 40}))
    requested_archive = archive
elif scenario == 'tampered-source':
    (bootstrap / '.node-version').write_text('0.0.0\n')
elif scenario.startswith('record-'):
    kind = scenario.removeprefix('record-').removesuffix('-probe')
    pin_path.write_text(json.dumps(wrong_objects[kind]))
    if not scenario.endswith('-probe'):
        request, requested_archive = bootstrap_package('c')
elif scenario.startswith('manifest-'):
    kind = scenario.removeprefix('manifest-').removesuffix('-probe')
    (bootstrap / 'nibfin-manifest.json').write_bytes(canonical(wrong_objects[kind]))
    if not scenario.endswith('-probe'):
        request, requested_archive = bootstrap_package('c')
elif scenario.startswith('invalid-'):
    pin_path.unlink()
    request, requested_archive = bootstrap_package('c')
    if scenario.startswith('invalid-request-'):
        request = wrong_objects[scenario.rsplit('-', 1)[1]]

original = (bootstrap / 'nibfin-manifest.json').read_bytes()
(root / 'original-manifest.json').write_bytes(original)

if scenario in ('foreign-archive', 'bad-archive') or scenario.startswith('invalid-'):
    if scenario == 'foreign-archive':
        request = {**request, 'repository': 'foreign/core'}
    elif scenario == 'bad-archive':
        requested_archive += b'changed'
    try:
        namespace['bootstrap_maintenance'](requested_archive, json.dumps(request))
    except ValueError:
        assert (bootstrap / 'nibfin-manifest.json').read_bytes() == original
        assert not (root / 'executed-b').exists()
        assert not list(maintenance.glob('.bootstrap-*'))
        print(json.dumps({'status': 'rejected'}))
        raise SystemExit(0)
    raise AssertionError('Foreign or corrupt archive was accepted')

if scenario.startswith('checkpoint-'):
    checkpoint = scenario.removeprefix('checkpoint-').removesuffix('-next-target')
    rename, replace = namespace['os'].rename, namespace['os'].replace
    publish_pin = namespace['publish_bootstrap_pin']
    sync_directory = namespace['sync_bootstrap_directory']

    def interrupt():
        raise RuntimeError('controlled publication interruption')

    def interrupted_rename(source, destination):
        if checkpoint == 'before-tree' and destination == bootstrap:
            interrupt()
        rename(source, destination)
        if checkpoint == 'retained' and destination.name == 'retained-bootstrap':
            interrupt()
        if checkpoint == 'tree' and destination == bootstrap:
            interrupt()

    def interrupted_replace(source, destination):
        replace(source, destination)
        if checkpoint == 'pin-replaced' and destination == pin_path:
            interrupt()

    def interrupted_pin(pin):
        publish_pin(pin)
        if checkpoint == 'pin-synced':
            interrupt()

    def interrupted_sync(path):
        sync_directory(path)
        if checkpoint == 'tree-synced' and path == maintenance and bootstrap.exists():
            interrupt()

    namespace['os'].rename = interrupted_rename
    namespace['os'].replace = interrupted_replace
    namespace['publish_bootstrap_pin'] = interrupted_pin
    namespace['sync_bootstrap_directory'] = interrupted_sync
    try:
        namespace['bootstrap_maintenance'](requested_archive, json.dumps(request))
    except RuntimeError:
        pass
    else:
        raise AssertionError('Publication checkpoint was not reached')
    finally:
        namespace['os'].rename, namespace['os'].replace = rename, replace
        namespace['publish_bootstrap_pin'] = publish_pin
        namespace['sync_bootstrap_directory'] = sync_directory
    retained = list(maintenance.glob('.bootstrap-*/retained-bootstrap'))
    assert len(retained) == 1
    assert (retained[0] / 'nibfin-manifest.json').read_bytes() == original
    assert not (root / 'executed-b').exists()
    if bootstrap.exists():
        namespace['verify_bootstrap'](bootstrap, json.loads(pin_path.read_text()))
    if scenario.endswith('-next-target'):
        request, requested_archive = bootstrap_package('d')

namespace['bootstrap_maintenance'](requested_archive, json.dumps(request))
