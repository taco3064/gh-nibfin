// GitHub may return numeric canonical pagination links for a named repository.
// Preserve the caller's original finite read guard: only an alias for its exact
// repository ID is projected back to that same named root for guard evaluation.
// Core still owns HTTP validation, pagination bounds, identities and permission.
export function nativeGithubProvider(core,configuration={}){
 const provider=core.createGithubProvider(configuration);
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
