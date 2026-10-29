import {removeAdmission} from './admission.mjs';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {createHash} from 'node:crypto';
import {createInterface} from 'node:readline/promises';
import {withCurrentCore as withRegistrationCore} from './current-core.mjs';
import {safePath,loadEnvironment,withEnvironmentLock,inventory,registrationProcess} from './register.mjs';
import {read as readRecord,publish,binding,managedUnit,terminationObservation,workObservation} from './unregister.mjs';
import {processEnvironment,githubObservation,observeProcess} from './create.mjs';
const read=path=>readRecord(path,1048576);
const fail=()=>{throw Error('Removal unavailable or unsafe state; inspect private removal records and retained resources before retrying.');};
export function removeOptions(args) {
  let name,yes=false;
  for(let i=0;i<args.length;i++) {
    if(args[i]==='--yes' && !yes) yes=true;
    else if(args[i]==='--runner' && name===undefined) name=args[++i];
    else fail();
  }
  if(!/^[a-z][a-z0-9-]{0,47}$/.test(name ?? '')) fail();
  return {name,yes};
}
async function confirm(message) {
  process.stdout.write(message+'\n');
  if(!process.stdin.isTTY || !process.stdout.isTTY) return false;
  const input=createInterface({input:process.stdin,output:process.stdout});
  try {return /^(y|yes)$/i.test((await input.question('Remove this entire environment? [y/N] ')).trim());}
  catch {return false;} finally {input.close();}
}
// Walk without following links. File modes may differ in official distributions;
// ownership, node type and absence of links are the destructive boundary.
async function tree(path,uid) {
  const s=await fs.lstat(path);
  if(s.uid!==uid || s.isSymbolicLink() || !(s.isDirectory() || s.isFile())) fail();
  if(s.isDirectory()) for(const name of await fs.readdir(path)) await tree(join(path,name),uid);
}
const fingerprint=s=>`${s.dev}:${s.ino}:${s.birthtimeMs}`;
function validateState(s,name,account) {
  if(!s || s.schemaVersion!==1 || s.name!==name || s.uid!==account.uid || !/^[1-9][0-9]{0,127}$/.test(s.memberId) ||
      !/^[0-9]+:[0-9]+:[0-9.]+$/.test(s.generation) || !/^[a-f0-9]{64}$/.test(s.descriptor) ||
      !Array.isArray(s.instances) || !s.instances.length || s.instances.length>128 ||
      typeof s.complete!=='boolean' || !['retained','removed'].includes(s.local) || !['stopping','deleting','removed'].includes(s.phase)) fail();
  // Reject arbitrary additions rather than laundering untrusted residue into output.
  if(Object.keys(s).some(k=>!['schemaVersion','name','uid','memberId','generation','descriptor','instances','local','complete','phase'].includes(k))) fail();
  const seen=new Set();
  for(const i of s.instances) {
    if(!/^[a-zA-Z0-9-]{1,80}$/.test(i.id) || seen.has(i.id) ||
        !['unknown','terminated'].includes(i.service) || !['retained','removed'].includes(i.local) ||
        !['unknown','present','absent','not-registered'].includes(i.remote) ||
        !['idle','unresolved'].includes(i.effects) || typeof i.attempted!=='boolean' ||
        Object.keys(i).some(k=>!['id','identity','reservation','service','local','remote','effects','attempted','operations','retainedSources'].includes(k))) fail();
    if(i.operations!==undefined && (!Array.isArray(i.operations) || i.operations.length>16) ||
        i.retainedSources!==undefined && (!Array.isArray(i.retainedSources) || i.retainedSources.length>4 || new Set(i.retainedSources).size!==i.retainedSources.length || i.retainedSources.some(v=>!operationSources.includes(v)))) fail();
    if((i.operations?.length || i.retainedSources?.length) && i.effects!=='unresolved') fail();
    seen.add(i.id);
    if(i.identity && i.reservation || !i.identity && !i.reservation && i.remote!=='not-registered' || i.reservation && (i.remote!=='unknown' || i.effects!=='unresolved')) fail();
  }
  if(s.phase!=='stopping' && s.instances.some(i=>i.service!=='terminated' || i.retainedSources?.length)) fail();
  if(s.complete && (s.local!=='removed' || s.instances.some(i=>i.local!=='removed' || i.service!=='terminated' || i.effects!=='idle' || !['absent','not-registered'].includes(i.remote)))) fail();
  return s;
}
// A failed official registration may never have produced a Runner ID or unit.
// Preserve its validated reservation, but it cannot authorize remote deletion.
function reservation(b,memberId,core,account) {
  const id=v=>typeof v==='string' && /^[1-9][0-9]{0,127}$/.test(v);
  if(b?.schemaVersion!==1 || b.memberId!==memberId || !id(b.registration?.id) || b.target?.id!==b.registration.id ||
      !['repository','organization'].includes(b.registration.level) || b.service?.account!==account.username ||
      !/^nibfin-[a-z0-9-]{1,80}$/.test(b.runnerName ?? '') || b.label!==core.memberRunnerLabel(memberId)) fail();
  const repo=b.registration.level==='repository',c=core.repositoryCoordinates(b.target.owner,repo?b.target.repository:'placeholder');
  if(c.owner!==b.target.owner || repo && (c.repository!==b.target.repository || !id(b.target.ownerId) || !['User','Organization'].includes(b.target.ownerType)) ||
      !repo && b.registration.organization!==c.owner || b.runnerId!==undefined && !id(b.runnerId)) fail();
  return {schemaVersion:1,memberId,registration:{level:b.registration.level,id:b.registration.id,...(!repo?{organization:c.owner}:{})},
    target:{id:b.target.id,owner:c.owner,...(repo?{repository:c.repository,ownerId:b.target.ownerId,ownerType:b.target.ownerType}:{})},
    label:b.label,runnerName:b.runnerName,service:{account:account.username},...(b.runnerId?{runnerId:b.runnerId}:{})};
}
// These are source observations for operator/runtime readback, never authority
// to execute or replay an operation. Unknown schemas keep their original file.
const operationSources=['execution.json','execution.json.pending','write-intent.json','write-intent.json.pending'];
function operationIdentity(raw,source,core) {
  const decimal=v=>typeof v==='string' && /^[1-9][0-9]{0,127}$/.test(v) ? v : Number.isSafeInteger(v) && v>0 ? String(v) : undefined;
  const label=v=>typeof v==='string' && /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(v);
  const ids=['issueId','issueNumber','pullRequestId','pullRequestNumber','repositoryId','runnerId','runId','runAttempt'];
  const labels=['requestId','correlation','environment'];
  const hashes=['commitSha','headSha','baseSha'];
  const discard=['token','credentials','authorization','secret','password','stdout','stderr','log','response'];
  if(!operationSources.includes(source) || !raw || Array.isArray(raw) || typeof raw!=='object' ||
      !label(raw.operation) || raw.operation.length>80 || !label(raw.requestId) ||
      typeof raw.repository!=='string' || raw.repository.split('/').length!==2 ||
      raw.source!==undefined && raw.source!==source || raw.schemaVersion!==undefined && raw.schemaVersion!==1 ||
      Object.keys(raw).some(k=>!['source','schemaVersion','operation','repository',...ids,...labels,...hashes,...discard].includes(k))) fail();
  const coordinates=core.repositoryCoordinates(...raw.repository.split('/'));
  if(coordinates.key!==raw.repository) fail();
  const value={source,operation:raw.operation,repository:coordinates.key,requestId:raw.requestId};
  if(raw.schemaVersion!==undefined) value.schemaVersion=raw.schemaVersion;
  for(const key of ids) if(raw[key]!==undefined) {
    const prefix={issueId:'I_',pullRequestId:'PR_',repositoryId:'R_'}[key];
    const id=decimal(raw[key]) ?? (prefix && typeof raw[key]==='string' && raw[key].startsWith(prefix) && /^[A-Za-z0-9_-]{3,128}$/.test(raw[key]) ? raw[key] : undefined);
    if(!id) fail();value[key]=id;
  }
  for(const key of labels) if(raw[key]!==undefined) {if(!label(raw[key])) fail();value[key]=raw[key];}
  for(const key of hashes) if(raw[key]!==undefined) {if(!/^[a-f0-9]{40}(?:[a-f0-9]{24})?$/.test(raw[key])) fail();value[key]=raw[key];}
  if(!ids.some(k=>value[k]) && !hashes.some(k=>value[k])) fail();
  return value;
}
async function captureOperations(instance,item,core) {
  const operations=[],retained=[];
  for(const source of operationSources) {
    try {
      const raw=await read(join(instance.service,source));
      if(raw!==undefined) {item.effects='unresolved';operations.push(operationIdentity(raw,source,core));}
    } catch {item.effects='unresolved';retained.push(source);}
  }
  // Append distinct observations: changed/absent source files cannot erase prior
  // readback identities. Exceeding the bounded inventory retains source evidence.
  const merged=[...new Map([...(item.operations ?? []),...operations].map(o=>[JSON.stringify(o),o])).values()];
  if(merged.length>16) fail();
  item.operations=merged;
  item.retainedSources=retained;
}
async function remoteCleanup(item,core,environment,dependencies,save) {
  if(!item.identity) return;
  const b=item.identity, repo=b.registration.level==='repository';
  const prefix=repo?`/repos/${b.target.owner}/${b.target.repository}`:`/orgs/${b.target.owner}`;
  const endpoint=prefix+'/actions/runners';
  const signal=AbortSignal.any([AbortSignal.timeout(dependencies.remoteTimeoutMs ?? 15000),...(dependencies.signal?[dependencies.signal]:[])]);
  try {
    if(await githubObservation({run:dependencies.run ?? observeProcess,environment,account:dependencies.account ?? userInfo()})!==b.memberId) return;
    const provider=core.createGithubProvider({transport:dependencies.transport ?? core.localGhTransport(),attempts:1,
      authorize:r=>r.method==='DELETE' && r.endpoint===endpoint+'/'+b.runnerId && r.target===b.registration.id});
    const observe=async()=>{
      const r=await provider.request({endpoint:prefix,target:b.registration.id,operation:'remove-readback',correlation:item.id,signal});
      const d=r.data;
      if(r.outcome!=='read' || d?.id!==b.target.id || (repo ? d.private!==false || d.visibility!=='public' ||
          d.full_name?.toLowerCase()!==`${b.target.owner}/${b.target.repository}` || d.owner?.id!==b.target.ownerId || d.owner?.type!==b.target.ownerType :
          d.type!=='Organization' || d.login?.toLowerCase()!==b.target.owner)) return undefined;
      const runners=await inventory(provider,endpoint,'runners',b.registration.id,signal), runner=runners.find(r=>r.id===b.runnerId);
      if(runner && (runner.name!==b.runnerName || runner.os!=='linux' || !['online','offline'].includes(runner.status) || typeof runner.busy!=='boolean' ||
          !Array.isArray(runner.labels) || !runner.labels.some(l=>l.name===b.label))) return undefined;
      return runner?{busy:runner.busy}:null;
    };
    let observed=await observe();
    item.remote=observed===undefined?'unknown':observed===null?'absent':'present';
    // Read back uncertain prior writes, never replay them. Busy is an unresolved
    // external execution, not permission to keep the member's local resources.
    if(observed?.busy) item.effects='unresolved';
    if(observed && !observed.busy && !item.attempted && !signal.aborted) {
      item.attempted=true;await save();
      try {await provider.request({method:'DELETE',endpoint:endpoint+'/'+b.runnerId,target:b.registration.id,operation:'remove',correlation:item.id,signal});} catch {}
      observed=await observe();item.remote=observed===undefined?'unknown':observed===null?'absent':'present';
    }
  } catch {item.remote='unknown';}
}
async function stop(instance,item,record,environment,dependencies) {
  const unit=`nibfin-${record.name}-${instance.id}.service`,path=join(instance.service,unit);
  const observe=()=> (dependencies.termination ?? terminationObservation)(unit,environment,dependencies.timeoutMs ?? 15000,['BoundBy','ConsistsOf','UpheldBy']);
  const invoke=async args=>(await (dependencies.process ?? registrationProcess)('systemctl',['--user',...args],
    {cwd:instance.service,env:environment,timeoutMs:dependencies.timeoutMs ?? 15000,signal:dependencies.signal}))?.ok===true;
  const empty=o=>o.MainPID==='0' && (o.TasksCurrent==='0' || o.ControlGroup==='' && o.TasksCurrent==='[not set]');
  const missing=o=>o.LoadState==='not-found' && o.ActiveState==='inactive' && empty(o);
  const owned=o=>o.LoadState==='loaded' && o.FragmentPath===path && o.DropInPaths==='' && o.KillMode==='control-group' &&
    o.NeedDaemonReload==='no' && o.TriggeredBy==='' && o.PropagatesStopTo==='' && o.BoundBy==='' && o.ConsistsOf==='' && o.UpheldBy==='';
  const initial=await observe();
  if(missing(initial)) {
    // A missing unit must not conceal an unvalidated source that could restart.
    try {await fs.lstat(path);await managedUnit(path,instance);} catch(e) {if(e.code!=='ENOENT') throw e;}
  } else {
    if(!owned(initial)) return false;
    try {await managedUnit(path,instance);}
    catch(e) {
      // A preceding attempt may have unlinked the owned source before a failed
      // reload. Require fresh disabled/empty proof before finishing that reload.
      if(e.code!=='ENOENT' || !['inactive','failed'].includes(initial.ActiveState) || !empty(initial) || initial.UnitFileState!=='disabled') throw e;
      if(!await invoke(['daemon-reload'])) return false;
      const refreshed=await observe();
      if(!(missing(refreshed) || owned(refreshed) && ['inactive','failed'].includes(refreshed.ActiveState) && empty(refreshed) && refreshed.UnitFileState==='disabled')) return false;
      item.service='terminated';return true;
    }
    if(!await invoke(['stop',unit])) return false;
    const ended=await observe();
    if(!owned(ended) || !['inactive','failed'].includes(ended.ActiveState) || !empty(ended)) return false;
    if(!await invoke(['disable',unit])) return false;
    const disabled=await observe();
    if(!(missing(disabled) || owned(disabled) && ['inactive','failed'].includes(disabled.ActiveState) && empty(disabled) && disabled.UnitFileState==='disabled')) return false;
  }
  await fs.rm(path,{force:true});
  if(!await invoke(['daemon-reload'])) return false;
  item.service='terminated';return true;
}
export async function removeEnvironment(args,dependencies={}) {
  const {name,yes}=removeOptions(args), account=dependencies.account ?? userInfo();
  if(process.platform!=='linux' || account.uid===0 || account.uid!==userInfo().uid || account.username!==userInfo().username) fail();
  const root=dependencies.root ?? join(account.homedir,'.local/share/nibfin/environments'),base=join(root,name);
  return withEnvironmentLock(root,name,async()=>{
    const records=join(root,'.removals'),statePath=join(records,name+'.json');
    let state=await read(statePath),record,stat;
    if(state) validateState(state,name,account);
    let pending=await read(statePath+'.pending');
    if(pending) {
      validateState(pending,name,account);
      if(state && (pending.generation!==state.generation || pending.descriptor!==state.descriptor || pending.memberId!==state.memberId ||
          JSON.stringify(pending.instances.map(i=>[i.id,i.identity,i.reservation]))!==JSON.stringify(state.instances.map(i=>[i.id,i.identity,i.reservation])) ||
          state.instances.some((i,n)=>i.attempted && !pending.instances[n].attempted ||
            i.operations?.some(o=>!pending.instances[n].operations?.some(p=>JSON.stringify(p)===JSON.stringify(o)))))) fail();
      state=pending;
    }
    try {stat=await fs.lstat(base);} catch(e) {if(e.code!=='ENOENT') throw e;}
    const save=async()=> {
      if(Buffer.byteLength(JSON.stringify(state,null,2)+'\n')>1048576) fail();
      if(pending) {await fs.rename(statePath+'.pending',statePath);pending=undefined;}
      return (dependencies.publish ?? publish)(statePath,state);
    };
    const result=()=>({...state,residue:statePath});
    const message=items=>`Remove environment ${name}: ALL instances, workspaces, application data and environment Agent credentials.\n`+
      items.map(i=>i.identity?`${i.id}: ${i.identity.registration.level} ${i.identity.target.owner}${i.identity.target.repository?'/'+i.identity.target.repository:''}; Runner ${i.identity.runnerId}${i.identity.registration.level==='organization'?' (entire shared Organization registration)':''}`:`${i.id}: ${i.reservation?i.reservation.registration.level+' '+i.reservation.target.owner+(i.reservation.target.repository?'/'+i.reservation.target.repository:' (entire shared Organization registration)')+'; unresolved registration':'unregistered'}`).join('\n');
    const validateIdentities=core=>{
      for(const i of state.instances) {
        const observations=new Set();
        for(const o of i.operations ?? []) {
          const serialized=JSON.stringify(o);
          if(observations.has(serialized) || JSON.stringify(operationIdentity(o,o.source,core))!==serialized) fail();
          observations.add(serialized);
        }
        if(i.reservation && JSON.stringify(reservation(i.reservation,state.memberId,core,account))!==JSON.stringify(i.reservation)) fail();
        if(!i.identity) continue;
        const clean=binding(i.identity,{id:i.id},{name,githubId:state.memberId},core,account);
        if(JSON.stringify(clean)!==JSON.stringify(i.identity)) fail();
      }
    };
    if(!stat) {
      if(!state) fail();
      const tomb=join(records,name+'.resources');
      let t;try {t=await fs.lstat(tomb);} catch(e) {if(e.code!=='ENOENT') throw e;}
      if(state.phase==='stopping' || t && fingerprint(t)!==state.generation) fail();

      // Validate retained identity without trusting old arbitrary payloads.
      const execute=async core=>{
        validateIdentities(core);
        if(!yes && !await (dependencies.confirm ?? confirm)(message(state.instances))) return {cancelled:true,complete:false,instances:[]};
        if(t) {await safePath(tomb);await tree(tomb,account.uid);await (dependencies.remove ?? fs.rm)(tomb,{recursive:true,force:true});}
        state.phase='removed';
        if(state.local!=='removed') {state.local='removed';for(const i of state.instances) i.local='removed';}
        const environment=processEnvironment('/nonexistent');
        for(const i of state.instances) await remoteCleanup(i,core,environment,dependencies,save);
        state.complete=state.instances.every(i=>i.service==='terminated' && i.effects==='idle' && ['absent','not-registered'].includes(i.remote));
        await save();return {...result(),already:true};
      };
      return dependencies.core?execute(dependencies.core):withRegistrationCore(execute);
    }
    record=await loadEnvironment(root,name,account);
    const descriptor=createHash('sha256').update(await fs.readFile(join(base,'environment.json'))).digest('hex');
    if(state && (state.generation!==fingerprint(stat) || state.descriptor!==descriptor)) fail();
    await tree(base,account.uid);
    const execute=async core=>{
      if(state) validateIdentities(core);
      const items=[];
      for(const instance of record.instances) {
        const current=await read(join(instance.registration,'binding.json')),old=await read(join(instance.registration,'unregister.json'));
        const pending=await read(join(instance.registration,'binding.json.pending'));
        if(pending && current && (pending.runnerId!==current.runnerId || pending.instanceId!==current.instanceId || pending.memberId!==current.memberId ||
            JSON.stringify(pending.registration)!==JSON.stringify(current.registration) || JSON.stringify(pending.target)!==JSON.stringify(current.target))) fail();
        if(current && old?.complete===false && old.identity?.runnerId!==current.runnerId) fail();
        const raw=pending ?? current ?? old?.identity;
        const partial=raw && (raw.disposition==='pending' || raw.disposition==='unknown' && (!raw.platform || !raw.service?.unit));
        const reserved=partial?reservation(raw,record.githubId,core,account):undefined;
        const identity=raw && !partial?binding(raw,instance,record,core,account):undefined;
        if(raw && raw.instanceId!==instance.id) fail();
        if(!identity && !reserved && instance.status!=='unregistered') fail();
        const previous=state?.instances.find(i=>i.id===instance.id);
        if(previous && (JSON.stringify(previous.identity)!==JSON.stringify(identity) || JSON.stringify(previous.reservation)!==JSON.stringify(reserved))) fail();
        items.push(previous ?? {id:instance.id,...(identity?{identity}:{}),...(reserved?{reservation:reserved}:{}),service:'unknown',local:'retained',
          remote:identity || reserved?'unknown':'not-registered',effects:reserved?'unresolved':'idle',attempted:old?.attempted===true && old.identity?.runnerId===identity?.runnerId});
      }
      if(state && (items.length!==state.instances.length || items.some((i,n)=>i.id!==state.instances[n].id))) fail();
      if(!yes && !await (dependencies.confirm ?? confirm)(message(items))) return {cancelled:true,complete:false,instances:[]};
      if(!state) state={schemaVersion:1,name,uid:account.uid,memberId:record.githubId,generation:fingerprint(stat),descriptor,instances:items,local:'retained',complete:false,phase:'stopping'};
      const locks=[];
      try {
        // Acquire EVERY existing per-instance lock before publishing the removal
        // marker or performing effects. Active unregister retains its resources.
        for(const i of record.instances) {
          const p=join(i.service,'registration.lock');
          try {await fs.mkdir(p,{mode:0o700});}
          catch(e) {
            if(e.code!=='EEXIST') throw e;
            // Only recover locks explicitly owned by a dead removal process.
            const owner=await read(join(p,'remove-owner.json')) ?? await read(join(p,'remove-owner.json.pending')) ?? await read(join(p,'lifecycle-owner.json'));
            if(!owner || owner.generation!==undefined && owner.generation!==state.generation || !Number.isSafeInteger(owner.pid) || owner.pid<1) fail();
            try {process.kill(owner.pid,0);fail();} catch(error) {if(error.code!=='ESRCH') throw error;}
            await fs.rm(join(p,'remove-owner.json'),{force:true});await fs.rm(join(p,'remove-owner.json.pending'),{force:true});await fs.rm(join(p,'lifecycle-owner.json'),{force:true});await fs.rmdir(p);await fs.mkdir(p,{mode:0o700});
          }
          locks.push(p);await publish(join(p,'remove-owner.json'),{pid:process.pid,generation:state.generation});
        }
        await fs.mkdir(records,{mode:0o700}).catch(e=>{if(e.code!=='EEXIST') throw e;});await safePath(records);
        await save();
        const marker=join(base,'.removing');
        const markerPending=await read(marker+'.pending');
        const existing=markerPending ?? await read(marker);
        if(existing && (existing.generation!==state.generation || existing.descriptor!==state.descriptor || Object.keys(existing).length!==2)) fail();
        if(markerPending) await fs.rename(marker+'.pending',marker);
        if(!existing) await publish(marker,{generation:state.generation,descriptor});
        const environment=processEnvironment(record.loginSpace);environment.XDG_RUNTIME_DIR=`/run/user/${account.uid}`;
        for(const [n,instance] of record.instances.entries()) {
          const item=state.instances[n];item.service='unknown';
          try {
            if(!await stop(instance,item,record,environment,dependencies)) continue;
            let work;try {work=await (dependencies.work ?? workObservation)(instance);} catch {work='unknown';}
            if(work!=='idle') item.effects='unresolved';
            await captureOperations(instance,item,core);
            await save();
            await remoteCleanup(item,core,environment,dependencies,save);
          } catch {item.service='unknown';}
          await save();
        }
        if(state.instances.some(i=>i.service!=='terminated' || i.retainedSources?.length)) {await save();return result();}
        for(const instance of record.instances)await removeAdmission(instance);
        // Preserve credential-free unknown-effect facts before erasing evidence.
        await save();await tree(base,account.uid);
        // Atomic name withdrawal preserves the entire stopped environment for
        // interruption/reentry. Its inode binds quarantine to this generation.
        const tomb=join(records,name+'.resources');
        try {await fs.lstat(tomb);fail();} catch(e) {if(e.code!=='ENOENT') throw e;}
        state.phase='deleting';await save();
        await fs.rename(base,tomb);
        await (dependencies.remove ?? fs.rm)(tomb,{recursive:true,force:true});
        state.phase='removed';
        state.local='removed';for(const i of state.instances) i.local='removed';
        state.complete=state.instances.every(i=>i.effects==='idle' && ['absent','not-registered'].includes(i.remote));
        await save();return result();
      } catch {if(state) {state.complete=false;try {await save();} catch {}}return result();}
      finally {
        // The name may already have been reused after atomic withdrawal. Never
        // clean old lock paths through a new environment at the same pathname.
        let same=false;try {same=fingerprint(await fs.lstat(base))===state.generation;} catch {}
        if(same) for(const p of locks) {await fs.rm(join(p,'remove-owner.json'),{force:true}).catch(()=>{});await fs.rmdir(p).catch(()=>{});}
      }
    };
    return dependencies.core?execute(dependencies.core):withRegistrationCore(execute);
  });
}
