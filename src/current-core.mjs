import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {execFileSync} from 'node:child_process';
import {pathToFileURL} from 'node:url';
import {parseArchive,verifyRegistrationCore} from './core.mjs';

export const currentProducer='9a3dc1148df17725c2e6484367a347ec03afd201';
export const currentPin=Object.freeze({packageVersion:'0.1.0',coreSha256:'56c8224f6cce98ea1deb6b6e4b1e9ec6bc29d365f1f648cdc708f0c7d61cad1e'});
const handoff=new URL('../docs/handoffs/current/',import.meta.url);
const hashes=Object.freeze({'nibfin-core.tgz':'a69ff28f776672e00bd5172aa37b9c3dca80492cd50ce20102fc1c533877a877',
  'release-manifest.json':'e33db9e20ddbd2a39825211c32afcea124687f7f3562ef17809a0c8a89a2b6e5',
  'compatibility.json':'2812125698ad9f316ae082b39924e780a2c1d8737fab966450257ecce7fc6481'});
const sha=b=>createHash('sha256').update(b).digest('hex');
const need=(ok,message)=>{if(!ok)throw Error('Current Core verification: '+message);};
const exportsRequired=['protectedWorkRestriction','createRescueRecords','initialRescueRecords','contentIdentity','canonicalJSON','releaseHash','validateExecutionPool','projectExecutionContext','createHostRouting','createHostIdentityAdapters','loadProtectedHost','loadProtectedDeployment','createInstallation','distributionIdentity','runnerConfiguration','preflightRunner','createAgentEnvironments',
  'createLinuxCodexBackend','createLinuxClaudeBackend','createLinuxGeminiBackend','createWindowsWslBackend',
  'runnerJobPolicies','verifyRunnerJobAdmission','workflowInstallation','rescueInstallation','planInstallationTransition','applyInstallationTransition',
  'codexProcess','observeRunnerLocal','createRescueHost','validateRescuePolicy','loadRescueCompletionSecret','createGithubProvider','localGhTransport','createRegistrationHost','openRegistrationResponse','verifyRelease'];
export async function verifyCurrentCore(directory=handoff){
  const assets={};
  for(const [name,digest] of Object.entries(hashes)){
    const path=new URL(name,directory),s=await fs.lstat(path);
    need(s.isFile()&&!s.isSymbolicLink()&&s.nlink===1&&s.size<=4000000,'unsafe or oversized '+name);
    const bytes=await fs.readFile(path);need(sha(bytes)===digest,'changed '+name);assets[name]=bytes;
  }
  const manifest=JSON.parse(assets['release-manifest.json']),compatibility=JSON.parse(assets['compatibility.json']);
  need(manifest.format===1&&manifest.version===currentPin.packageVersion&&manifest.components.core.sha256===currentPin.coreSha256,'manifest identity');
  const inventory=manifest.components.core.files;
  need(Array.isArray(inventory)&&inventory.length>100,'complete public runtime inventory required');
  const files=parseArchive(assets['nibfin-core.tgz'],inventory.map(f=>f.path),'current');
  for(const f of inventory)need(files.has(f.path)&&sha(files.get(f.path))===f.sha256,'changed component '+f.path);
  const pkg=JSON.parse(files.get('package.json'));
  need(pkg.name==='@nibfin/core'&&pkg.version===currentPin.packageVersion&&pkg.type==='module'&&pkg.exports?.['.']==='./src/index.mjs'
    &&JSON.stringify(pkg.dependencies)===JSON.stringify({jose:'6.2.13'})&&pkg.engines?.node==='>=22 <23'&&pkg.engines?.npm==='>=10 <11'
    &&!pkg.bin&&!pkg.optionalDependencies&&!pkg.peerDependencies
    &&!['preinstall','install','postinstall','prepare'].some(k=>pkg.scripts?.[k]),'unsupported metadata');
  const {assets:historical}=await verifyRegistrationCore();
  const lock=JSON.parse(files.get('npm-shrinkwrap.json'));
  need(lock.packages['node_modules/jose']?.integrity==='sha512-'+createHash('sha512').update(historical['jose-6.2.13.tgz']).digest('base64'),'locked dependency');
  return {assets:{...assets,'jose-6.2.13.tgz':historical['jose-6.2.13.tgz']},files,manifest,compatibility};
}
// Only exact reviewed bytes enter this private, offline installation. Keep it alive
// for the complete operation: runtime factories read their installed files lazily.
export async function withCurrentCore(consume,{directory=handoff,temporaryRoot=tmpdir()}={}){
  const verified=await verifyCurrentCore(directory);
  need(process.versions.node.split('.')[0]==='22','Node 22 required');
  const root=await fs.mkdtemp(join(temporaryRoot,'nibfin-current-'));
  try{
    const env={PATH:process.env.PATH,HOME:root,LANG:'C',npm_config_cache:join(root,'cache'),
      npm_config_userconfig:join(root,'empty.npmrc'),npm_config_globalconfig:join(root,'global.npmrc')};
    need(/^10\./.test(execFileSync('npm',['--version'],{env,encoding:'utf8',timeout:15000})),'npm 10 required');
    for(const name of ['nibfin-core.tgz','jose-6.2.13.tgz'])await fs.writeFile(join(root,name),verified.assets[name],{flag:'wx',mode:0o600});
    await fs.writeFile(join(root,'package.json'),'{"private":true,"type":"module"}',{flag:'wx',mode:0o600});
    execFileSync('npm',['install','--offline','--ignore-scripts','--omit=dev','--no-audit','--no-fund','--package-lock=false',
      join(root,'jose-6.2.13.tgz'),join(root,'nibfin-core.tgz')],{cwd:root,env,stdio:'pipe',timeout:30000});
    const packageRoot=join(root,'node_modules/@nibfin/core');
    for(const [name,bytes] of verified.files)need((await fs.readFile(join(packageRoot,name))).equals(bytes),'installed source '+name);
    const bridge=join(root,'consumer.mjs');await fs.writeFile(bridge,"export * from '@nibfin/core';\n",{flag:'wx'});
    const core=await import(pathToFileURL(bridge).href);
    for(const name of exportsRequired)need(typeof core[name]==='function','missing export '+name);
    core.verifyRelease({manifest:verified.manifest,compatibility:verified.compatibility,compatibilityBytes:verified.assets['compatibility.json'],
      archive:verified.assets['nibfin-core.tgz'],expectedVersion:currentPin.packageVersion,expectedSource:verified.manifest.source});
    const identity=core.distributionIdentity();need(identity.components.core.sha256===currentPin.coreSha256,'installed identity');
    return await consume(core,{root,packageRoot,identity,...verified});
  }finally{await fs.rm(root,{recursive:true,force:true});}
}
