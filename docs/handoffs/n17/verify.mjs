import {withAutomaticCore} from '../../../src/core.mjs';
import {readFile,writeFile,mkdtemp,mkdir,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {execFileSync} from 'node:child_process';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
const root=await mkdtemp(join(tmpdir(),'n17-verification-'));
try {
  const provenance=JSON.parse(await readFile(new URL('provenance.json',import.meta.url)));
  for(const [name,digest] of Object.entries(provenance.supporting))assert.equal(createHash('sha256').update(await readFile(new URL(name,import.meta.url))).digest('hex'),digest);
  await withAutomaticCore(async core=>{
    assert.equal(typeof core.openRegistrationResponse,'function');
    assert.equal(typeof core.registrationArtifactReader,'function');
  });
  // Adapt only public-entry import; fixture helper bytes come from the handoff.
  const source=await readFile(new URL('producer-tests.mjs',import.meta.url),'utf8');
  const fixture=await readFile(new URL('custody-fixture.mjs',import.meta.url));
  await mkdir(join(root,'test'));await mkdir(join(root,'tooling'));
  
  await withAutomaticCore(async (core,installed)=>{
    await writeFile(join(root,'tooling/custody-fixture.mjs'),fixture.toString().replace("'../src/index.mjs'",JSON.stringify(join(installed.root,'consumer.mjs'))));
    await writeFile(join(root,'test/registration.test.mjs'),source.replace("'../src/index.mjs'",JSON.stringify(join(installed.root,'consumer.mjs'))));
    const output=execFileSync(process.execPath,['--test',join(root,'test/registration.test.mjs')],
      {env:{PATH:process.env.PATH,HOME:root,LANG:'C'},encoding:'utf8',timeout:60000});
    assert.match(output,/# fail 0/);process.stdout.write(output);
  });
  console.log('N17 exact archive/source/dependency bytes and installed public exports verified.');
} finally {await rm(root,{recursive:true,force:true});}
