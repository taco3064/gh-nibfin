// Internal service entry installed by init on the ONE existing control host.
// There is no additional product command and no delivery planner here.
import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {userInfo} from 'node:os';
import {pathToFileURL} from 'node:url';
import * as core from '@nibfin/core';
import {managedHostRecords} from './host-store.mjs';
const controllers=[];
let storage;
try{
  const path=process.argv[2],s=await fs.lstat(path);
  if(!s.isFile()||s.isSymbolicLink()||s.uid!==process.getuid()||s.mode&0o077)throw Error();
  const config=JSON.parse(await fs.readFile(path,'utf8'));
  if(config.version!==1||core.distributionIdentity().components.core.sha256!==config.core)throw Error();
  const st=await fs.lstat(config.binding.path);if(!st.isFile()||st.isSymbolicLink()||st.uid!==process.getuid()||st.mode&0o077)throw Error();
  const bytes=await fs.readFile(config.binding.path);
  if(createHash('sha256').update(bytes).digest('hex')!==config.binding.sha256)throw Error();
  const binding=await import(pathToFileURL(config.binding.path).href);
  if(!Array.isArray(binding.hosts)||binding.hosts.length<1||binding.hosts.length>32)throw Error();
  const repositories=new Set();
  storage=await managedHostRecords(core,{directory:config.directory,hosts:binding.hosts,installation:config.installation});
  for(const entry of storage.hosts){
    const policy=core.validateRescuePolicy(entry.configuration.policy);
    if(repositories.has(policy.repository))throw Error();repositories.add(policy.repository);
    const secret=await core.loadRescueCompletionSecret(entry.secretFile,{uid:userInfo().uid,excludedRoots:entry.excludedRoots});
    const host=core.createRescueHost({configuration:entry.configuration,secret,attribute:entry.attribute,onError:entry.onError});
    controllers.push(await host.start());
  }
  await new Promise(resolve=>{for(const signal of ['SIGINT','SIGTERM'])process.once(signal,resolve);});
}catch{process.stderr.write('Nibfin notifier unavailable; inspect private host source, policy, records and account configuration.\n');process.exitCode=2;}
finally{for(const controller of controllers)controller.close();await storage?.close();}
