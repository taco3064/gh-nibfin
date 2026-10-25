import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawnSync} from 'node:child_process';
import {withRegistrationCore} from './core.mjs';
import {safePath, loadEnvironment, inventory, registrationProcess} from './register.mjs';
import {processEnvironment, githubObservation, observeProcess} from './create.mjs';
const fail = () => {throw Error('Unregister unavailable or conflicting state; inspect private instance records and reconcile before retrying.');};
const id = v => typeof v === 'string' && /^[1-9][0-9]{0,127}$/.test(v);
export function unregisterOptions(args) {
  const v = {};
  for (let i=0;i<args.length;i+=2) {
    if (!['--runner','--repo','--org','--instance'].includes(args[i]) || args[i] in v ||
        !args[i+1] || /[\x00-\x20\x7f]/.test(args[i+1])) fail();
    v[args[i]]=args[i+1];
  }
  if (!/^[a-z][a-z0-9-]{0,47}$/.test(v['--runner'] ?? '') || Boolean(v['--repo']) === Boolean(v['--org']) ||
      v['--instance'] !== undefined && !/^[a-zA-Z0-9-]{1,80}$/.test(v['--instance'])) fail();
  return {name:v['--runner'],repo:v['--repo'],org:v['--org'],instance:v['--instance']};
}
export async function read(path, limit=65536) {
  try {
    await safePath(path,{directory:false});
    const h = await fs.open(path,fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
    try {const b=Buffer.alloc(limit+1), r=await h.read(b,0,b.length,0);if(r.bytesRead>limit) fail();return JSON.parse(b.subarray(0,r.bytesRead));}
    finally {await h.close();}
  } catch(e) {if(e.code==='ENOENT') return undefined;throw e;}
}
export async function publish(path, value) {
  const temp=path+'.pending';
  const h=await fs.open(temp,'wx',0o600);
  try {await h.writeFile(JSON.stringify(value,null,2)+'\n');await h.sync();} finally {await h.close();}
  await fs.rename(temp,path);
  const d=await fs.open(join(path,'..'),'r');try {await d.sync();} finally {await d.close();}
}
// Require actual systemd observations, including the unit's origin and empty cgroup.
export function terminationObservation(unit, environment, timeoutMs, extraKeys=[]) {
  const keys=['LoadState','ActiveState','MainPID','TasksCurrent','FragmentPath','UnitFileState','ControlGroup','DropInPaths','KillMode','NeedDaemonReload','TriggeredBy','PropagatesStopTo',...extraKeys];
  const r=spawnSync('systemctl',['--user','show',unit,...keys.map(k=>'--property='+k)],
    {env:environment,cwd:'/tmp',shell:false,encoding:'utf8',timeout:timeoutMs,maxBuffer:8192});
  if (r.error || r.status!==0) return {};
  const lines=r.stdout.trim().split('\n');
  if(lines.length!==keys.length || new Set(lines.map(l=>l.split('=')[0])).size!==keys.length) return {};
  return Object.fromEntries(lines.map(l=>l.split('=')));
}
// No product runtime journal exists in this handoff. Only a pristine workspace
// without Worker history establishes no execution needing reconciliation here.
// Used instances conservatively retain resources for the owning runtime/operator.
export async function workObservation(instance) {
  if ((await fs.readdir(instance.workspace)).length) return 'unknown';
  try {
    const diag=join(instance.registration,'_diag');await safePath(diag);
    if((await fs.readdir(diag)).some(n=>/^Worker_/.test(n))) return 'unknown';
  } catch(e) {if(e.code!=='ENOENT') return 'unknown';}
  for(const name of ['execution.json','write-intent.json']) if(await read(join(instance.service,name)) !== undefined) return 'unknown';
  return 'idle';
}
export function binding(b, instance, record, core, account) {
  if(!b || b.schemaVersion!==1 || b.instanceId!==instance.id || b.memberId!==record.githubId ||
      !['registered','unknown'].includes(b.disposition) || !id(b.runnerId) || !id(b.registration?.id) ||
      b.target?.id!==b.registration.id || !['repository','organization'].includes(b.registration.level) ||
      b.label!==core.memberRunnerLabel(record.githubId) || b.platform!=='linux' ||
      b.service?.account!==account.username || b.service.unit!==`nibfin-${record.name}-${instance.id}.service` ||
      !['online','offline'].includes(b.status) || typeof b.busy!=='boolean') fail();
  const repo=b.registration.level==='repository';
  if(b.runnerName!==undefined && !/^nibfin-[a-z0-9-]{1,80}$/.test(b.runnerName)) fail();
  const c=core.repositoryCoordinates(b.target.owner,repo?b.target.repository:'placeholder');
  if(c.owner!==b.target.owner || repo && (c.repository!==b.target.repository || !id(b.target.ownerId) ||
      !['User','Organization'].includes(b.target.ownerType)) || !repo && b.registration.organization!==c.owner) fail();
  // Copy only validated identity fields; never persist arbitrary binding/provider data.
  return {schemaVersion:1,instanceId:instance.id,memberId:b.memberId,runnerId:b.runnerId,label:b.label,platform:'linux',
    disposition:'unknown',status:b.status,busy:b.busy,...(b.runnerName?{runnerName:b.runnerName}:{}),
    registration:{level:b.registration.level,id:b.registration.id,...(!repo?{organization:c.owner}:{})},
    target:{id:b.target.id,owner:c.owner,...(repo?{repository:c.repository,ownerId:b.target.ownerId,ownerType:b.target.ownerType}:{})},
    service:{account:account.username,unit:b.service.unit,status:'unknown'}};
}
export async function managedUnit(path, instance) {
  await safePath(path,{directory:false});
  const text=await fs.readFile(path,'utf8');
  if(text.length>65536) fail();
  const quote=(v,expand=false)=>'"'+v.replace(/\\/g,'\\\\').replace(/"/g,'\\"').replace(/%/g,'%%').replace(/\$/g,expand?'$$$$':'$')+'"';
  const lines=text.trim().split('\n');
  const allowed=/^(\[(Unit|Service|Install)\]|Description=Nibfin instance runner|WorkingDirectory=.*|ExecStart=.*|Environment=.*|UnsetEnvironment=.*|Restart=on-failure|WantedBy=default.target)$/;
  if(lines.some(l=>!allowed.test(l)) ||
      lines.filter(l=>l.startsWith('ExecStart=')).join('')!=='ExecStart='+quote(join(instance.registration,'runsvc.sh'),true) ||
      lines.filter(l=>l.startsWith('WorkingDirectory=')).join('')!=='WorkingDirectory='+quote(instance.registration)) fail();
}
export async function unregisterInstance(args, dependencies={}) {
  const options=unregisterOptions(args);
  const execute=async core=>{
    const account=dependencies.account ?? userInfo();
    if(process.platform!=='linux' || account.uid===0 || account.uid!==userInfo().uid) fail();
    let coordinates;
    try {
      if(options.repo && options.repo.split('/').length!==2) fail();
      coordinates=options.repo?core.repositoryCoordinates(...options.repo.split('/')):core.repositoryCoordinates(options.org,'placeholder');
    } catch {fail();}
    const root=dependencies.root ?? join(account.homedir,'.local/share/nibfin/environments');
    const record=await loadEnvironment(root,options.name,account), matches=[];
    for(const instance of record.instances) {
      if(options.instance && instance.id!==options.instance) continue;
      const b=await read(join(instance.registration,'binding.json'));
      const old=b?undefined:await read(join(instance.registration,'unregister.json'));
      const identity=b ?? old?.identity;
      if(!identity) continue;
      const value=binding(identity,instance,record,core,account);
      if(value.registration.level===(options.repo?'repository':'organization') && value.target.owner===coordinates.owner &&
          (!options.repo || value.target.repository===coordinates.repository)) matches.push({instance,value,bound:Boolean(b)});
    }
    const active=matches.filter(m=>m.bound), selected=active.length?active:matches;
    if(selected.length!==1) fail();
    const {instance,value}=selected[0], lock=join(instance.service,'registration.lock');
    let locked=false, intent=false;
    const statePath=join(instance.registration,'unregister.json'), bindingPath=join(instance.registration,'binding.json');
    const save=dependencies.publish ?? publish;
    const result={schemaVersion:1,instanceId:instance.id,identity:value,local:'retained',service:'unknown',remote:'unknown',complete:false};
    try {
      await fs.mkdir(lock,{mode:0o700});locked=true;
      await fs.writeFile(join(lock,'lifecycle-owner.json'),JSON.stringify({pid:process.pid}),{flag:'wx',mode:0o600});
      try {await fs.lstat(join(root,options.name,'.removing'));return result;} catch(e) {if(e.code!=='ENOENT') throw e;}
      const current=await read(bindingPath), raw=await read(statePath);
      let previous;
      if(raw) {
        if(raw.schemaVersion!==1 || raw.instanceId!==instance.id || typeof raw.complete!=='boolean' || typeof raw.attempted!=='boolean' ||
            !['retained','cleanup-unknown','detached','publication-unknown'].includes(raw.local) ||
            !['unknown','terminated'].includes(raw.service) || !['unknown','present','absent'].includes(raw.remote) ||
            raw.complete && (raw.local!=='detached' || raw.service!=='terminated' || raw.remote!=='absent')) fail();
        previous={schemaVersion:1,instanceId:instance.id,identity:binding(raw.identity,instance,record,core,account),
          local:raw.local,service:raw.service,remote:raw.remote,complete:raw.complete,attempted:raw.attempted};
      }
      if(current && JSON.stringify(binding(current,instance,record,core,account))!==JSON.stringify(value)) fail();
      if(!current && (!previous || JSON.stringify(previous.identity)!==JSON.stringify(value))) fail();
      if(previous && previous.identity?.runnerId!==value.runnerId && !previous.complete) fail();
      // Pending files are evidence of interrupted publication, never overwritten.
      if(await read(statePath+'.pending') !== undefined || await read(bindingPath+'.pending') !== undefined) fail();
      if(!current && previous.complete===true) {Object.assign(result,previous,{already:true});return result;}
      const priorAttempt=previous?.identity?.runnerId===value.runnerId && previous.attempted===true;
      result.attempted=priorAttempt;
      if(previous?.identity?.runnerId===value.runnerId && previous.service==='terminated') result.service='terminated';
      await save(statePath,result);intent=true; // Durable identity and intent before any effect.
      await save(bindingPath,value); // Unknown/pending lifecycle cannot be reused by registration.
      const environment=processEnvironment(record.loginSpace);environment.XDG_RUNTIME_DIR=`/run/user/${account.uid}`;
      const proc=dependencies.process ?? registrationProcess;
      const invoke=async argv=>(await proc('systemctl',['--user',...argv],{cwd:instance.service,env:environment,
        signal:dependencies.signal,timeoutMs:dependencies.timeoutMs ?? 15000}))?.ok===true;
      const observe=()=> (dependencies.termination ?? terminationObservation)(value.service.unit,environment,dependencies.timeoutMs ?? 15000);
      const unitPath=join(instance.service,value.service.unit);
      const initial=await observe();result.service='unknown';
      const owned=o=>o.LoadState==='loaded' && o.FragmentPath===unitPath && o.DropInPaths==='' &&
        o.KillMode==='control-group' && o.NeedDaemonReload==='no' && o.TriggeredBy==='' && o.PropagatesStopTo==='';
      const empty=o=>o.MainPID==='0' && (o.TasksCurrent==='0' || o.ControlGroup==='' && o.TasksCurrent==='[not set]');
      const missing=o=>o.LoadState==='not-found' && o.ActiveState==='inactive' && o.MainPID==='0';
      if(owned(initial)) {
        await managedUnit(unitPath,instance);
        if(!await invoke(['stop',value.service.unit])) return result;
        const stopped=await observe();
        if(!owned(stopped) || !['inactive','failed'].includes(stopped.ActiveState) || !empty(stopped)) return result;
        if(!await invoke(['disable',value.service.unit])) return result;
        const disabled=await observe();
        if(!(missing(disabled) || owned(disabled) && ['inactive','failed'].includes(disabled.ActiveState) &&
            empty(disabled) && disabled.UnitFileState==='disabled')) return result;
      } else if(!(previous?.service==='terminated' && missing(initial))) return result;
      result.service='terminated';await save(statePath,result);
      const signal=AbortSignal.any([AbortSignal.timeout(dependencies.remoteTimeoutMs ?? 30000),...(dependencies.signal?[dependencies.signal]:[])]);
      const prefix=options.repo?`/repos/${coordinates.owner}/${coordinates.repository}`:`/orgs/${coordinates.owner}`;
      const endpoint=prefix+'/actions/runners';
      let authenticated=false;
      try {authenticated=await githubObservation({run:dependencies.run ?? observeProcess,environment,account})===value.memberId;} catch {}
      const provider=core.createGithubProvider({transport:dependencies.transport ?? core.localGhTransport(),attempts:1,
        authorize:r=>r.method==='DELETE' && r.endpoint===endpoint+'/'+value.runnerId && r.target===value.registration.id});
      let remote, mismatch=false;
      const observeRemote=async()=>{
        if(!authenticated) return undefined;
        try {
          const t=await provider.request({endpoint:prefix,target:value.registration.id,operation:'unregister-readback',correlation:instance.id,signal});
          if(t.outcome!=='read') return undefined;
          const d=t.data;
          if(d.id!==value.registration.id || (options.repo ? d.private!==false || d.visibility!=='public' || d.full_name?.toLowerCase()!==coordinates.key ||
              d.owner?.id!==value.target.ownerId || d.owner?.type!==value.target.ownerType :
              d.type!=='Organization' || d.login?.toLowerCase()!==coordinates.owner)) {mismatch=true;return undefined;}
          const list=await inventory(provider,endpoint,'runners',value.registration.id,signal);
          const r=list.find(r=>r.id===value.runnerId);
          if(r && (r.name!==value.runnerName && value.runnerName!==undefined || r.os!=='linux' ||
              !['online','offline'].includes(r.status) || typeof r.busy!=='boolean' ||
              !Array.isArray(r.labels) || !r.labels.some(l=>l.name===value.label))) {mismatch=true;return undefined;}
          return {present:Boolean(r),busy:r?.busy ?? false};
        } catch {return undefined;}
      };
      remote=await observeRemote();
      result.remote=remote?(remote.present?'present':'absent'):'unknown';
      const work=await (dependencies.work ?? workObservation)(instance);
      if(mismatch || work!=='idle' || (remote?.busy ?? value.busy)) return result;
      // A previous uncertain DELETE is reconciled only; do not blindly replay it.
      if(remote?.present && !priorAttempt && !dependencies.signal?.aborted) {
        result.attempted=true;await save(statePath,result);
        await provider.request({method:'DELETE',endpoint:endpoint+'/'+value.runnerId,target:value.registration.id,
          operation:'unregister',correlation:instance.id,signal});
        remote=await observeRemote();result.remote=remote?(remote.present?'present':'absent'):'unknown';
      }
      if(mismatch || remote?.busy) return result;
      // Validate every cleanup leaf before removing any. Never recurse or touch work/login/application.
      const leaves=['.runner','.runner_migrated','.credentials','.credentials_migrated','.credentials_rsaparams','.credential_store','.service','runsvc.sh'];
      for(const name of leaves) {
        try {await safePath(join(instance.registration,name),{directory:false});}
        catch(e) {if(e.code!=='ENOENT') throw e;}
      }
      result.local='cleanup-unknown';await save(statePath,result);
      for(const name of leaves) await fs.rm(join(instance.registration,name),{force:true});
      // systemctl disable removed user-manager symlinks; unlink only the owned source.
      await fs.rm(unitPath,{force:true});
      if(!await invoke(['daemon-reload'])) return result;
      result.local='detached';
      if(result.remote==='absent') {
        result.complete=true;await save(statePath,result);
        await fs.rm(bindingPath,{force:true});
      } else await save(bindingPath,value);
      return result;
    } catch {result.complete=false;return result;}
    finally {
      if(locked) {
        // Preserve dispositions on all partial paths. Failure cannot report success.
        try {if(intent) await save(statePath,result);} catch {result.complete=false;result.local='publication-unknown';}
        try {await fs.rm(join(lock,'lifecycle-owner.json'),{force:true});await fs.rmdir(lock);} catch {result.complete=false;}
      }
    }
  };
  try {return await (dependencies.core?execute(dependencies.core):withRegistrationCore(execute));}
  catch {fail();}
}
