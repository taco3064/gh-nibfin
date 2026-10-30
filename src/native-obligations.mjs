import fs from'node:fs/promises';import{join}from'node:path';import{execFileSync}from'node:child_process';
const need=(ok,code)=>{if(!ok)throw Error('native-obligations:'+code);};
// Inspect a fixed, public source checkout. Unknown setup is held explicitly;
// absence of a host-private hook is never treated as a project requirement.
export async function observeNativeObligations(core,{directory,scopeBody,base,repository}={}){
 const output=execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null','ls-files','-z'],{cwd:directory,encoding:'utf8',timeout:30000,
  env:{PATH:'/usr/bin:/bin',HOME:'/nonexistent',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'}});
 const paths=output.split('\0').filter(Boolean);need(paths.length>0&&paths.length<=20000,'source-inventory');
 const evidenceReference='https://github.com/'+repository+'/tree/'+base,observations=[];
 const hookPaths=paths.filter(p=>/(^|\/)(?:\.husky|\.githooks|git-hooks|lefthook)(?:\/|\.)/.test(p)
  ||/(^|\/)(?:\.pre-commit-config\.yaml|\.gitmodules)$/.test(p));
 need(hookPaths.length===0,'hook-configuration-needs-supported-observer');
 for(const path of paths.filter(p=>p==='package.json'||p.endsWith('/package.json'))){
  const raw=await fs.readFile(join(directory,path),'utf8');need(Buffer.byteLength(raw)<=1048576,'package-size');const value=JSON.parse(raw);
  need(!['preinstall','install','postinstall','prepare'].some(k=>value.scripts?.[k])&&!value.husky&&!value['simple-git-hooks'],'package-hook-setup-needs-supported-observer');
  need(!Object.keys({...value.dependencies,...value.devDependencies}).some(k=>['husky','simple-git-hooks','lefthook'].includes(k)),'package-hook-setup-needs-supported-observer');
  observations.push({path,identity:core.bodyDigest(raw),scripts:value.scripts??{},workspaces:value.workspaces??null});
 }
 // This explicit Scope clause describes an empty required-artifact set. Without
 // an independent Scope contract, the model's own empty inventory is insufficient.
 need(scopeBody.includes('No additional untracked or ignored Candidate artifacts are required.'),'scope-artifact-applicability-required');
 const source={repository,base,paths,packages:observations},identity=core.contentIdentity(source);
 return {requiredArtifacts:[],scopeEvidenceReference:'https://github.com/'+repository+'/issues#scope-'+core.bodyDigest(scopeBody),
  hookObservation:{hooks:[],areas:core.hookConfigurationAreas.map(area=>({area,status:'resolved',evidenceReference:evidenceReference+'#'+identity,
   detail:'Inspected fixed source '+base+': no repository hook setup, lifecycle installer or supported Hook directory; Git execution disables ambient hooks.'}))},source};
}
