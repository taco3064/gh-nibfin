import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{execFileSync}from'node:child_process';
import{withCurrentCore}from'../../src/current-core.mjs';import{prepareNativeCleanBase}from'../../src/native-clean-base.mjs';import{retainUnacceptedWorkspace}from'../../src/native-unaccepted-work.mjs';

test('native repair prepares a fresh base before the same Writer, preserving dirty or ignored work',async()=>withCurrentCore(async core=>{
 const parent=await fs.mkdtemp('/tmp/nibfin-clean-base-'),source=join(parent,'source'),work=join(parent,'work');
 const env={PATH:'/usr/bin:/bin',HOME:'/nonexistent',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null'},git=(cwd,args)=>execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null',...args],{cwd,env,stdio:['pipe','pipe','pipe']}).toString().trim();
 try{
  await fs.mkdir(source,{mode:0o700});git(source,['init','--initial-branch=main','--template=']);
  await fs.writeFile(join(source,'README.md'),'old\n');await fs.writeFile(join(source,'guide.md'),'original guide\n');await fs.writeFile(join(source,'.gitignore'),'ignored\n');
  git(source,['add','.']);const commit=()=>git(source,['-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-m','fixture']);commit();const old=git(source,['rev-parse','HEAD']);
  await fs.writeFile(join(source,'README.md'),'new\n');git(source,['add','README.md']);commit();const base=git(source,['rev-parse','HEAD']);git(source,['checkout','--detach',base]);git(parent,['clone','--no-hardlinks',source,work]);await fs.chmod(work,0o700);git(work,['checkout','--detach',old]);
  const scopeBody='Update documentation only.',shapeBody='Candidate artifacts applicability: The Scope requires no additional untracked or ignored artifacts.\nCandidate hooks applicability: The fixed source has no applicable Hook configuration.\n```nibfin-candidate\n{"artifacts":[],"hooks":[]}\n```';
  const state={target:{repository:'example/project'},cycle:'fixture',policy:{version:1},paths:['README.md','.gitignore','draft.txt','guide.md'],scopeBody,shapeBody,scopeHash:core.bodyDigest(scopeBody)},records=[];
  const retain=async v=>{records.push(v);return'native-record:'+core.contentIdentity(v);};
  const snapshot=async()=>{let v;await retainUnacceptedWorkspace({directory:work,execution:{id:'original'},retain:async x=>{v=x;return retain(x);}});return v;};
  let original=await snapshot();
  await prepareNativeCleanBase(core,{directory:work,sourceDirectory:source,base,state,snapshot:original,reference:'original-custody',retain});
  assert.equal(git(work,['rev-parse','HEAD']),base);assert.equal(await fs.readFile(join(work,'README.md'),'utf8'),'new\n');
  assert.equal(Buffer.from(original.entries.find(e=>e.path==='README.md').data,'base64').toString(),'old\n');assert.equal(records.at(-1).accepted,false);
  git(work,['checkout','--detach',old]);await fs.writeFile(join(work,'draft.txt'),'unaccepted draft\n');await fs.writeFile(join(work,'guide.md'),'unaccepted guide\n');original=await snapshot();
  await prepareNativeCleanBase(core,{directory:work,sourceDirectory:source,base,state,snapshot:original,reference:'draft-custody',retain});
  assert.equal(git(work,['rev-parse','HEAD']),base);assert.equal(await fs.readFile(join(work,'draft.txt'),'utf8'),'unaccepted draft\n');
  assert.equal(await fs.readFile(join(work,'guide.md'),'utf8'),'unaccepted guide\n');
  assert.equal(Buffer.from(original.entries.find(e=>e.path==='draft.txt').data,'base64').toString(),'unaccepted draft\n');
  git(work,['reset','--hard',old]);await fs.writeFile(join(work,'README.md'),'unaccepted edit\n');original=await snapshot();
  await assert.rejects(prepareNativeCleanBase(core,{directory:work,sourceDirectory:source,base,state,snapshot:original,reference:'dirty-custody',retain}),/three-way-repair-required/);
  assert.equal(await fs.readFile(join(work,'README.md'),'utf8'),'unaccepted edit\n');
  await prepareNativeCleanBase(core,{directory:work,sourceDirectory:source,base,state,snapshot:original,reference:'conflict-custody',retain,allowConflicts:true});
  assert.equal(git(work,['rev-parse','HEAD']),base);assert.match(await fs.readFile(join(work,'README.md'),'utf8'),/<<<<<<<[\s\S]*unaccepted edit/);
  assert.ok(git(work,['ls-files','--unmerged']).includes('README.md'));
  assert.equal(records.at(-1).diagnostic.conflicts,true,'conflicts reach the original Writer, never a false Candidate pass');
  git(work,['reset','--hard',old]);
  await fs.writeFile(join(work,'README.md'),'old\n');await fs.writeFile(join(work,'ignored'),'preserve me');original=await snapshot();
  await assert.rejects(prepareNativeCleanBase(core,{directory:work,sourceDirectory:source,base,state,snapshot:original,reference:'ignored-custody',retain}),/untracked-bytes-require-repair/);
  assert.equal(await fs.readFile(join(work,'ignored'),'utf8'),'preserve me');
 }finally{await fs.rm(parent,{recursive:true});}
}));
