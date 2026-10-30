import fs from 'node:fs/promises';
import {join,dirname} from 'node:path';
import {userInfo} from 'node:os';
import {randomUUID} from 'node:crypto';
import {fileURLToPath} from 'node:url';
import {currentPin,currentProducer} from './current-core.mjs';
import {nativeHostRoot} from './managed-upgrade.mjs';
import {maintenanceLock} from './upgrade.mjs';
import {privatePath,readPrivate,writePrivate,ensurePrivateDirectory,sha} from './local-state.mjs';
import {prepareRunnerDistribution,verifyRunnerDistribution,runnerDistribution} from './runner-distribution.mjs';
import {nativeControlService} from './native-control-service.mjs';

const need=(ok,code)=>{if(!ok)throw Error('native-host:'+code);};
const absent=async path=>{try{await fs.lstat(path);return false;}catch(e){if(e.code==='ENOENT')return true;throw e;}};
const clientRoot=dirname(fileURLToPath(import.meta.url));
const remaining=()=>[
 {capability:'runner',code:'native-runner-registrations-pending'},
 {capability:'handover',code:'retained-original-work-reconciliation-required'}
];

// Trusted package inputs only. Public configuration cannot select another module,
// executable or directory. Every retained byte is checked before reuse.
async function runtimeInputs(context){
 const files=new Map();
 const collect=async(root,prefix,relative='')=>{
  for(const entry of await fs.readdir(join(root,relative),{withFileTypes:true})){
   const name=relative?relative+'/'+entry.name:entry.name,path=join(root,name),s=await fs.lstat(path);
   need(!s.isSymbolicLink()&&(s.isDirectory()||s.isFile()),'package-source-type');
   if(s.isDirectory())await collect(root,prefix,name);
   else files.set(prefix+'/'+name,await fs.readFile(path));
  }
 };
 // npm's generated lock contains the temporary installation path. It is not
 // runtime authority and must not make an unchanged package look different.
 await collect(join(context.root,'node_modules/@nibfin/core'),'node_modules/@nibfin/core');
 await collect(join(context.root,'node_modules/jose'),'node_modules/jose');
 await collect(clientRoot,'client');
 await collect(join(clientRoot,'../docs/handoffs'),'docs/handoffs');
 need(files.has('node_modules/@nibfin/core/src/index.mjs')&&files.has('client/native-host.mjs'),'package-source-incomplete');
 return files;
}
async function verifyRuntime(root,files){
 await privatePath(root,{directory:true});const seen=new Set();
 const walk=async(relative='')=>{
  for(const entry of await fs.readdir(join(root,relative),{withFileTypes:true})){
   const name=relative?relative+'/'+entry.name:entry.name,path=join(root,name),s=await fs.lstat(path);
   need(s.uid===userInfo().uid&&!s.isSymbolicLink(),'runtime-owner');
   if(s.isDirectory()){need((s.mode&0o077)===0&&[...files.keys()].some(n=>n.startsWith(name+'/')),'runtime-directory');await walk(name);}
   else{need(s.isFile()&&s.nlink===1&&(s.mode&0o777)===0o600&&files.has(name),'runtime-file');
    need((await fs.readFile(path)).equals(files.get(name)),'runtime-bytes');seen.add(name);}
  }
 };await walk();need(seen.size===files.size,'runtime-inventory');
}
async function publishRuntime(root,files){
 const target=join(root,'runtime');if(!await absent(target)){await verifyRuntime(target,files);return;}
 const staging=join(root,'.runtime-staging-'+randomUUID());await fs.mkdir(staging,{mode:0o700});
 try{
  for(const [path,bytes]of files){await fs.mkdir(dirname(join(staging,path)),{recursive:true,mode:0o700});await fs.writeFile(join(staging,path),bytes,{flag:'wx',mode:0o600});}
  await verifyRuntime(staging,files);need(await absent(target),'runtime-publication-conflict');await fs.rename(staging,target);
 }finally{await fs.rm(staging,{recursive:true,force:true});}
}
async function stageRunner(root,name,{prepare,verify,copyFrom}){
 const target=join(root,name);
 if(!await absent(target)){await verify(target);return;}
 const staging=join(root,'.'+name+'-staging-'+randomUUID());await fs.mkdir(staging,{mode:0o700});
 try{
  if(copyFrom){await verify(copyFrom);await prepare(staging,{archive:join(copyFrom,runnerDistribution.archive)});}
  else await prepare(staging);
  // Acquisition does not configure or execute Runner binaries. Credentials or
  // registration residue must never be copied to another physical Runner.
  for(const name of ['.runner','.credentials','.credentials_rsaparams'])need(await absent(join(staging,name)),'unexpected-runner-registration');
  need(await absent(target),'runner-publication-conflict');await fs.rename(staging,target);
 }finally{await fs.rm(staging,{recursive:true,force:true});}
}

