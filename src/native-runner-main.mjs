// Internal fixed systemd entry, installed and invoked only by the maintained host.
import {runNativeRunner} from './native-runner-process.mjs';
const controller=new AbortController(),stop=()=>controller.abort();
for(const signal of ['SIGTERM','SIGINT'])process.once(signal,stop);
try{
 if(process.argv.length!==4)throw Error();
 const result=await runNativeRunner(process.argv[2],process.argv[3],{signal:controller.signal});
 if(result.status!=='terminated')process.exitCode=2;
}catch(error){
 const code=/^(?:native-runner|native-jit|native-runner-service):[a-z-]+$/.test(error?.message??'')?error.message:'native-runner:unconfirmed';
 process.stderr.write(code+'; retain the original slot and work.\n');process.exitCode=2;
}
finally{for(const signal of ['SIGTERM','SIGINT'])process.removeListener(signal,stop);}
