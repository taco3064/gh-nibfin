// Internal first-party systemd entry; init owns its fixed source and unit.
import {spawnSync} from 'node:child_process';
import {runNativeControl} from './native-control-process.mjs';
const controller=new AbortController();
const stop=()=>controller.abort();for(const signal of ['SIGTERM','SIGINT'])process.once(signal,stop);
try{
 if(process.argv.length!==3||!/^[/@]/.test(process.env.NOTIFY_SOCKET??''))throw Error();
 await runNativeControl(process.argv[2],{signal:controller.signal,ready:async()=>{
  const r=spawnSync('/usr/bin/systemd-notify',['--ready','--status=Nibfin control running; inspect native readiness before delivery'],
   {env:{LANG:'C',NOTIFY_SOCKET:process.env.NOTIFY_SOCKET},stdio:'ignore',timeout:15000});
  if(r.error||r.status!==0)throw Error();
 }});
}catch{process.stderr.write('Nibfin native control unavailable; original work is preserved.\n');process.exitCode=2;}
finally{for(const signal of ['SIGTERM','SIGINT'])process.removeListener(signal,stop);}
