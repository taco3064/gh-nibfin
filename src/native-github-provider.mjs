// GitHub may return numeric canonical pagination links for a named repository.
// Preserve the caller's original finite read guard: only an alias for its exact
// repository ID is projected back to that same named root for guard evaluation.
// Core still owns HTTP validation, pagination bounds, identities and permission.
export function nativeGithubProvider(core,configuration={}){
 const send=configuration.transport??core.localGhTransport();
 const response=(request,value)=>{
  if(request.method!=='GET'||!value?.headers)return value;const headers={...value.headers};
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
