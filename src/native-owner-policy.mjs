import fs from 'node:fs/promises';import{join}from'node:path';import{generateKeyPairSync,createPublicKey}from'node:crypto';
import{readPrivate,writePrivate,privatePath,sha}from'./local-state.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-owner:'+code);};
export async function prepareNativeOwnerPolicy(core,{root,configuration,provider}={}){
 const selected=core.validateExecutionPool(configuration),path=join(root,'owner-policy.json');
 const get=async(endpoint,target)=>{const r=await provider.request({endpoint,target,operation:'native-owner-policy',correlation:'installation'});
  need(r.outcome==='read','authenticated-observation');return r.data;};
 const member=await get('/user',selected.targets[0].repositoryId);need(/^[1-9][0-9]*$/.test(member.id),'member');
 const existing=await readPrivate(path,{missing:true});if(existing){
  const {identity,...body}=existing;need(core.contentIdentity(body)===identity&&existing.memberId===member.id
   &&core.canonicalJSON(existing.targets)===core.canonicalJSON(selected.targets),'preserve-owner-policy');return existing;
 }
 for(const target of selected.targets){const repo=await get('/repos/'+target.repository,target.repositoryId);
  need(repo.id===target.repositoryId&&repo.full_name?.toLowerCase()===target.repository&&repo.private===false&&repo.visibility==='public'
   &&repo.owner.id===target.ownerId&&repo.owner.type===target.ownerType&&repo.permissions?.admin===true,'owner-installation-permission');}
 const keyPath=join(root,'owner-signing-key.pem');let privateKey;
 try{await privatePath(keyPath);privateKey=await fs.readFile(keyPath,'utf8');}
 catch(e){if(e.code!=='ENOENT')throw e;const key=generateKeyPairSync('ed25519').privateKey;privateKey=key.export({format:'pem',type:'pkcs8'});
  const file=await fs.open(keyPath,'wx',0o600);try{await file.writeFile(privateKey);await file.sync();}finally{await file.close();}}
 need(createPublicKey(privateKey).asymmetricKeyType==='ed25519','signing-key-type');
 const publicKey=createPublicKey(privateKey).export({format:'pem',type:'spki'}),keyId='native-owner-'+sha(publicKey).slice(0,24);
 const body={version:1,memberId:member.id,memberLogin:member.login,keyId,publicKey,targets:selected.targets,projects:{}};
 await writePrivate(path,{...body,identity:core.contentIdentity(body)},{exclusive:true});return readNativeOwnerPolicy(core,{root});
}
export async function readNativeOwnerPolicy(core,{root}={}){
 const value=await readPrivate(join(root,'owner-policy.json')),{identity,...body}=value;
 need(value.version===1&&core.contentIdentity(body)===identity&&Array.isArray(value.targets)&&value.projects&&value.publicKey
  &&value.keyId==='native-owner-'+sha(value.publicKey).slice(0,24),'policy-integrity');
 return value;
}
export async function nativeOwnerSigner(core,{root}={}){
 const policy=await readNativeOwnerPolicy(core,{root}),path=join(root,'owner-signing-key.pem');await privatePath(path);
 const privateKey=await fs.readFile(path,'utf8');need(createPublicKey(privateKey).export({format:'pem',type:'spki'})===policy.publicKey,'signing-identity');
 return {policy,sign:payload=>core.signGrant(payload,{keyId:policy.keyId,privateKey})};
}
