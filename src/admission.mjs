import fs from 'node:fs/promises';
import {join,dirname} from 'node:path';
import {userInfo} from 'node:os';
import {createHash} from 'node:crypto';
import {currentPin,verifyCurrentCore} from './current-core.mjs';
import {managedWorkflowPlan} from './workflow-inputs.mjs';
import {observeExecutionPool} from './execution-pool.mjs';
import {privatePath,ensurePrivateDirectory,readPrivate,writePrivate,sha} from './local-state.mjs';
const base=()=>join(userInfo().homedir,'.local/share/nibfin');
function contents(value,path){
  if(value?.type!=='file'||value.path!==path||value.encoding!=='base64'||typeof value.content!=='string'
    ||value.content.length>100000||!Number.isSafeInteger(value.size)||value.size<1||value.size>65536
    ||!/^[A-Za-z0-9+/=\r\n]+$/.test(value.content))throw Error('Admission content identity unavailable.');
  const encoded=value.content.replace(/[\r\n]/g,''),bytes=Buffer.from(encoded,'base64');
  if(bytes.length!==value.size||bytes.toString('base64')!==encoded
    ||createHash('sha1').update(`blob ${bytes.length}\0`).update(bytes).digest('hex')!==value.sha)throw Error('Admission Git blob identity differs.');
  return bytes;
}
export const policyDirectory=id=>{
  if(!/^[a-zA-Z0-9-]{1,80}$/.test(id))throw Error('Invalid admission instance.');return join(base(),'admission',id);
};
async function file(path,limit=1048576){
  for(let p=path;p!=='/';p=dirname(p))if((await fs.lstat(p)).isSymbolicLink())throw Error('Admission source link.');
  const s=await fs.lstat(path);if(!s.isFile()||s.nlink!==1||s.uid!==userInfo().uid||s.size>limit||s.mode&0o022)throw Error('Admission source unsafe.');
  return fs.readFile(path);
}
async function retainRuntime(context){
  const root=join(base(),'runtime',currentPin.coreSha256);await ensurePrivateDirectory(root);
  const nodes=join(root,'node_modules');
  try{await fs.lstat(nodes);}catch(e){if(e.code!=='ENOENT')throw e;await fs.cp(join(context.root,'node_modules'),nodes,{recursive:true,errorOnExist:true,force:false});}
  const compare=async(relative='')=>{
    const source=join(context.root,'node_modules',relative),target=join(nodes,relative),s=await fs.lstat(target);
    if(s.isSymbolicLink()||s.uid!==userInfo().uid)throw Error('Installed admission runtime is unsafe.');
    if(s.isDirectory()){
      await fs.chmod(target,0o700);const names=await fs.readdir(source);
      if(JSON.stringify(names.sort())!==JSON.stringify((await fs.readdir(target)).sort()))throw Error('Installed admission runtime inventory changed.');
      for(const n of names)await compare(join(relative,n));
    }else{
      if(!s.isFile()||s.nlink!==1||!(await fs.readFile(source)).equals(await fs.readFile(target)))throw Error('Installed admission runtime bytes changed.');
      await fs.chmod(target,relative.endsWith('.sh')?0o700:0o600);
    }
  };await compare();return join(nodes,'@nibfin/core');
}
/** Read-only installation adapter. Membership comes from the protected local
 * Owner installation, never from the public runtime file or a Job input. */
