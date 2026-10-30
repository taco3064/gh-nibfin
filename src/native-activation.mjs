import{join,dirname}from'node:path';import{readPrivate,writePrivate,sha}from'./local-state.mjs';
import{openFileHostStore}from'./host-store.mjs';import{readNativeMember}from'./native-member.mjs';
import{readNativeOwnerPolicy,nativeOwnerSigner}from'./native-owner-policy.mjs';import{nativePoolStore}from'./native-pool-store.mjs';
import{managedWorkflowPlan}from'./workflow-inputs.mjs';import{currentPin}from'./current-core.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-activation:'+code);};

export async function nativeActivationStatus(core,{root}={}){
 const activation=await readPrivate(join(root,'activation.json'),{missing:true});
 if(!activation)return {status:'pending',reason:'native-project-installation-and-activation-pending'};
 const{identity,...body}=activation,installation=await readPrivate(join(root,'native-installation.json'));
 need(core.contentIdentity(body)===identity&&activation.version===1&&activation.installation===core.contentIdentity(installation),'identity');
 const store=await openFileHostStore(core,{directory:join(root,'state')});
 try{const value=await store.read();return activation.phase==='active'&&value.work.restrictionsChecked&&value.work.paused===false
  &&value.work.hold===null&&value.work.unknownWrite===false?{status:'ready',identity}:{status:'paused',reason:'native-owner-restriction'};}
 finally{await store.close();}
}

// Source presence is verified separately from service liveness. Initial Owner
// activation requires all finite project installations; a later init never
// clears an existing Owner pause or an unresolved execution.
export async function activateNativeHost(core,{root,configuration,provider,signal}={}){
 const previous=await readPrivate(join(root,'activation.json'),{missing:true});
 if(previous?.phase==='active')return nativeActivationStatus(core,{root});
 if(previous){const{identity,...body}=previous;need(previous.version===1&&previous.phase==='prepared'&&core.contentIdentity(body)===identity,'original-activation');}
 const owner=await readNativeOwnerPolicy(core,{root});await nativeOwnerSigner(core,{root});await readNativeMember(core,{root,memberId:owner.memberId});
 const installation=await readPrivate(join(root,'native-installation.json')),projects=[];
 need(core.canonicalJSON(configuration)===core.canonicalJSON(installation.executionPool),'pool');
 const get=async(target,endpoint)=>{const r=await provider.request({endpoint,target:target.repositoryId,operation:'native-activation',correlation:'owner-init',signal});
  need(r.outcome==='read','observation');return r.data;};
 const account=await get(configuration.targets[0],'/user');need(account.id===owner.memberId,'member');
 for(const target of configuration.targets){
  const local=await readPrivate(join(dirname(root),'installations',target.repository.replace('/','--'),'installation.json'),{missing:true});
  if(!local)return {status:'pending',reason:'native-project-source-pending',repository:target.repository};
  need(local.version===1&&local.repositoryId===target.repositoryId&&local.repository===target.repository
   &&core.canonicalJSON(local.installation.pins)===core.canonicalJSON(core.distributionIdentity().pins)
   &&local.installation.packageVersion===currentPin.packageVersion&&core.canonicalJSON(local.executionPool)===core.canonicalJSON(configuration),'project-installation');
  const expected=new Set([...managedWorkflowPlan(core,{executionPool:configuration,repository:target.repository}).files.keys(),'.nibfin/runtime.json',
   ...['shape','deliver','accept','rescue'].map(role=>'.nibfin/skills/'+role+'/SKILL.md')]);
  need(Object.keys(local.files).length===expected.size&&Object.keys(local.files).every(p=>expected.has(p)),'managed-source-set');
  const base='/repos/'+target.repository,repo=await get(target,base),head=(await get(target,base+'/git/ref/heads/main')).object.sha;
  need(repo.id===target.repositoryId&&repo.private===false&&repo.visibility==='public'&&repo.permissions?.admin===true
   &&repo.owner.id===target.ownerId&&repo.owner.type===target.ownerType&&/^[a-f0-9]{40}$/.test(head),'project');
  for(const[path,hash]of Object.entries(local.files)){
   const file=await get(target,base+'/contents/'+path+'?ref='+head);need(file.type==='file'&&file.path===path&&file.encoding==='base64'
    &&typeof file.content==='string'&&file.content.length<=262144,'source');
   const bytes=Buffer.from(file.content,'base64');need(sha(bytes)===hash,'source-bytes');
   if(path==='.nibfin/runtime.json'){const policy=JSON.parse(bytes);need(policy.allowedActorIds?.includes(owner.memberId)
    &&policy.package.coreSha256===currentPin.coreSha256&&core.canonicalJSON(policy.executionPool)===core.canonicalJSON(configuration),'project-policy');}
  }
  projects.push({repository:target.repository,repositoryId:target.repositoryId,commit:head,files:local.files});
 }
 const pool=await nativePoolStore(core,{root,configuration}).status();need(pool.occupied===0&&pool.pending===0,'pool-busy');
 const store=await openFileHostStore(core,{directory:join(root,'state')});
 try{
  const before=await store.read(),work=before.work;
  need(!previous||work.paused===false,'prepared-activation-reconciliation-required');
  need(work.restrictionsChecked&&work.hold===null&&!work.unknownWrite&&!work.active&&work.terminated&&work.reconciled
   &&work.pending.length===0&&work.conversations.length===0&&work.receipts.length===0,'original-work-reconciliation-required');
  const body={version:1,phase:'prepared',installation:core.contentIdentity(installation),memberId:owner.memberId,projects};
  if(previous)need(previous.installation===body.installation&&core.canonicalJSON(previous.projects)===core.canonicalJSON(projects),'original-activation');
  await writePrivate(join(root,'activation.json'),{...body,identity:core.contentIdentity(body)});
  if(work.paused)await store.updateWork({expected:before.identity,work:{...work,paused:false}});
  const active={...body,phase:'active'};await writePrivate(join(root,'activation.json'),{...active,identity:core.contentIdentity(active)});
 }finally{await store.close();}
 return nativeActivationStatus(core,{root});
}
