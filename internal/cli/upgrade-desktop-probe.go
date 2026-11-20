package cli

// Unsupported installed CLI and unavailable installed inspection are different
// observations. The supervisor reports only fixed codes, never raw diagnostics.
const upgradeProbeScript = `import json, subprocess, sys
try:
    p = subprocess.run(sys.argv[1:], capture_output=True)
    if p.returncode == 0:
        sys.stdout.buffer.write(p.stdout)
    elif p.stderr.strip() in (b'UNKNOWN_COMMAND_USE_HELP',
                             b'INSTALLED_CORE_LACKS_SHARED_HOST_INSPECTION'):
        print(json.dumps({"gap": "INSTALLED_CLI_UPGRADE_TRANSPORT_UNSUPPORTED"}))
    else:
        print(json.dumps({"gap": "GUEST_UPGRADE_OBSERVATION_UNAVAILABLE"}))
except OSError:
    print(json.dumps({"gap": "INSTALLED_GUEST_CLI_UNAVAILABLE"}))
`