export async function observeAdmissionPlan(core,{state,provider,signal,repository,installation,workerSha256,runnerDirectory}={}){
  const get=async endpoint=>{
    const r=await provider.request({endpoint,target:state.registration.id,operation:'admission-install',correlation:'admission',signal});
    if(r.outcome!=='read')throw Error('Authenticated admission read unavailable.');return r.data;
  };
  const c=core.repositoryCoordinates(...repository.split('/'));
  if(c.key!==repository||repository.split('/').length!==2||state.registration.level==='organization'&&c.owner!==state.registration.organization)
    throw Error('Wrong admission repository.');
  const target=await get('/repos/'+repository);
  if(target.private!==false||target.visibility!=='public'||target.full_name?.toLowerCase()!==repository
    ||!/^[1-9][0-9]*$/.test(target.id??'')||!/^[1-9][0-9]*$/.test(target.owner?.id??'')
    ||state.registration.level==='repository'&&(target.id!==state.registration.id||target.id!==state.target.id
      ||target.owner.id!==state.target.ownerId||target.owner.type!==state.target.ownerType)
    ||state.registration.level==='organization'&&(target.owner.type!=='Organization'||target.owner.id!==state.registration.id))
    throw Error('Admission target identity/scope mismatch.');
  const commit=await get('/repos/'+repository+'/commits/main');
  if(!/^[a-f0-9]{40}$/.test(commit?.sha??''))throw Error('Admission immutable source commit unavailable.');
  const read=async path=>contents(await get('/repos/'+repository+'/contents/'+path+'?ref='+commit.sha),path);
  const ownerBytes=await read('.nibfin/runtime.json'),ownerPolicy=JSON.parse(ownerBytes);
  if(ownerPolicy.version!==1||ownerPolicy.package?.coreSha256!==currentPin.coreSha256
    ||!Array.isArray(ownerPolicy.allowedActorIds)||!ownerPolicy.allowedActorIds.length)throw Error('Owner installation/admission policy is incompatible.');
  let pool;
  if(installation?.executionPool!==undefined||ownerPolicy.executionPool!==undefined){
    if(installation?.version!==1||installation.repository!==repository||installation.repositoryId!==target.id
      ||installation.installation?.packageVersion!==currentPin.packageVersion
      ||core.canonicalJSON(installation.installation.pins)!==core.canonicalJSON(core.distributionIdentity().pins)
      ||installation.files?.['.nibfin/runtime.json']!==sha(ownerBytes)
      ||installation.executionPool===undefined)throw Error('Protected shared installation unavailable; public runtime data cannot select membership.');
    pool=await observeExecutionPool(core,{previous:installation.executionPool,repo:repository,provider,memberId:state.memberId,signal});
    if(pool.source.repository!==repository||pool.source.repositoryId!==target.id||pool.source.ownerId!==target.owner.id
      ||state.registration.level!=='repository'||core.canonicalJSON(pool)!==core.canonicalJSON(ownerPolicy.executionPool))
      throw Error('Shared admission requires the exact protected physical source and membership.');
  }
  const plan=managedWorkflowPlan(core,{executionPool:pool,repository}),expected=plan.files;
  const stages=pool?plan.deployments.map(d=>[d.stagePath,d.deliveryPath]):[['.github/workflows/nibfin-stage.yml','.github/workflows/nibfin-delivery.yml']];
  const files=new Map();
  for(const path of new Set(stages.flat().concat('.github/workflows/nibfin-rescue.yml'))){
    const bytes=await read(path),approved=expected.get(path);
    if(typeof approved!=='string'||!bytes.equals(Buffer.from(approved)))throw Error('Admission workflow source differs from immutable managed installation.');
    files.set(path,bytes);
  }
  const policies=[];
  for(const [path,dependency] of [...stages,['.github/workflows/nibfin-rescue.yml',null]]){
    for(const jobName of dependency?['stage / execute','stage / compute']:['rescue'])policies.push({version:1,repository,repositoryId:target.id,
      ownerId:target.owner.id,runnerId:state.runnerId,runnerName:state.runnerName,runnerDirectory,workerSha256,
      workflowPath:path,workflowSha256:sha(files.get(path)),jobName,allowedActorIds:ownerPolicy.allowedActorIds,
      ref:'refs/heads/main',registrationLevel:state.registration.level,
      ...(dependency?{workflowDependencies:[{path:dependency,sha256:sha(files.get(dependency))}]}:{})});
  }
  return {sourceCommit:commit.sha,policies:core.runnerJobPolicies({schemaVersion:1,policies})};
}
export async function prepareAdmission(core,context,{state,instance,provider,signal,configuration}={}){
  if(process.platform!=='linux'||process.arch!=='x64')throw Error('Admission requires the supported Linux process layout.');
  if(!context?.files)throw Error('Verified installed public admission package unavailable.');
  const worker=await file(join(instance.registration,'bin/Runner.Worker'));
  await file(join(instance.registration,'bin/Runner.Listener'));
  await fs.access('/usr/local/bin/node',fs.constants.X_OK);await fs.access('/usr/bin/gh',fs.constants.X_OK);await fs.access('/proc/self/stat');
  let repositories;
  if(state.registration.level==='repository')repositories=[state.target.owner+'/'+state.target.repository];
  else{
    const c=configuration??await readPrivate(join(userInfo().homedir,'.config/nibfin/admission.json'));
    if(!Array.isArray(c.repositories)||!c.repositories.length||c.repositories.length>8||new Set(c.repositories).size!==c.repositories.length)throw Error('Configure a bounded exact repository set for Organization admission.');
    repositories=c.repositories;
  }
  const policies=[],sourceCommits={};
  for(const repository of repositories){
    const installation=await readPrivate(join(base(),'installations',repository.replace('/','--'),'installation.json'),{missing:true});
    const observed=await observeAdmissionPlan(core,{state,provider,signal,repository,installation,workerSha256:sha(worker),runnerDirectory:instance.registration});
    policies.push(...observed.policies);sourceCommits[repository]=observed.sourceCommit;
  }
  const validated=core.runnerJobPolicies({schemaVersion:1,policies});
  const root=await retainRuntime(context),directory=policyDirectory(instance.id);await ensurePrivateDirectory(directory);
  const path=join(directory,'policy.json');
  const previous=await readPrivate(path,{missing:true});if(previous)throw Error('Existing admission policy requires reconciliation before registration.');
  await writePrivate(path,{schemaVersion:1,policies:validated},{exclusive:true});
  const hook=join(root,'host/runner-job-hook.sh');
  const record={version:1,core:currentPin.coreSha256,policy:sha(await fs.readFile(path)),hook,hookSha256:sha(await file(hook)),workerSha256:sha(worker),sourceCommits};
  await writePrivate(join(directory,'installation.json'),record,{exclusive:true});
  return {environment:{ACTIONS_RUNNER_HOOK_JOB_STARTED:hook,NIBFIN_RUNNER_JOB_POLICY:path},record};
}
export async function checkAdmission(instance,environment){
  const directory=policyDirectory(instance.id),record=await readPrivate(join(directory,'installation.json'));
  const local=JSON.parse(await file(join(instance.registration,'.runner')));
  if(local.disableUpdate!==true)throw Error('Runner automatic updates must remain disabled at admission activation.');
  if(record.version!==1||record.core!==currentPin.coreSha256||environment.ACTIONS_RUNNER_HOOK_JOB_STARTED!==record.hook
    ||environment.NIBFIN_RUNNER_JOB_POLICY!==join(directory,'policy.json')||sha(await file(record.hook))!==record.hookSha256
    ||sha(await file(join(instance.registration,'bin/Runner.Worker')))!==record.workerSha256)throw Error('Admission activation or source/binary identity unavailable.');
  const runtime=join(base(),'runtime',currentPin.coreSha256,'node_modules/@nibfin/core');
  if(record.hook!==join(runtime,'host/runner-job-hook.sh'))throw Error('Admission hook is outside the immutable managed runtime.');
  const {manifest}=await verifyCurrentCore();
  for(const item of manifest.components.core.files){const path=join(runtime,item.path);await privatePath(path);if(sha(await file(path))!==item.sha256)throw Error('Installed admission component source changed.');}
  await privatePath(environment.NIBFIN_RUNNER_JOB_POLICY);
  if(sha(await fs.readFile(environment.NIBFIN_RUNNER_JOB_POLICY))!==record.policy)throw Error('Admission policy identity changed.');
  return record;
}
export async function removeAdmission(instance){
  const directory=policyDirectory(instance.id);try{await privatePath(directory,{directory:true});}catch(e){if(e.code==='ENOENT')return;throw e;}
  // Called only after the existing lifecycle has proved service termination.
  for(const name of await fs.readdir(directory)){
    if(!/^(policy|installation|last-admission)\.json$/.test(name)&&!/^admission-[0-9]+-[a-f0-9-]+\.json$/.test(name))throw Error('Unrecognized admission residue retained.');
    await privatePath(join(directory,name));
  }
  await fs.rm(directory,{recursive:true});
}
