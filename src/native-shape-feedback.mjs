import { join } from 'node:path';
import { openFileHostStore } from './host-store.mjs';
const need = (v, c) => {
  if (!v) throw Error('native-shape-feedback:' + c);
};
// Core owns current proposal validation and recovery. This adapter only finishes
// an already-retained pre-1.1 journal; it never creates another format revision.
export async function nativeShapeFeedback(
  core,
  { root, store, claim, provider, allowPaused = false } = {},
) {
  const s = await store.read();
  if (s.shapeRevision?.phase === 'prepared') {
    const marker = s.shapeRevision,
      event = await store.get(marker.reference);
    need(
      event.kind === 'native-shape-format-feedback' &&
        event.scopeHash === s.scopeHash &&
        event.cycle === s.cycle,
      'feedback-journal',
    );
    const issue = await provider.request({
      endpoint: '/repos/' + s.target.repository + '/issues/' + s.target.number,
      target: s.target.repositoryId,
      operation: 'native-shape-feedback',
      correlation: event.source.request,
    });
    need(
      issue.outcome === 'read' &&
        issue.data.state === 'open' &&
        issue.data.assignees.some((a) => a.id === s.member.id) &&
        core.bodyDigest(core.parseRequirementsDocument(issue.data.body).scope) ===
          s.scopeHash,
      'current-scope',
    );
    const host = await openFileHostStore(core, { directory: join(root, 'state') });
    try {
      for (let attempt = 0; attempt < 20; attempt++) {
        const fresh = await host.read(),
          w = fresh.work,
          own = w.pending.filter((p) => p.cycle === s.cycle);
        need(
          w.restrictionsChecked &&
            (!w.paused || allowPaused) &&
            !w.hold &&
            !w.unknownWrite,
          'owner-restriction',
        );
        need(
          own.length === 0 ||
            (own.length === 1 &&
              own[0].request === event.source.request &&
              own[0].status === 'UNACCEPTED'),
          'pending-changed',
        );
        if (own.length) {
          const next = w.pending.filter((p) => p.cycle !== s.cycle),
            active = next.some(
              (p) =>
                p.kind !== 'native-stage' ||
                ['ACTIVE', 'UNRESOLVED'].includes(p.status),
            );
          try {
            await host.updateWork({
              expected: fresh.identity,
              work: {
                ...w,
                pending: next,
                active,
                terminated: !active,
                reconciled: next.length === 0 && !active,
              },
            });
          } catch (e) {
            if (e.message?.endsWith('work-compare-and-swap')) continue;
            throw e;
          }
        }
        await store.update((v) => {
          need(v.shapeRevision.id === marker.id, 'feedback-changed');
          v.shapeRevision.phase = 'applied';
          return v;
        });
        return {
          outcome: 'revision-prepared',
          id: marker.id,
          reference: marker.reference,
        };
      }
      need(false, 'work-unavailable');
    } finally {
      await host.close();
    }
  }
  return { outcome: 'not-applicable' };
}
