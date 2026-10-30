import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {withCurrentCore,currentPin,currentProducer} from './current-core.mjs';
import {githubObservation,observeProcess,processEnvironment} from './create.mjs';
import {managedWorkflowPlan} from './workflow-inputs.mjs';
import {nativeControlHost} from './native-host.mjs';
import {installNativeSource} from './native-source-install.mjs';
import {observeExecutionPool} from './execution-pool.mjs';
import {sha,readPrivate,writePrivate,ensurePrivateDirectory,localLock,privatePath} from './local-state.mjs';
const targets=new WeakMap();
const requiredLabels=[{name:'scope',color:'1d76db',description:'Original authorized scope'},{name:'shaped',color:'5319e7',description:'Published independently validated Shape'},{name:'delivered',color:'0e8a16',description:'Core verified completed delivery'}];
export const installationRoot=()=>join(userInfo().homedir,'.local/share/nibfin/installations');
export function installationOptions(args,{upgrade=false}={}){
  const v={};for(let i=0;i<args.length;i++){
    const key=args[i];if(Object.hasOwn(v,key))throw Error('Duplicate installation option.');
    if(['--check','--auto-register'].includes(key))v[key]=true;
    else if(['--repo','--execution-repo','--pool-repos','--runner-count','--image','--runtime-profile',...(upgrade?['--runner','--version']:[])].includes(key)&&args[i+1]&&!args[i+1].startsWith('--'))v[key]=args[++i];
    else throw Error('Use init --repo OWNER/REPO [--check] [--auto-register] [--pool-repos OWNER/ONE,OWNER/TWO --runner-count N], or upgrade --repo OWNER/REPO [--check] [--version VERSION].');
  }
  if(!v['--repo']||v['--runner'])throw Error('Select --repo OWNER/REPO; local Runner upgrade requires its separately reconciled registration.');
  if(upgrade&&v['--auto-register'])throw Error('Automatic registration policy changes use Owner init.');
  if(v['--execution-repo'])throw Error('Central execution repositories are no longer supported; use --pool-repos with --runner-count. Existing installations require preserved-work reconciliation.');
  if(upgrade&&(v['--pool-repos']||v['--runner-count']))throw Error('Pool membership transitions require reconciled installation; upgrade cannot replace them implicitly.');
  if(v['--runner-count']&&!v['--pool-repos'])throw Error('Supply --pool-repos with --runner-count.');
  let pool;
  if(v['--pool-repos']){
    const names=v['--pool-repos'].split(',');
    const count=v['--runner-count']??'2';
    if(!/^[1-9][0-9]*$/.test(count)||Number(count)>128)throw Error('Runner capacity must be an integer from 1 to 128.');
    if(names.length>128||new Set(names).size!==names.length||!names.includes(v['--repo'])
      ||names.some(n=>!/^[a-z0-9-]+\/[a-z0-9_.-]+$/.test(n)))throw Error('Select a finite unique canonical pool including the initialized repository.');
    pool={id:'member-default',capacity:Number(count),targets:names};
  }
  if(v['--image']&&!/^sha256:[a-f0-9]{64}$/.test(v['--image']))throw Error('Select a pinned --image sha256 digest.');
  if(v['--runtime-profile']&&!v['--image'])throw Error('Select --image with its reviewed --runtime-profile.');
  if((v['--image']||v['--runtime-profile'])&&(upgrade||!pool))throw Error('Native member runtime selection belongs to pooled init.');
  if(v['--version']&&![currentPin.packageVersion,currentProducer].includes(v['--version']))throw Error('Version is not in the reviewed immutable compatibility catalog.');
  return {repo:v['--repo'],check:!!v['--check'],automatic:v['--auto-register']===true?true:undefined,version:v['--version'],...(pool?{pool}:{}),...(v['--image']?{memberRuntime:{image:v['--image'],runtimeProfile:v['--runtime-profile']??'linux-amd64-codex-v1'}}:{})};
}
export function installationProvider(core,{transport,repo,write}){
  const prefix='/repos/'+repo;
  return core.createGithubProvider({transport:transport??core.localGhTransport(),attempts:1,
    authorize:r=>write===true&&r.endpoint.startsWith(prefix+'/')&&['PUT','POST','PATCH'].includes(r.method)
      &&(/\/contents\/(?:\.github\/workflows\/nibfin-[\w-]+\.yml|\.nibfin\/[\w./-]+)$/.test(r.endpoint)
      ||r.endpoint===prefix+'/labels'||r.endpoint===prefix+'/actions/variables'||r.endpoint.startsWith(prefix+'/actions/variables/')
      ||r.method==='POST'&&r.endpoint===prefix+'/pulls'||r.method==='PUT'&&new RegExp('^'+prefix+'/pulls/[1-9][0-9]*/merge$').test(r.endpoint))});
}
async function request(provider,repo,endpoint,{method='GET',body}={}){
  const r=await provider.request({method,endpoint,target:targets.get(provider),operation:'installation',correlation:'installation',...(body?{body}:{}),signal:AbortSignal.timeout(30000)});
  return r;
}
const prefix=repo=>'/repos/'+repo;
export async function contentObservation(provider,repo,path){
  const r=await request(provider,repo,prefix(repo)+'/contents/'+path);
  if(r.outcome!=='read')return {state:'unknown',diagnostic:r.diagnostic?.kind??'unavailable'};
  const v=r.data;
  if(v?.type!=='file'||v.encoding!=='base64'||!Number.isInteger(v.size)||v.size<0||v.size>1048576||typeof v.content!=='string'||typeof v.sha!=='string')return {state:'unknown'};
  const bytes=Buffer.from(v.content.replace(/\s/g,''),'base64');if(bytes.length!==v.size)return {state:'unknown'};
  return {state:'present',bytes,sha:v.sha};
}
// A 404 is absence only after a fresh authenticated repository administration
// observation, and only for this specific managed-file endpoint.
async function managedObservation(provider,repo,path,admin){
  const r=await request(provider,repo,prefix(repo)+'/contents/'+path);
  if(r.outcome==='read'){
    const v=r.data;if(v?.type==='file'&&v.encoding==='base64'&&typeof v.content==='string'&&Number.isInteger(v.size)&&v.size<=1048576){
      const bytes=Buffer.from(v.content.replace(/\s/g,''),'base64');if(bytes.length===v.size&&/^[a-f0-9]{40}$/.test(v.sha??''))return {state:'present',bytes,sha:v.sha};
    }return {state:'unknown'};
  }
  return admin&&r.diagnostic?.status===404?{state:'absent'}:{state:'unknown',diagnostic:r.diagnostic?.kind??'unavailable'};
}
export async function expectedInstallation(core,context,{automatic=false,allowedActorIds=[],executionPool,repo}={}){
  const source=core.createInstallation({version:1,expected:currentPin,profile:'source',observeCapability:async({version,sha256})=>({version,status:'verified',sha256})});
  const plan=await source.plan({commit:currentProducer,hostRepository:'taco3064/nibfin',hostCommit:currentProducer,repository:repo,
    ...(executionPool?{executionPool}:{})});
  if(plan.status!=='planned')throw Error('Packaged installation plan rejected the reviewed source.');
  const files=new Map(managedWorkflowPlan(core,{executionPool,repository:repo}).files);
  // Core owns the rendered workflow/Skill/security sources; CLI only projects
  // those exact immutable installation inputs into its managed locations.
  for(const path of ['skills/shape/SKILL.md','skills/deliver/SKILL.md','skills/accept/SKILL.md','skills/rescue/SKILL.md'])files.set('.nibfin/'+path,context.files.get(path).toString());
  files.set('.nibfin/runtime.json',JSON.stringify({version:1,producer:currentProducer,package:currentPin,pins:context.identity.pins,automaticRegistration:automatic,allowedActorIds,
    ...(executionPool?{executionPool:core.validateExecutionPool(executionPool)}:{})},null,2)+'\n');
  return {plan,files};
}
export async function inspectInstallation(core,context,{repo,automatic,provider,root,host,memberId,pool}={}){
  const coordinates=core.repositoryCoordinates(...repo.split('/'));if(coordinates.key!==repo||repo.split('/').length!==2)throw Error('Use a canonical public OWNER/REPO.');
  const target=await request(provider,repo,prefix(repo));
  const diagnostics=[];
  if(target.outcome!=='read'||target.data?.private!==false||target.data?.visibility!=='public'||target.data?.full_name?.toLowerCase()!==repo)
    return {status:'blocked',diagnostics:[{capability:'repository',code:'identity-or-public-access-unavailable'}]};
  if(!/^[1-9][0-9]*$/.test(target.data.id??''))return {status:'blocked',diagnostics:[{capability:'repository',code:'invalid-stable-identity'}]};
  targets.set(provider,target.data.id);
  const administration=target.data.permissions?.admin===true;
  if(!administration)diagnostics.push({capability:'permission',code:'repository-administration-required-for-installation'});
  const previous=root?await readPrivate(join(root,'installation.json'),{missing:true}):null;
  if(previous&&!previous.executionPool&&pool)return {status:'blocked',diagnostics:[{capability:'executionPool',code:'protected-pool-transition-required'}]};
  let executionPool;
  try{executionPool=await observeExecutionPool(core,{selection:pool,previous:previous?.executionPool,repo,provider,memberId,signal:AbortSignal.timeout(30000)});}
  catch(error){return {status:'blocked',diagnostics:[{capability:'executionPool',code:error.code??'pool-observation-unavailable'}]};}
  const remotePolicy=await managedObservation(provider,repo,'.nibfin/runtime.json',administration);
  let actors,remoteSettings;
  try{remoteSettings=JSON.parse(remotePolicy.bytes);actors=remoteSettings.allowedActorIds;}catch{}
  automatic=automatic??previous?.automaticRegistration??(typeof remoteSettings?.automaticRegistration==='boolean'?remoteSettings.automaticRegistration:false);
  if(!Array.isArray(actors)||!actors.length||actors.length>32||actors.some(v=>!/^([1-9][0-9]*)$/.test(v))||new Set(actors).size!==actors.length)actors=[memberId].filter(v=>/^[1-9][0-9]*$/.test(v));
  const desired=await expectedInstallation(core,context,{automatic,allowedActorIds:actors,executionPool,repo});
  const labels=await request(provider,repo,prefix(repo)+'/labels?per_page=100');
  const missingLabels=labels.outcome==='read'&&Array.isArray(labels.data)?requiredLabels.filter(l=>!labels.data.some(v=>v.name===l.name)):null;
  if(missingLabels===null)diagnostics.push({capability:'labels',code:'inventory-unavailable'});
  else for(const l of missingLabels)diagnostics.push({capability:'labels',code:'missing',name:l.name});
  const observations=new Map();
  for(const [path,body] of desired.files){
    const o=await managedObservation(provider,repo,path,administration);observations.set(path,o);
    if(o.state!=='present'||!o.bytes.equals(Buffer.from(body)))diagnostics.push({capability:path.includes('skills/')?'skills':path.includes('rescue')?'recovery':'workflow',
      code:o.state==='unknown'?'observation-unavailable':o.state==='absent'?'missing':'modified',path});
  }
  // Host and service readiness are measured separately from source presence.
  const hostReport=host?await host.check({core,context,repo,executionPool,readOnly:true}):{status:'blocked',diagnostics:[{capability:'recovery',code:'authorized-control-host-notifier-unconfigured'}]};
  if(hostReport?.status!=='ready')diagnostics.push(...(hostReport?.diagnostics??[{capability:'recovery',code:'notifier-capability-unknown'}]));
  const check=core.createInstallation({version:1,expected:currentPin,profile:'source',observeCapability:async({capability,version,sha256})=>{
    const present=capability==='source'||capability==='runtime'?true:capability==='recovery'?hostReport?.status==='ready'&&!diagnostics.some(d=>d.capability==='recovery'):
      capability==='jobAdmission'?context.files.has('host/runner-job-hook.sh')&&context.files.has('host/runner-job-hook.mjs'):
        !diagnostics.some(d=>d.capability===capability);
    return {version,status:present?'verified':'unknown',sha256};
  }});
  const source=await check.check();
  return {status:diagnostics.length||source.status!=='ready'?'blocked':'ready',scope:'repository-installation',repositoryId:target.data.id,
    administration,diagnostics,desired,observations,previous,hostReport,missingLabels,executionPool};
}
export async function initializeRepository(args,dependencies={}){
  const selected=installationOptions(args);
  if(!dependencies.core)return withCurrentCore((core,context)=>initializeRepository(args,{...dependencies,core,context}));
  const host=dependencies.host??nativeControlHost();
  const {core,context}=dependencies,provider=dependencies.provider??installationProvider(core,{transport:dependencies.transport,repo:selected.repo,write:!selected.check});
  const memberId=dependencies.memberId??await githubObservation({run:dependencies.run??observeProcess,environment:processEnvironment('/nonexistent'),account:userInfo()});
  targets.set(provider,memberId);
  const root=join(dependencies.root??installationRoot(),selected.repo.replace('/','--'));
  const inspect=()=>inspectInstallation(core,context,{...selected,provider,root,host,memberId});
  if(selected.check){const r=await inspect();return {status:r.status,scope:r.scope,diagnostics:r.diagnostics};}
  await ensurePrivateDirectory(root);
  return localLock(root,'installation',async()=>{
    const report=await inspect();if(!report.administration)return {status:'blocked',diagnostics:report.diagnostics};
    if(!report.desired)return report;
    if(report.diagnostics.some(d=>d.capability==='executionPool'))return {status:'blocked',diagnostics:report.diagnostics};
    let nativePrepared=false;
    if(host.prepare){
      const prepared=await host.prepare({core,context,repo:selected.repo,executionPool:report.executionPool,memberId,...(selected.memberRuntime?{memberRuntime:selected.memberRuntime}:{})});
      nativePrepared=prepared?.status==='prepared'&&report.executionPool?.version===2;
      if(prepared?.status!=='ready'&&!nativePrepared)return {status:'blocked',phase:prepared?.status??'unknown',diagnostics:prepared?.diagnostics??[{capability:'host',code:'native-preparation-unconfirmed'}]};
    }
    const nextInstallation={packageVersion:currentPin.packageVersion,pins:context.identity.pins};
    if(report.previous&&JSON.stringify(report.previous.installation)!==JSON.stringify(nextInstallation))return {status:'blocked',diagnostics:[{capability:'installation',code:'runtime-transition-required',action:'Use the existing upgrade boundary after reconciling active work, holds and unknown writes.'}]};
    if(report.missingLabels===null)return {status:'blocked',diagnostics:report.diagnostics};
    const journalPath=join(root,'intent.json'),oldIntent=await readPrivate(journalPath,{missing:true});
    const labelMatches=(value,label)=>value.name===label.name&&value.color?.toLowerCase()===label.color&&value.description===label.description;
    if(oldIntent&&oldIntent.status!=='completed'){
      if(oldIntent.version!==1||oldIntent.repository!==selected.repo||oldIntent.repositoryId!==report.repositoryId||oldIntent.kind!=='label'&&(!report.desired.files.has(oldIntent.path)||oldIntent.next!==sha(report.desired.files.get(oldIntent.path))))return {status:'blocked',diagnostics:[{capability:'installation',code:'original-intent-identity-mismatch'}]};
      let confirmed=false;
      if(oldIntent.kind==='label'){
        const desired=requiredLabels.find(l=>l.name===oldIntent.name);
        const labels=await request(provider,selected.repo,prefix(selected.repo)+'/labels?per_page=100');
        confirmed=desired&&oldIntent.next===sha(JSON.stringify(desired))&&labels.outcome==='read'&&Array.isArray(labels.data)&&labels.data.some(v=>labelMatches(v,desired));
        if(confirmed)report.missingLabels=report.missingLabels.filter(l=>l.name!==oldIntent.name);
      }else{
        const observed=await managedObservation(provider,selected.repo,oldIntent.path,true);
        confirmed=observed.state==='present'&&sha(observed.bytes)===oldIntent.next;
      }
      if(confirmed)await writePrivate(journalPath,{...oldIntent,status:'completed'});
      else return {status:'unknown',diagnostics:[{capability:'installation',code:'original-write-reconciliation-required',...(oldIntent.path?{path:oldIntent.path}:{name:oldIntent.name})}]};
    }
    for(const label of report.missingLabels){
      const intent={version:1,kind:'label',repository:selected.repo,repositoryId:report.repositoryId,name:label.name,next:sha(JSON.stringify(label)),status:'pending'};
      await writePrivate(journalPath,intent);
      const submitted=await request(provider,selected.repo,prefix(selected.repo)+'/labels',{method:'POST',body:label});
      const after=await request(provider,selected.repo,prefix(selected.repo)+'/labels?per_page=100');
      if([401,403].includes(submitted.diagnostic?.status)){await writePrivate(journalPath,{...intent,status:'denied'});return {status:'blocked',diagnostics:[{capability:'permission',code:'label-installation-denied',name:label.name}]};}
      if(after.outcome!=='read'||!Array.isArray(after.data)||!after.data.some(v=>labelMatches(v,label)))return {status:'unknown',diagnostics:[{capability:'labels',code:'write-readback-unconfirmed',name:label.name}]};
      await writePrivate(journalPath,{...intent,status:'completed'});
    }
    if(nativePrepared){
      const source=await installNativeSource(core,{root,repository:selected.repo,repositoryId:report.repositoryId,desired:report.desired,
        observations:report.observations,previous:report.previous,provider,signal:AbortSignal.timeout(300000)});
      if(source.status!=='installed')return {status:source.status,phase:'protected-source-installation',pr:source.pr,
        diagnostics:[{capability:'installation',code:source.reason}]};
    }else for(const [path,body] of report.desired.files){
      const o=report.observations.get(path);if(o.state==='unknown')return {status:'blocked',diagnostics:[{capability:'workflow',code:'observation-unavailable',path}]};
      if(o.state==='present'&&o.bytes.equals(Buffer.from(body)))continue;
      if(o.state==='present'&&report.previous?.files?.[path]!==sha(o.bytes))return {status:'blocked',diagnostics:[{capability:'workflow',code:'user-modification-preserved',path,action:'Reconcile the managed file with the reviewed package; no force overwrite is performed.'}]};
      const intent={version:1,repository:selected.repo,repositoryId:report.repositoryId,path,previous:o.sha??null,next:sha(body),status:'pending'};
      await writePrivate(journalPath,intent);
      const submitted=await request(provider,selected.repo,prefix(selected.repo)+'/contents/'+path,{method:'PUT',body:{message:'Install pinned Nibfin managed source',content:Buffer.from(body).toString('base64'),...(o.sha?{sha:o.sha}:{})}});
      const after=await managedObservation(provider,selected.repo,path,true);
      if([401,403].includes(submitted.diagnostic?.status)){await writePrivate(journalPath,{...intent,status:'denied'});return {status:'blocked',diagnostics:[{capability:'permission',code:'managed-source-installation-denied',path}]};}
      if(after.state!=='present'||!after.bytes.equals(Buffer.from(body)))return {status:'unknown',diagnostics:[{capability:'installation',code:'write-readback-unconfirmed',path}]};
      await writePrivate(journalPath,{...intent,status:'completed'});
    }
    // Local source records are not a model login, Owner Grant or recovery proof.
    await writePrivate(join(root,'installation.json'),{version:1,repository:selected.repo,repositoryId:report.repositoryId,
      installation:{packageVersion:currentPin.packageVersion,pins:context.identity.pins},files:Object.fromEntries([...report.desired.files].map(([p,b])=>[p,sha(b)])),automaticRegistration:JSON.parse(report.desired.files.get('.nibfin/runtime.json')).automaticRegistration,
      ...(report.executionPool?{executionPool:report.executionPool}:{})});
    await host.install({core,context,repo:selected.repo,executionPool:report.executionPool});
    const final=await inspect();return {status:final.status,scope:final.scope,diagnostics:final.diagnostics};
  });
}
