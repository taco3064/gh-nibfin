import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {pathToFileURL} from 'node:url';
import {spawnSync} from 'node:child_process';
import {readPrivate,privatePath,ensurePrivateDirectory,sha,writePrivate} from './local-state.mjs';
import {managedHostRecords} from './host-store.mjs';
// Trusted local host bindings are the public createRescueHost configuration,
// supplied by the existing control host. They never enter a member environment.
const configPath=()=>join(userInfo().homedir,'.config/nibfin/host.json');
export async function hostConfiguration(path=configPath()){
  const c=await readPrivate(path);
  if(c.version!==1||c.account!==userInfo().username||typeof c.directory!=='string'||c.directory.includes(' ')||!c.binding
    ||!/^[-a-zA-Z0-9_./]+$/.test(c.binding.path)||!/^[a-f0-9]{64}$/.test(c.binding.sha256))throw Error('Unsupported control-host configuration.');
  await privatePath(c.binding.path);const bytes=await fs.readFile(c.binding.path);if(bytes.length>131072||sha(bytes)!==c.binding.sha256)throw Error('Control-host binding identity changed.');
  return {...c,bindingBytes:bytes};
}
function systemctl(args){
  const r=spawnSync('systemctl',['--user',...args],{env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,LANG:'C',XDG_RUNTIME_DIR:`/run/user/${userInfo().uid}`},encoding:'utf8',timeout:args[0]==='start'?60000:15000,maxBuffer:16384});
  return {ok:!r.error&&r.status===0,output:r.status===0?r.stdout:''};
}
export function existingControlHost({path,process:run=systemctl}={}){
  async function source(core,{context,initialize=false}){
    const c=await hostConfiguration(path),binding=await import(pathToFileURL(c.binding.path).href+'?identity='+c.binding.sha256);
    if(initialize)await ensurePrivateDirectory(c.directory);
    const storage=await managedHostRecords(core,{directory:c.directory,hosts:binding.hosts,
      installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins},initialize});
    return {c,binding:{...binding,hosts:storage.hosts},storage};
  }
  async function check({core,context,repo,executionPool}){
    if(executionPool)return {status:'blocked',diagnostics:[{capability:'executionPool',code:'protected-shared-host-onboarding-incomplete'}]};
    let storage;
    try{
      const loaded=await source(core,{context});storage=loaded.storage;const {c,binding}=loaded;
      if(!Array.isArray(binding.hosts)||binding.hosts.length<1||binding.hosts.length>32)throw Error();
      const matches=binding.hosts.filter(h=>h.configuration?.policy?.repository===repo);
      if(matches.length!==1)throw Error();
      const entry=matches[0];core.validateRescuePolicy(entry.configuration.policy);
      if(typeof entry.configuration.records?.read!=='function'||typeof entry.configuration.records?.subscribe!=='function')throw Error();
      const records=await entry.configuration.records.read();if(records?.version!==1||!records.identity)throw Error();
      const installed=await readPrivate(join(c.directory,'installation.json'));
      if(installed.core!==context.identity.components.core.sha256||installed.binding!==c.binding.sha256)throw Error();
      if(installed.launcher!==sha(await fs.readFile(new URL('./notifier.mjs',import.meta.url)))||sha(await fs.readFile(join(c.directory,'notifier.mjs')))!==installed.launcher||sha(await fs.readFile(join(c.directory,'notifier.json')))!==installed.configuration)throw Error();
      if(!installed.adapters||Object.keys(installed.adapters).sort().join(' ')!=='host-store.mjs local-state.mjs')throw Error();
      for(const [name,digest] of Object.entries(installed.adapters)){
        await privatePath(join(c.directory,name));
        if(sha(await fs.readFile(join(c.directory,name)))!==digest||sha(await fs.readFile(new URL('./'+name,import.meta.url)))!==digest)throw Error();
      }
      const unit=join(c.directory,'nibfin-rescue-wakeups.service');await privatePath(unit);
      if(sha(await fs.readFile(unit))!==installed.unit)throw Error();
      const observed=run(['show','nibfin-rescue-wakeups.service','--property=ActiveState','--property=FragmentPath','--property=User','--property=Type']);
      const properties=Object.fromEntries(observed.output.trim().split('\n').map(line=>{const at=line.indexOf('=');return [line.slice(0,at),line.slice(at+1)];}));
      if(!observed.ok||properties.ActiveState!=='active'||properties.Type!=='notify'
        ||properties.User&&properties.User!==userInfo().username||await fs.realpath(properties.FragmentPath)!==unit)throw Error();
      for(const [file,digest] of Object.entries(installed.files)){
        await privatePath(join(c.directory,'node_modules/@nibfin/core',file),{missing:false});
        if(sha(await fs.readFile(join(c.directory,'node_modules/@nibfin/core',file)))!==digest)throw Error();
      }
      return {status:'ready'};
    }catch{return {status:'blocked',diagnostics:[{capability:'recovery',code:'missing-incompatible-or-inactive-host-notifier',action:'Restore the authorized host source, private state and compatible notifier; init installs its one notifier. Member create never installs a host.'}]};}
    finally{await storage?.close();}
  }
  async function install({core,context,repo,executionPool}){
    if(executionPool)throw Error('The existing notifier-only host cannot install shared execution. Complete the official protected host onboarding first.');
    let storage;try{
    const loaded=await source(core,{context,initialize:true});storage=loaded.storage;const {c,binding}=loaded;const entry=binding.hosts?.find(h=>h.configuration?.policy?.repository===repo);
    if(!entry)throw Error('The authorized control host has no matching public repository binding.');
    core.validateRescuePolicy(entry.configuration.policy);
    // Validate the real existing host composition before installing a service.
    // Its original state/authority/operations remain owned by that host and Core.
    const secret=await core.loadRescueCompletionSecret(entry.secretFile,{uid:userInfo().uid,excludedRoots:entry.excludedRoots});
    core.createRescueHost({configuration:entry.configuration,secret,attribute:entry.attribute,onError:entry.onError});
    await ensurePrivateDirectory(c.directory);
    const old=await readPrivate(join(c.directory,'installation.json'),{missing:true});
    const unitPath=join(c.directory,'nibfin-rescue-wakeups.service');
    try{const bytes=await fs.readFile(unitPath);if(!old||sha(bytes)!==old.unit)throw Error('Modified notifier service preserved.');}catch(e){if(e.code!=='ENOENT')throw e;}
    if(old&&old.binding!==c.binding.sha256)throw Error('Existing notifier host binding changed; reconcile its original state and perform a safe restart through upgrade.');
    if(old&&old.core!==context.identity.components.core.sha256)throw Error('Host runtime identity changed; use reconciled upgrade before replacing its notifier.');
    try{await fs.lstat(join(c.directory,'node_modules'));}catch(e){if(e.code!=='ENOENT')throw e;
      await fs.cp(join(context.root,'node_modules'),join(c.directory,'node_modules'),{recursive:true,errorOnExist:true,force:false});}
    const compare=async(relative='')=>{
      const expected=join(context.root,'node_modules',relative),actual=join(c.directory,'node_modules',relative),st=await fs.lstat(actual);
      if(st.isSymbolicLink()||st.uid!==userInfo().uid)throw Error('Unsafe installed host package.');
      if(st.isDirectory()){
        await fs.chmod(actual,0o700);const names=await fs.readdir(expected);if(JSON.stringify(names.sort())!==JSON.stringify((await fs.readdir(actual)).sort()))throw Error('Host package inventory changed.');
        for(const name of names)await compare(join(relative,name));
      }else{if(!st.isFile()||st.nlink!==1||!(await fs.readFile(expected)).equals(await fs.readFile(actual)))throw Error('Host package source changed.');await fs.chmod(actual,0o600);}
    };await compare();
    const daemon=join(c.directory,'notifier.mjs'),launcher=await fs.readFile(new URL('./notifier.mjs',import.meta.url));
    try{const before=await fs.readFile(daemon);if(!before.equals(launcher))throw Error('Modified notifier launcher preserved.');}catch(e){if(e.code!=='ENOENT')throw e;await fs.writeFile(daemon,launcher,{flag:'wx',mode:0o600});}
    const adapters={};
    for(const name of ['host-store.mjs','local-state.mjs']){
      const bytes=await fs.readFile(new URL('./'+name,import.meta.url)),target=join(c.directory,name);adapters[name]=sha(bytes);
      try{await privatePath(target);if(!(await fs.readFile(target)).equals(bytes))throw Error('Modified host adapter preserved.');}
      catch(error){if(error.code!=='ENOENT')throw error;await fs.writeFile(target,bytes,{flag:'wx',mode:0o600});}
    }
    const configuration={version:1,binding:c.binding,core:context.identity.components.core.sha256,directory:c.directory,
      installation:{packageVersion:context.identity.packageVersion,pins:context.identity.pins}};
    await writePrivate(join(c.directory,'notifier.json'),configuration);
    const unit=`[Unit]\nDescription=Nibfin durable recovery notifications\n[Service]\nType=notify\nNotifyAccess=all\nTimeoutStartSec=45\nExecStart=/usr/local/bin/node ${daemon} ${join(c.directory,'notifier.json')}\nEnvironment=HOME=${userInfo().homedir}\nUnsetEnvironment=GH_TOKEN GITHUB_TOKEN NODE_OPTIONS NODE_PATH\nRestart=on-failure\nNoNewPrivileges=true\n[Install]\nWantedBy=default.target\n`;
    if(!old)await fs.writeFile(unitPath,unit,{flag:'wx',mode:0o600});
    await writePrivate(join(c.directory,'installation.json'),{version:1,core:configuration.core,binding:c.binding.sha256,unit:sha(unit),launcher:sha(launcher),adapters,configuration:sha(await fs.readFile(join(c.directory,'notifier.json'))),
      files:Object.fromEntries(context.manifest.components.core.files.map(f=>[f.path,f.sha256]))});
    for(const args of [['link',unitPath],['daemon-reload'],['enable','nibfin-rescue-wakeups.service'],['start','nibfin-rescue-wakeups.service']])
      if(!run(args).ok)throw Error('Host notifier installation interrupted; retain original service/configuration for reconciliation.');
    return check({core,context,repo});
    }finally{await storage?.close();}
  }
  return {check,install};
}
