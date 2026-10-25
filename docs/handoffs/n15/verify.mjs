// Handoff verification only; never imported by the product CLI.
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {readFile,writeFile,mkdtemp,mkdir,rm,realpath} from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
const directory=fileURLToPath(new URL('./',import.meta.url));
const sha=b=>createHash('sha256').update(b).digest('hex');
assert.equal(process.versions.node.split('.')[0],'22','Use the producer Node.js 22 runtime');
const raw=await readFile(join(directory,'provenance.json'));
assert.equal(sha(raw),'6b2fb0502748d8517350663ddca5f1b679946bdd8924c75159f256c776ae0b3b');
const provenance=JSON.parse(raw);
assert.equal(provenance.producer.commit,'e4e2a835f4236975d41ddb4f4bc5ada7dbae1b30');
assert.equal(provenance.artifact.sha256,'01f6b5fbd01ebbba37ea6723db7bdfff25f5cc44c68e832206974248e5717fef');
for(const [name,expected] of Object.entries(provenance.assets)){
 const b=await readFile(join(directory,name));assert.equal(b.length,expected.bytes);assert.equal(sha(b),expected.sha256);
}
const lock=JSON.parse(await readFile(join(directory,'producer-lock.json'),'utf8'));
const dependency=await readFile(join(directory,'jose-6.2.13.tgz'));
assert.equal('sha512-'+createHash('sha512').update(dependency).digest('base64'),lock.packages['node_modules/jose'].integrity);
assert.equal(provenance.assets['producer-contract.md'].sha256,provenance.files['docs/package.md'].sha256);
assert.equal(provenance.assets['producer-tests.mjs'].sha256,provenance.supporting['test/runners.test.mjs'].sha256);
const scratch=await mkdtemp(join(tmpdir(),'nibfin-n15-consumer-'));
try{
 assert.ok(!(await realpath(scratch)).startsWith(resolve(directory)));
 const env={PATH:process.env.PATH,HOME:scratch,LANG:'C',TMPDIR:scratch,npm_config_cache:join(scratch,'cache'),npm_config_userconfig:join(scratch,'empty.npmrc'),npm_config_globalconfig:join(scratch,'global.npmrc')};
 await writeFile(join(scratch,'package.json'),JSON.stringify({private:true,type:'module'}));
 execFileSync('npm',['install','--offline','--ignore-scripts','--no-audit','--no-fund','--package-lock=false',join(directory,'jose-6.2.13.tgz'),join(directory,'nibfin-core.tgz')],{cwd:scratch,env,stdio:'pipe'});
 const root=join(scratch,'node_modules/@nibfin/core');
 for(const [path,expected] of Object.entries(provenance.files)){
  const b=await readFile(join(root,path));assert.equal(b.length,expected.bytes);assert.equal(sha(b),expected.sha256);
 }
 const original=await readFile(join(directory,'producer-tests.mjs'),'utf8');
 assert.equal(original.split("'../src/index.mjs'").length,2,'One explicit public-entry adaptation required');
 await mkdir(join(scratch,'test'));
 await writeFile(join(scratch,'test/runners.test.mjs'),original.replace("'../src/index.mjs'","'@nibfin/core'"));
 const output=execFileSync(process.execPath,['--test','test/runners.test.mjs'],{cwd:scratch,env,encoding:'utf8'});
 assert.match(output,/# fail 0/);process.stdout.write(output);
 console.log(JSON.stringify({status:'VERIFIED',producer:provenance.producer.commit,archiveSha256:provenance.artifact.sha256,installedFiles:Object.keys(provenance.files).length,dependencyIntegrity:'producer-lock sha512 matched',network:'offline installation; fixture transports only',liveRegistrationPerformed:false}));
}finally{await rm(scratch,{recursive:true,force:true});}
