import fs from 'node:fs/promises';import{join}from'node:path';
import{nativeOwnerSigner}from'./native-owner-policy.mjs';import{readNativeMember}from'./native-member.mjs';
import{createNativeCycle,openNativeCycle,nativeCycleDirectory}from'./native-delivery-store.mjs';import{currentPin,currentProducer}from'./current-core.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-cycle:'+code);};
const operations=core=>[...new Set([...core.coreStages,'readiness','publish-shape','commit-pr','observe-ci','verify-deployment','close','mark-delivered','issue-body','label','comment','reopen','dispatch'])];
const snapshot=(core,source,revision,body)=>({source,revision,body,hash:core.bodyDigest(body)});
export async function prepareNativeCycle(core,{root,configuration,repository,number,provider,context,signal}={}){
 need(core.distributionAPI?.nativeHost===1,'compatible-native-core-required');
 const signer=await nativeOwnerSigner(core,{root}),owner=signer.policy,target=configuration.targets.find(t=>t.repository===repository);
 need(target&&owner.targets.some(t=>core.canonicalJSON(t)===core.canonicalJSON(target)),'installed-target');
 const get=async endpoint=>{const r=await provider.request({endpoint,target:target.repositoryId,operation:'native-cycle-read',correlation:'prepare-cycle',signal});
  need(r.outcome==='read','observation-unavailable');return r.data;};
 const repo=await get('/repos/'+repository),account=await get('/user'),issue=await get('/repos/'+repository+'/issues/'+number);
 need(repo.id===target.repositoryId&&repo.full_name?.toLowerCase()===repository&&repo.owner.id===target.ownerId&&repo.owner.type===target.ownerType
  &&repo.private===false&&repo.visibility==='public'&&repo.default_branch==='main'&&repo.permissions?.admin===true
  &&account.id===owner.memberId&&account.login.toLowerCase()===owner.memberLogin.toLowerCase(),'execution-account');
 need(['open','closed'].includes(issue.state)&&!issue.pull_request&&String(issue.number)===number&&typeof issue.body==='string'
  &&issue.assignees.some(a=>a.id===account.id)&&issue.labels.some(l=>l.name==='scope'),'eligible-issue');
 const selected={repository,repositoryId:target.repositoryId,issueId:issue.node_id,databaseId:issue.id,number};
 const document=core.parseRequirementsDocument(issue.body),identity=core.distributionIdentity();
 try{await fs.lstat(nativeCycleDirectory(root,selected));const existing=await openNativeCycle(core,{root,target:selected}),state=await existing.read();
  need(state.scopeHash===core.bodyDigest(document.scope)&&state.core===currentPin.coreSha256,'original-cycle-reconciliation-required');
  need(issue.state==='open'||Boolean(state.receipts.closure),'original-closure-required');return existing;
 }catch(e){if(e.code!=='ENOENT')throw e;}
 need(issue.state==='open'&&document.format==='raw-scope'&&!issue.labels.some(l=>l.name==='shaped'),'original-work-handover-required');
 const base=(await get('/repos/'+repository+'/git/ref/heads/main')).object;
 need(base.type==='commit'&&/^[a-f0-9]{40}$/.test(base.sha),'base');
 const tree=await get('/repos/'+repository+'/git/trees/'+base.sha);
 need(tree.truncated===false&&Array.isArray(tree.tree)&&tree.tree.length>0,'source-tree');
 const paths=tree.tree.map(entry=>entry.path);
 need(paths.every(path=>/^[A-Za-z0-9_.-]+$/.test(path)&&!['.','..','.git'].includes(path)),'supported-source-paths');
 const checks=await get('/repos/'+repository+'/branches/main/protection/required_status_checks');
 const requiredChecks=[...new Set([...(checks.contexts??[]),...(checks.checks??[]).map(c=>c.context)])];
 need(requiredChecks.length>0&&requiredChecks.every(x=>typeof x==='string'&&x.length>0),'protected-ci-required');
 const runs=await provider.collection({endpoint:'/repos/'+repository+'/commits/'+base.sha+'/check-runs?per_page=100',envelope:'check_runs',
  target:target.repositoryId,operation:'native-cycle-read',correlation:'prepare-cycle',signal,
  allowPage:path=>path.split('?')[0]==='/repos/'+repository+'/commits/'+base.sha+'/check-runs'});
 need(runs.outcome==='complete','ci-observation');
 const trustedAppIds=[...new Set(runs.data.filter(c=>requiredChecks.includes(c.name)).map(c=>c.app?.id))];
 need(trustedAppIds.length>0&&trustedAppIds.every(id=>typeof id==='string'&&/^[1-9][0-9]*$/.test(id)),'ci-app-identity');
 const ci=await get('/repos/'+repository+'/contents/.github/workflows/ci.yml?ref='+base.sha);
 need(ci.encoding==='base64'&&typeof ci.content==='string','ci-source');
 const cycle='native-'+core.contentIdentity({target:selected,scope:document.scope,core:currentPin.coreSha256}).slice(0,32);
 const observation=await core.observeRequirementAuthority({source:{kind:'github-issue',target:selected},provider,cycle,approvalId:owner.keyId,signal});
 need(observation.outcome==='observed'&&observation.authority.components[0].body===document.scope,'fresh-issue-authority');
 const member=await readNativeMember(core,{root,memberId:account.id}),runtime=core.contentIdentity({agent:member.agent,image:member.image,runtimeProfile:member.runtimeProfile});
 const policy={core:currentPin.coreSha256,runtime,agent:'codex-cli-0.163.0',skills:Object.fromEntries(['shape','deliver','accept'].map(skill=>[skill,
  {identity:skill,mode:'default',instructions:snapshot(core,'https://github.com/taco3064/nibfin/blob/'+currentProducer+'/skills/'+skill+'/SKILL.md',currentProducer,context.files.get('skills/'+skill+'/SKILL.md').toString())}])),
  ci:{requiredChecks,trustedAppIds,workflow:snapshot(core,'https://github.com/'+repository+'/blob/'+base.sha+'/.github/workflows/ci.yml',base.sha,Buffer.from(ci.content,'base64').toString('utf8'))},
  merge:{baseBranch:'main',method:repo.allow_rebase_merge?'rebase':repo.allow_squash_merge?'squash':'merge',protected:true},maxParallel:configuration.capacity,
  repair:{mode:'bounded',maxRepairs:3}};
 const repositoryIdentity={id:target.repositoryId,name:repository,visibility:'public'},url='https://github.com/'+repository+'/issues/'+number;
 const sourceScope=snapshot(core,url,core.bodyDigest(document.scope),document.scope);
 const work=[{repository:repositoryIdentity,issueId:selected.issueId,number,scope:sourceScope,currentBody:sourceScope,shape:null,
  observation:snapshot(core,url,String(issue.updated_at),core.canonicalJSON({id:issue.id,node_id:issue.node_id,body:issue.body,updated_at:issue.updated_at}))}];
 const payload={version:1,recordId:cycle,revision:'1',revocationId:owner.keyId,validFrom:Date.now()-1000,expiresAt:Date.now()+604800000,
  authority:core.authoritySnapshot(observation.authority),work,permissions:[{repository:repositoryIdentity,paths,operations:operations(core)}],policy};
 core.prepareGrant(payload);const grant=signer.sign(payload);
 return createNativeCycle(core,{root,target:selected,value:{cycle,core:currentPin.coreSha256,installation:identity.pins,
  createdAt:Date.now(),title:issue.title,scopeBody:document.scope,scopeHash:core.bodyDigest(document.scope),shapeBody:null,authority:observation.authority,grant,policy,
  member:{id:account.id,login:account.login},memberRuntime:{image:member.image,runtimeProfile:member.runtimeProfile,socket:'/run/user/'+process.getuid()+'/docker.sock'},
  ownerKey:{id:owner.keyId,publicKey:owner.publicKey},paths,base:base.sha,
  receipts:{},expectations:{},stageInput:null,candidate:null,workspace:null,head:null,ci:null,completion:null,
  attempts:{},dispatch:null,observations:{},closed:false}});
}
