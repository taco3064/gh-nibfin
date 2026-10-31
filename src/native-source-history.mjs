import fs from 'node:fs/promises';
import { join } from 'node:path';
import { readPrivate, ensurePrivateDirectory } from './local-state.mjs';
import {
  nativeSourceBody,
  sourceInstallationIdentity,
} from './native-source-contract.mjs';

const need = (ok, reason) => {
  if (!ok) throw Error('native-source-history:' + reason);
};

/** Retire only a fully merged, independently read-back installation proposal.
 * Unknown writes and incomplete proposals keep their original reconciliation
 * owner. The new installation still checks every managed file before mutation.
 */
export async function retireInstalledSourceProposal(
  core,
  { root, proposal, previous, repository, repositoryId, member, get },
) {
  need(
    proposal.version === 1 &&
      proposal.kind === 'administrative-installation' &&
      proposal.phase === 'installed' &&
      proposal.repository === repository &&
      /^[1-9][0-9]*$/.test(proposal.pr?.number) &&
      proposal.branch === 'nibfin/install-' + proposal.identity.slice(0, 24) &&
      proposal.repositoryId === repositoryId &&
      previous?.repository === repository &&
      previous.repositoryId === repositoryId &&
      core.canonicalJSON(proposal.files) === core.canonicalJSON(previous.files) &&
      proposal.identity ===
        sourceInstallationIdentity(core, {
          repository,
          repositoryId,
          files: previous.files,
        }),
    'original-installation',
  );
  const pr = await get('/pulls/' + proposal.pr.number);
  need(
    pr.id === proposal.pr.id &&
      pr.merged === true &&
      pr.head?.sha === proposal.commit &&
      pr.head.ref === proposal.branch &&
      pr.head.repo?.id === repositoryId &&
      pr.base?.ref === 'main' &&
      pr.base.repo?.id === repositoryId &&
      pr.user?.id === member.id &&
      pr.merge_commit_sha === proposal.mergedCommit &&
      /^[a-f0-9]{40}$/.test(pr.merge_commit_sha) &&
      pr.body === nativeSourceBody(proposal.identity),
    'merged-source-readback',
  );
  const history = join(root, 'managed-source-history');
  await ensurePrivateDirectory(history);
  const path = join(root, 'managed-source-pr.json');
  const target = join(history, proposal.identity + '.json');
  need((await readPrivate(target, { missing: true })) === null, 'history-conflict');
  need(
    core.canonicalJSON(await readPrivate(path)) === core.canonicalJSON(proposal),
    'proposal-drift',
  );
  try {
    await fs.rename(path, target);
  } catch {
    // Reconcile this exact local move; do not erase or replace another proposal.
  }
  need(
    (await readPrivate(path, { missing: true })) === null &&
      core.canonicalJSON(await readPrivate(target)) === core.canonicalJSON(proposal),
    'retirement-readback',
  );
}
