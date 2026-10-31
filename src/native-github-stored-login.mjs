import{spawn}from'node:child_process';import{tmpdir,userInfo}from'node:os';
// Compatibility with the installed Core's stored-login sender: retain its
// credential boundary, API version, bounded subprocess and request-local auth
// preparation. That older sender ignores supplied headers; forward ONLY the
// conditional GET header needed by the upstream ETag observation contract.
export function nativeStoredGithubTransport(core,{executable='gh'}={}){
 const environment=()=>({PATH:process.env.PATH,HOME:userInfo().homedir,GH_PROMPT_DISABLED:'1',GH_NO_UPDATE_NOTIFIER:'1',GH_NO_EXTENSION_UPDATE_NOTIFIER:'1',NO_COLOR:'1'});
 const run=(args,input,signal,env)=>new Promise((resolve,reject)=>{
  const child=spawn(executable,args,{cwd:tmpdir(),env,shell:false,signal,stdio:['pipe','pipe','pipe']});let output='',size=0;
  child.stdout.setEncoding('utf8');child.stdout.on('data',chunk=>{size+=Buffer.byteLength(chunk);if(size>8388608)child.kill();else output+=chunk;});
  child.stderr.resume();child.once('error',()=>reject(Error('native-github:process')));child.once('close',code=>resolve({code,output,overflow:size>8388608}));
  child.stdin.on('error',()=>{});child.stdin.end(input);
 });
 const dispatch=async(request,env)=>{
  const args=['api','--hostname','github.com','--include','--method',request.method,'--header','Content-Type: application/json','--header','Accept: application/vnd.github+json','--header','X-GitHub-Api-Version: '+core.githubApiVersion];
  const etag=Object.entries(request.headers??{}).find(([k])=>k.toLowerCase()==='if-none-match')?.[1];
  if(request.method==='GET'&&typeof etag==='string'&&etag.length<=1024&&!/[\x00-\x1f\x7f]/.test(etag))args.push('--header','If-None-Match: '+etag);
  if(request.body!==undefined)args.push('--input','-');args.push('https://api.github.com'+request.endpoint);
  const value=await run(args,request.body??'',request.signal,env);if(value.overflow)return{failure:'malformed'};
  const match=value.output.match(/^HTTP\/\S+ (\d{3})[^\r\n]*\r?\n((?:[^\r\n]+\r?\n)*)\r?\n([\s\S]*)$/);if(!match)return{failure:'transport'};
  const headers={};for(const line of match[2].split(/\r?\n/).filter(Boolean)){const at=line.indexOf(':');if(at<1)return{failure:'malformed'};headers[line.slice(0,at).toLowerCase()]=line.slice(at+1).trim();}
  if(value.code!==0&&Number(match[1])<400&&Number(match[1])!==304)return{failure:'transport'};return{status:Number(match[1]),headers,body:match[3]};
 };
 const prepare=async({signal}={})=>{const env=environment(),auth=await run(['auth','status','--hostname','github.com'],'',signal,env);
  return auth.code===0?request=>dispatch(request,env):()=>({failure:'authentication'});
 };
 // Each GET authenticates at GitHub; Core's /user checks bind that live
 // identity. Avoid a second network auth-status check for every read. Retain
 // request-local authentication preparation before every actual mutation.
 const send=async request=>request.method==='GET'?dispatch(request,environment()):(await prepare({signal:request.signal}))(request);
 Object.defineProperty(send,'prepareMutation',{value:prepare});return Object.freeze(send);
}
