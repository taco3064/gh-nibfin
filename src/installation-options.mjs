import { currentPin, currentProducer } from './current-core.mjs';
export function installationOptions(args, { upgrade = false } = {}) {
  const v = {};
  for (let i = 0; i < args.length; i++) {
    const key = args[i];
    if (Object.hasOwn(v, key)) throw Error('Duplicate installation option.');
    if (['--check', '--auto-register'].includes(key)) v[key] = true;
    else if (
      [
        '--repo',
        '--execution-repo',
        '--pool-repos',
        '--runner-count',
        '--image',
        '--runtime-profile',
        '--handover',
        ...(upgrade ? ['--runner', '--version'] : []),
      ].includes(key) &&
      args[i + 1] &&
      !args[i + 1].startsWith('--')
    )
      v[key] = args[++i];
    else
      throw Error(
        'Use init --repo OWNER/REPO [--check] [--auto-register] ' +
          '[--pool-repos OWNER/ONE,OWNER/TWO --runner-count N] ' +
          '[--handover ARCHIVE].',
      );
  }
  if (!v['--repo'] || v['--runner'])
    throw Error(
      'Select --repo OWNER/REPO; local Runner upgrade requires ' +
        'its separately reconciled registration.',
    );
  if (upgrade && v['--auto-register'])
    throw Error('Automatic registration policy changes use Owner init.');
  if (upgrade && v['--handover'])
    throw Error('Original work is imported through Owner init.');
  if (v['--execution-repo'])
    throw Error(
      'Central execution repositories are no longer supported; ' +
        'use --pool-repos with --runner-count. Existing installations ' +
        'require preserved-work reconciliation.',
    );
  if (upgrade && (v['--pool-repos'] || v['--runner-count']))
    throw Error(
      'Pool membership transitions require reconciled installation; ' +
        'upgrade cannot replace them implicitly.',
    );
  if (v['--runner-count'] && !v['--pool-repos'])
    throw Error('Supply --pool-repos with --runner-count.');
  let pool;
  if (v['--pool-repos']) {
    const names = v['--pool-repos'].split(',');
    const count = v['--runner-count'] ?? '2';
    if (!/^[1-9][0-9]*$/.test(count) || Number(count) > 128)
      throw Error('Runner capacity must be an integer from 1 to 128.');
    if (
      names.length > 128 ||
      new Set(names).size !== names.length ||
      !names.includes(v['--repo']) ||
      names.some((n) => !/^[a-z0-9-]+\/[a-z0-9_.-]+$/.test(n))
    )
      throw Error(
        'Select a finite unique canonical pool including the initialized repository.',
      );
    pool = { id: 'member-default', capacity: Number(count), targets: names };
  }
  if (v['--image'] && !/^sha256:[a-f0-9]{64}$/.test(v['--image']))
    throw Error('Select a pinned --image sha256 digest.');
  if (v['--runtime-profile'] && !v['--image'])
    throw Error('Select --image with its reviewed --runtime-profile.');
  if ((v['--image'] || v['--runtime-profile']) && (upgrade || !pool))
    throw Error('Native member runtime selection belongs to pooled init.');
  if (
    v['--version'] &&
    ![currentPin.packageVersion, currentProducer].includes(v['--version'])
  )
    throw Error('Version is not in the reviewed immutable compatibility catalog.');
  return {
    repo: v['--repo'],
    check: !!v['--check'],
    automatic: v['--auto-register'] === true ? true : undefined,
    version: v['--version'],
    ...(pool ? { pool } : {}),
    ...(v['--handover'] ? { handover: v['--handover'] } : {}),
    ...(v['--image']
      ? {
          memberRuntime: {
            image: v['--image'],
            runtimeProfile: v['--runtime-profile'] ?? 'linux-amd64-codex-v1',
          },
        }
      : {}),
  };
}
