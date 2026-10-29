import {createHash} from 'node:crypto';
import {dockerObservation} from './docker-observation.mjs';
// Synthetic OS/Docker transport only. The installed Core constructs commands,
// validates returned daemon/container identities and interprets readiness.
// This does not establish native daemon, login, model or egress isolation.
export function readinessTransport(core,context,{agent='codex',flags={}}={}){
  const calls=[],networks=new Map(),gateways=new Map(),containers=new Map(),image='sha256:'+'1'.repeat(64);
  const respond=v=>({ok:true,output:typeof v==='string'?v:JSON.stringify(v)});
  const process=async request=>{
    calls.push(structuredClone(request));
    const a=request.args.slice(2),op=a[0];
    if(flags.throw===op)throw Error('PRIVATE-SENTINEL');
    if(op==='version')return respond(JSON.stringify(flags.docker??core.linuxCodexProfile.docker));
    if(op==='info')return respond({ServerVersion:flags.serverDocker??flags.docker??core.linuxCodexProfile.docker,OSType:'linux',Architecture:'x86_64',CgroupVersion:'2',CgroupDriver:'systemd',MemoryLimit:true,PidsLimit:true,CpuCfsQuota:true,CpuCfsPeriod:true,SecurityOptions:flags.security??['name=rootless','name=seccomp,profile=builtin']});
    if(op==='image')return respond([{Id:image,Os:'linux',Architecture:'amd64',Config:{Env:flags.imageEnvironment??['PATH=/usr/local/bin:/usr/bin:/bin']}}]);
    if(op==='network'){
      if(a[1]==='create'){const name=a.at(-1);networks.set(name,{Name:name,Internal:true,EnableIPv6:false,Options:{'com.docker.network.bridge.gateway_mode_ipv4':'isolated'},Driver:'bridge',Labels:{'nibfin.egress':name}});return respond(name);}
      if(a[1]==='inspect')return respond([networks.get(a[2])]);
      if(a[1]==='connect'){const name=a.at(-1),network=a.at(-2);gateways.get(name).NetworkSettings.Networks[network]={IPAddress:'172.30.1.2',Aliases:['nibfin-egress']};networks.get(network).Containers={[name]:{Name:name,IPv4Address:'172.30.1.2/24'}};return respond('');}
      if(a[1]==='rm'){networks.delete(a[2]);return respond('');}
      if(a[1]==='ls')return respond([...networks.keys()].join('\n'));
    }
    if(op==='create'){
      const name=a[a.indexOf('--name')+1],observed=dockerObservation(a,image),gateway=a.includes('/gateway.mjs');
      const labels=gateway?{'nibfin.egress':a[a.indexOf('--label')+1].split('=')[1]}:{'nibfin.execution':name};
      const value={...observed,Id:name,Name:'/'+name,Image:image,Config:{...observed.Config,Labels:labels},State:{Running:false,Status:'created',ExitCode:0},HostConfig:{...observed.HostConfig,...(!gateway?{Dns:['127.0.0.1'],DnsSearch:['.']}:{})},NetworkSettings:{Networks:gateway?{bridge:{}}:{[observed.HostConfig.NetworkMode]:{IPAddress:'172.30.1.3'}}}};
      (gateway?gateways:containers).set(name,value);return respond(name);
    }
    if(op==='inspect')return respond([gateways.get(a[1])??containers.get(a[1])]);
    if(op==='start'){
      const name=a.at(-1),gateway=gateways.get(name);if(gateway){gateway.State.Running=true;return respond('');}
      const c=containers.get(name);c.State={Running:false,Status:'exited',ExitCode:0};
      const tool=agent==='claude'?core.linuxClaudeProfile.claude:agent==='gemini'?core.linuxGeminiProfile.gemini:core.linuxCodexProfile.codex;
      return respond({verificationSupervisor:true,diagnostic:'ready',node:core.linuxCodexProfile.node,platform:'linux',arch:'x64',guest:true,version:tool,login:flags.login??true,
        network:[...core.endpointProfiles[agent],...core.endpointProfiles['dependency-download']].filter(h=>h!==flags.missingDomain).map(hostname=>({hostname,kind:'verified'}))});
    }
    if(op==='exec'&&gateways.has(a[1]))return respond(createHash('sha256').update(context.files.get('src/egress-gateway.mjs')).digest('hex'));
    if(op==='rm'){if(flags.removeFail)return {ok:false,output:''};containers.delete(a.at(-1));gateways.delete(a.at(-1));return respond('');}
    if(op==='container'&&a.includes('--filter'))return respond([...gateways.keys()].join('\n'));
    if(op==='container')return respond([...containers.keys()].map(Names=>JSON.stringify({Names})).join('\n'));
    throw Error('Unexpected readiness transport operation '+op);
  };
  return {process,calls,flags,image,networks,gateways,containers};
}
