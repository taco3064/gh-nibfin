import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';
import {gzipSync,gunzipSync} from 'node:zlib';
import {withRegistrationCore,verifyRegistrationCore,parseArchive,validateRegistrationMetadata} from '../src/core.mjs';
const handoff = new URL('../docs/handoffs/n15/',import.meta.url);
const verified = await verifyRegistrationCore();
async function disposable(run) {
  const root=await fs.mkdtemp(join(tmpdir(),'N15 loader test '));
  try {await run(root);} finally {await fs.rm(root,{recursive:true,force:true});}
}
test('AC6 N15 trust pins reject missing/tampered archives, provenance, dependency and self-consistent substitutions',()=>disposable(async root=>{
  const directory=pathToFileURL(root+'/');
  for(const name of ['provenance.json',...Object.keys(verified.assets)]) await fs.copyFile(new URL(name,handoff),join(root,name));
  for(const name of ['provenance.json',...Object.keys(verified.assets)]) {
    const path=join(root,name), original=await fs.readFile(path);
    await fs.rm(path);
    await assert.rejects(withRegistrationCore(()=>assert.fail('executed'),{directory,temporaryRoot:root}));
    for(const bytes of [Buffer.from('replacement'),Buffer.alloc(100001)]) {
      await fs.writeFile(path,bytes);
      await assert.rejects(withRegistrationCore(()=>assert.fail('executed'),{directory,temporaryRoot:root}));
    }
    await fs.writeFile(path,original);
  }
  const p=JSON.parse(await fs.readFile(join(root,'provenance.json')));
  p.assets['nibfin-core.tgz'].sha256='0'.repeat(64);
  await fs.writeFile(join(root,'provenance.json'),JSON.stringify(p));
  await assert.rejects(verifyRegistrationCore(directory),/provenance/);
  assert.deepEqual((await fs.readdir(root)).sort(),['jose-6.2.13.tgz','nibfin-core.tgz','producer-lock.json','provenance.json']);
}));
test('AC6 N15 structural and metadata checks retain exact allowlist and inert imports',()=>{
  const bytes=verified.assets['nibfin-core.tgz'], names=[...verified.files.keys()];
  assert.equal(parseArchive(bytes,names).size,23);
  for(const mutation of [tar=>{tar.write('package/../evil',0);},tar=>{tar[156]=50;},tar=>{tar.write('package/extra',0);}]) {
    const tar=gunzipSync(bytes);mutation(tar);tar.fill(32,148,156);
    tar.write(tar.subarray(0,512).reduce((a,b)=>a+b,0).toString(8).padStart(6,'0')+'\0 ',148);
    assert.throws(()=>parseArchive(gzipSync(tar),names));
  }
  for(const patch of [{version:'9'},{dependencies:{jose:'*'}},{scripts:{install:'evil'}},{bin:'evil'},
    {exports:{'.':'./src/index.mjs','./evil':'./evil.mjs'}},{engines:{node:'*'}},{optionalDependencies:{evil:'1'}}]) {
    const files=new Map(verified.files);
    files.set('package.json',Buffer.from(JSON.stringify({...JSON.parse(files.get('package.json')),...patch})));
    assert.throws(()=>validateRegistrationMetadata(files));
  }
});
test('AC6 actual offline N15 installation from checkout with spaces; callback failures remove residue',()=>disposable(async root=>{
  const copy=join(root,'checkout with spaces');await fs.mkdir(copy);
  await fs.cp(new URL('../src/',import.meta.url),join(copy,'src'),{recursive:true});
  await fs.mkdir(join(copy,'docs/handoffs'),{recursive:true});
  await fs.cp(handoff,join(copy,'docs/handoffs/n15'),{recursive:true});
  const loader=await import(pathToFileURL(join(copy,'src/core.mjs')).href);
  const scratch=join(root,'scratch');await fs.mkdir(scratch);
  await loader.withRegistrationCore(async core=>{
    assert.equal(core.githubId('9007199254740993'),'9007199254740993');
    assert.equal(core.createLinuxCodexBackend,undefined);
    const provider=core.createGithubProvider({attempts:1,transport:async()=>({status:403,headers:{},body:'PRIVATE_SENTINEL'})});
    const result=await core.observeRunners(provider,{target:{owner:'example',repository:'a',id:'51',ownerId:'7',ownerType:'User'},
      registration:{level:'repository',id:'51'},records:[],conditions:{platform:'linux',labels:[]},correlation:'fixture'});
    assert.equal(result.outcome,'unavailable');assert.ok(!JSON.stringify(result).includes('PRIVATE_SENTINEL'));
  },{temporaryRoot:scratch});
  await assert.rejects(loader.withRegistrationCore(()=>{throw Error('callback');},{temporaryRoot:scratch}),/callback/);
  assert.deepEqual(await fs.readdir(scratch),[]);
}));
