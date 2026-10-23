import { isAbsolute } from 'node:path';
import { pathToFileURL } from 'node:url';

// Only a trusted installer supplies this configuration; no CLI argument or
// environment variable selects a module, expected identity, or handler.
export async function verifyCoreInstallation(configuration) {
  try {
    const { modulePath, expected } = configuration ?? {};
    if (!isAbsolute(modulePath ?? '') || !expected ||
        !['name', 'version', 'contentDigest'].every(k => typeof expected[k] === 'string' && expected[k].length > 0)) {
      return { ok: false, message: 'Core installation configuration is missing or invalid.' };
    }
    const core = await import(pathToFileURL(modulePath).href);
    if (typeof core.verifyInstallation !== 'function') return { ok: false, message: 'Core verifier is unavailable.' };
    // Core returns the actual identity on success and throws on mismatch.
    // Do not reinterpret its identity record as a boolean or an {ok} result.
    await core.verifyInstallation(expected);
    return { ok: true, message: 'Core installation verified.' };
  } catch {
    return { ok: false, message: 'Core installation verification failed; check the trusted configuration and package.' };
  }
}
