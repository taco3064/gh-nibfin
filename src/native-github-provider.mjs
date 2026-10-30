// GitHub may return numeric canonical pagination links for a named repository.
// Preserve the caller's original finite read guard: only an alias for its exact
// repository ID is projected back to that same named root for guard evaluation.
// Core still owns HTTP validation, pagination bounds, identities and permission.
import{nativeJitConfiguration}from'./native-jit.mjs';
export function nativeGithubProvider(core,configuration={}){
 const send=configuration.transport??core.localGhTransport();
 const response=async(request,value)=>{
  if(configuration.retainResponse)await configuration.retainResponse(request,value);
  const runners=/^\/repos\/([^/]+\/[^/]+)\/actions\/runners(?:\/[1-9][0-9]*|\/generate-jitconfig)?(?:\?|$)/.exec(request.endpoint);
  if(runners&&[200,201].includes(value?.status)&&typeof value.body==='string'){
   try{
    let changed=false;const parsed=JSON.parse(value.body,function(key,item,context){
     // JIT Runner labels are name-bearing placeholders with id 0, not
     // independently addressable resources. Do not invent a positive label ID.
     if(key==='id'&&item===0&&context.source==='0'&&this.type==='read-only'&&typeof this.name==='string'&&/^[A-Za-z0-9_.:-]{1,128}$/.test(this.name)){
      changed=true;return null;
     }
     return typeof item==='number'?JSON.rawJSON(context.source):item;
    });let body=changed?JSON.stringify(parsed):value.body;
    if(request.method==='POST'&&request.endpoint.endsWith('/generate-jitconfig')&&value.status===201)try{
     const data=core.parseGithubJson(body),requested=JSON.parse(request.body);
     if(data.runner?.ephemeral===undefined&&data.runner.name===requested.name){
      const jit=nativeJitConfiguration(data.encoded_jit_config,{runnerId:data.runner.id,runnerName:requested.name,request:{repository:runners[1]}});
      if(jit.settings.ephemeral===true){parsed.runner.ephemeral=true;body=JSON.stringify(parsed);changed=true;}
     }
    }catch{}
    if(changed)value={...value,body};
   }catch{}
  }
  if(request.method!=='GET'||!value)return value;
  // GitHub uses runner_id:0 while a queued Job has no assigned Runner.
  // Core's resource IDs remain strictly positive. Map
  // only this observed absence to null, retaining every other numeric spelling.
  if(value.status===200&&typeof value.body==='string'&&/^\/repos\/[^/]+\/[^/]+\/actions\/(?:jobs\/[1-9][0-9]*|runs\/[1-9][0-9]*\/(?:attempts\/[1-9][0-9]*\/)?jobs)(?:\?|$)/.test(request.endpoint)){
   try{let changed=false;const parsed=JSON.parse(value.body,function(key,item,context){
    if(key==='runner_id'&&item===0&&context.source==='0'&&this.status==='queued'&&this.runner_name===''){
     changed=true;return null;
    }
    return typeof item==='number'?JSON.rawJSON(context.source):item;
   });if(changed)value={...value,body:JSON.stringify(parsed)};}catch{}
  }
  if(!value.headers)return value;const headers={...value.headers};
  for(const key of Object.keys(headers))if(key.toLowerCase()==='link'&&typeof headers[key]==='string')headers[key]=headers[key].replace(/<([^>]+)>/g,(whole,uri)=>{
   if(!/%3d/i.test(uri))return whole;const candidate=uri.replace(/%3d/ig,'=');
   try{const before=new URL(uri),after=new URL(candidate);
    if(before.origin==='https://api.github.com'&&before.pathname===after.pathname&&before.hash===after.hash
     &&JSON.stringify([...before.searchParams])===JSON.stringify([...after.searchParams]))return '<'+candidate+'>';
   }catch{}return whole;
  });
  return {...value,headers};
 };
 const transport=async request=>response(request,await send(request));
 if(typeof send.prepareMutation==='function')Object.defineProperty(transport,'prepareMutation',{value:async options=>{
  const prepared=await send.prepareMutation(options);return async request=>response(request,await prepared(request));
 }});
 const provider=core.createGithubProvider({...configuration,transport:Object.freeze(transport)});
 return Object.freeze({...provider,collection(options){
  const named=typeof options.endpoint==='string'?/^(\/repos\/[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+)(?:\/|\?)/.exec(options.endpoint)?.[1]:null;
  const numeric=typeof options.target==='string'&&/^[1-9][0-9]*$/.test(options.target)?'/repositories/'+options.target+'/':null;
  const guard=options.allowPage;
  return provider.collection({...options,allowPage:path=>{
   const equivalent=named&&numeric&&path.startsWith(numeric)?named+'/'+path.slice(numeric.length):path;
   return guard?guard(equivalent):true;
  }});
 }});
}
