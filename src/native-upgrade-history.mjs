import fs from 'node:fs/promises';
import {join} from 'node:path';
import {privatePath,readPrivate,writePrivate,ensurePrivateDirectory} from './local-state.mjs';
import {managedUpgradeLayout} from './managed-upgrade.mjs';
import {observeUnusedNativePaths} from './native-staged-transition.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-upgrade-history:'+code);};

// The current upgrade keeps its rollback generation in the fixed previous
// paths. Before another reviewed upgrade, park that completed transaction as
// exact private recovery evidence, outside the next transaction's paths. No
// work, login, active runtime, unknown transaction or original journal is erased.
// Caller owns the root lease and has verified the stopped, never-used boundary.
export async function archiveCompletedNativeUpgrade(core,{root,current,installation,observeUnused=observeUnusedNativePaths}={}){
 const journal=await readPrivate(join(root,'upgrade.json'),{missing:true});if(!journal)return {status:'absent'};
 need(journal.phase==='complete'&&/^[a-f0-9-]{36}$/.test(journal.transaction)
  &&core.canonicalJSON(journal.next)===core.canonicalJSON(current),'unfinished-or-different-transaction');
 const plan=await readPrivate(join(root,'upgrade-plan.json'),{limit:4000000});
 need(plan.version===2&&core.canonicalJSON(plan.current)===core.canonicalJSON(journal.current)
  &&core.canonicalJSON(plan.next)===core.canonicalJSON(journal.next),'plan-journal');
 const layout=managedUpgradeLayout(core,root,plan);
 need(core.canonicalJSON(layout.resources)===core.canonicalJSON(journal.resources)
  &&core.canonicalJSON(layout.services)===core.canonicalJSON(journal.services),'resource-journal');
 for(const resource of layout.resources){await layout.verify(resource,'activated');await layout.verify(resource,'previous');}
 const selected=await readPrivate(join(root,'native-installation.json'));
 need(core.canonicalJSON(selected)===core.canonicalJSON(installation),'installation-drift');
 const history=join(root,'upgrade-history');await ensurePrivateDirectory(history);
 for(const name of await fs.readdir(history)){
  need(/^[a-f0-9-]{36}$/.test(name),'history-inventory');
  const prior=await readPrivate(join(history,name,'archive.json')),{identity,...body}=prior;
  need(core.contentIdentity(body)===identity&&prior.phase==='retained','unfinished-history');
 }
 const path=join(history,journal.transaction);await fs.mkdir(path,{mode:0o700});
 const sources=layout.resources.map(r=>r.previous);
 const body={version:1,transaction:journal.transaction,phase:'intent',plan,journal,installation};
 await writePrivate(join(path,'archive.json'),{...body,identity:core.contentIdentity(body)},{exclusive:true});
 need((await observeUnused(sources)).status==='unused','resources');
 for(const resource of layout.resources){await layout.verify(resource,'previous');
  await fs.rename(resource.previous,join(path,resource.name==='runtime'?'runtime':'native-installation.json'));}
 for(const name of ['upgrade-plan.json','upgrade.json'])await fs.rename(join(root,name),join(path,name));
 const done={...body,phase:'retained'};await writePrivate(join(path,'archive.json'),{...done,identity:core.contentIdentity(done)});
 await privatePath(path,{directory:true});return {status:'retained',transaction:journal.transaction};
}