/** Default first-party onboarding. Preparation installs the maintained control
 * process; it has no GitHub writes, model login or Runner registration effects.
 * A running process and staged package are explicitly not delivery readiness.
 * Existing work and earlier installations are never reset to make init pass. */
export function nativeControlHost({root=nativeHostRoot(),prepare=prepareRunnerDistribution,verify=verifyRunnerDistribution,service}={}){
 const control=service??nativeControlService({root});
 const poolFor=(core,value)=>{if(value===undefined)return null;const p=core.validateExecutionPool(value);
  need(p.version===2,'legacy-central-pool-reconciliation-required');return p;};
 const selectPool=()=>({status:'blocked',diagnostics:[{capability:'host',code:'native-execution-pool-selection-required'}]});
 async function check({core,context,executionPool}){
  const pool=poolFor(core,executionPool);
  if(!pool)return selectPool();
  try{
   const record=await readPrivate(join(root,'native-installation.json'),{missing:true});
   if(!record)return {status:'blocked',diagnostics:[{capability:'host',code:'native-host-not-staged'}]};
   need(record.version===1&&record.phase==='staged'&&record.core===currentPin.coreSha256
    &&record.runnerCount===pool.capacity&&core.canonicalJSON(record.executionPool)===core.canonicalJSON(pool),'installation-identity');
   const files=await runtimeInputs(context);await verifyRuntime(join(root,'runtime'),files);
   for(let i=1;i<=pool.capacity;i++)await verify(join(root,'runner-'+i));
   const observed=await control.check();
   return {status:'blocked',phase:'staged',diagnostics:[...observed.diagnostics,...remaining()]};
  }catch{return {status:'blocked',diagnostics:[{capability:'host',code:'native-installation-reconciliation-required'}]};}
 }
 async function prepareHost({core,context,executionPool}){
  need(process.platform==='linux'&&process.arch==='x64'&&process.versions.node.split('.')[0]==='22','platform');
  const pool=poolFor(core,executionPool);if(!pool)return selectPool();await ensurePrivateDirectory(root);
  return maintenanceLock(root,async assertLease=>{
   const previous=await readPrivate(join(root,'native-installation.json'),{missing:true});
   if(previous)need(previous.version===1&&previous.phase==='staged'&&previous.core===currentPin.coreSha256
    &&core.canonicalJSON(previous.executionPool)===core.canonicalJSON(pool),'preserve-existing-installation');
   const files=await runtimeInputs(context);assertLease();await publishRuntime(root,files);
   await stageRunner(root,'runner-1',{prepare,verify});assertLease();
   for(let i=2;i<=pool.capacity;i++){await stageRunner(root,'runner-'+i,{prepare,verify,copyFrom:join(root,'runner-1')});assertLease();}
   const record={version:1,phase:'staged',producer:currentProducer,core:currentPin.coreSha256,
    executionPool:pool,runnerCount:pool.capacity,runnerDistribution,
    runtimeInventory:[...files].map(([path,bytes])=>({path,sha256:sha(bytes)}))};
   await writePrivate(join(root,'native-installation.json'),record);
   const observed=await control.install();
   return {status:'staged',runnerCount:pool.capacity,diagnostics:[...observed.diagnostics,...remaining()]};
  });
 }
 return Object.freeze({check,prepare:prepareHost,async install(options){return check(options);}});
}
