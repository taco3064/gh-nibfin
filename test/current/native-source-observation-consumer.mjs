import test from'node:test';import assert from'node:assert/strict';import fs from'node:fs/promises';import{join}from'node:path';import{execFileSync}from'node:child_process';
import{withCurrentCore}from'../../src/current-core.mjs';import{observeNativeObligations}from'../../src/native-obligations.mjs';
import{retainUnacceptedWorkspace}from'../../src/native-unaccepted-work.mjs';
test('real source inspection requires explicit Scope artifact applicability and detects package Hook installers',async()=>withCurrentCore(async(core,context)=>{
 const directory=join(context.root,'source-observation');await fs.mkdir(directory,{mode:0o700});
 const git=args=>execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null',...args],{cwd:directory,env:{PATH:'/usr/bin:/bin',HOME:'/nonexistent',GIT_CONFIG_GLOBAL:'/dev/null',GIT_CONFIG_NOSYSTEM:'1'}});
 git(['init','--template=']);await fs.writeFile(join(directory,'package.json'),JSON.stringify({scripts:{test:'node --test'}}));git(['add','package.json']);
 const input={directory,scopeBody:'No additional untracked or ignored Candidate artifacts are required.',base:'a'.repeat(40),repository:'example/project'};
 const observed=await observeNativeObligations(core,input);assert.equal(core.validateHookObservation(observed.hookObservation).ok,true);
 assert.deepEqual(observed.requiredArtifacts,[]);assert.ok(observed.source.packages[0].identity);
 await assert.rejects(observeNativeObligations(core,{...input,scopeBody:'Missing independent applicability'}),/scope-artifact-applicability-required/);
 await fs.writeFile(join(directory,'package.json'),JSON.stringify({scripts:{prepare:'husky'}}));
 await assert.rejects(observeNativeObligations(core,input),/package-hook-setup-needs-supported-observer/);
}));

test('unaccepted work retains actual binary/Git bytes and link targets without copying role service credentials',async()=>withCurrentCore(async(core,context)=>{
 const directory=join(context.root,'writer');await fs.mkdir(directory,{mode:0o700});await fs.mkdir(join(directory,'.git'),{mode:0o700});
 await fs.writeFile(join(directory,'.git/HEAD'),'ref: refs/heads/original\n');await fs.writeFile(join(directory,'binary'),Buffer.from([0,255,3]));
 await fs.symlink('/outside/private',join(directory,'link'));const before=await fs.readFile(join(directory,'binary'));let retained;
 const ref=await retainUnacceptedWorkspace({directory,execution:{id:'original'},retain:async value=>{retained=value;return'native-record:retained';}});
 assert.equal(ref,'native-record:retained');assert.equal(retained.status,'UNACCEPTED');
 assert.deepEqual(Buffer.from(retained.entries.find(e=>e.path==='binary').data,'base64'),before);
 assert.equal(Buffer.from(retained.entries.find(e=>e.path==='link').data,'base64').toString(),'/outside/private');
 assert.ok(retained.entries.some(e=>e.path==='.git/HEAD'));assert.deepEqual(await fs.readFile(join(directory,'binary')),before);
}));
