import {userInfo} from 'node:os';
const need=(ok,code)=>{if(!ok)throw Error('native-jit:'+code);};
const base64=value=>{
 need(typeof value==='string'&&value.length>0&&value.length<=2097152&&/^[A-Za-z0-9+/]+={0,2}$/.test(value),'encoding');
 const bytes=Buffer.from(value,'base64');need(bytes.toString('base64')===value,'encoding');return bytes;
};
/** The pinned official Listener writes every dictionary key as a pathname.
 * Validate the decoded paths BEFORE supplying JIT configuration to that binary.
 * https://github.com/actions/runner/blob/d1eb12d4b115c301201de27d667284e1a80f2ab5/src/Runner.Listener/Runner.cs
 * Credentials remain opaque private bytes, never diagnostics or command args.
 */
export function nativeJitConfiguration(encoded,record){
 const files=JSON.parse(base64(encoded).toString('utf8'));
 need(files&&Object.getPrototypeOf(files)===Object.prototype
  &&Object.keys(files).sort().join(' ')==='.credentials .credentials_rsaparams .runner','files');
 for(const value of Object.values(files))base64(value);
 const settings=JSON.parse(base64(files['.runner']).toString('utf8'));
 need(settings&&Object.getPrototypeOf(settings)===Object.prototype,'settings');
 const runnerId=typeof settings.agentId==='number'&&Number.isSafeInteger(settings.agentId)?String(settings.agentId):settings.agentId;
 need(runnerId===record.runnerId&&settings.agentName===record.runnerName&&settings.ephemeral===true
  &&settings.disableUpdate===true&&settings.workFolder==='_work','runner-identity');
 // GitHub documents that JIT GitHubUrl can be empty. The actual repository
 // binding is the independently verified scoped POST and Runner ID readback.
 if(settings.gitHubUrl)need(settings.gitHubUrl==='https://github.com/'+record.request.repository,'repository');
 for(const field of ['serverUrl','serverUrlV2'])if(settings[field]){
  const url=new URL(settings[field]);need(url.protocol==='https:'&&!url.username&&!url.password&&!url.port&&!url.search&&!url.hash
   &&url.hostname.endsWith('.actions.githubusercontent.com'),'server');
 }
 need(['serverUrl','serverUrlV2'].some(k=>typeof settings[k]==='string'&&settings[k].length>0),'server');
 return {encoded,settings,files:Object.fromEntries(Object.entries(files).map(([path,value])=>[path,base64(value)]))};
}

export function nativeRunnerEnvironment(admission){
 need(admission&&Object.keys(admission).sort().join(' ')==='ACTIONS_RUNNER_HOOK_JOB_STARTED NIBFIN_RUNNER_JOB_POLICY'
  &&Object.values(admission).every(v=>typeof v==='string'&&v.startsWith('/')&&!/[\x00-\x1f\x7f]/.test(v)),'admission-environment');
 return {PATH:'/usr/local/bin:/usr/bin:/bin',HOME:userInfo().homedir,LANG:'C',
  XDG_RUNTIME_DIR:'/run/user/'+userInfo().uid,...admission};
}
