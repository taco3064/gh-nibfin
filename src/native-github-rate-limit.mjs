import{join}from'node:path';import{readPrivate,writePrivate}from'./local-state.mjs';
// A response cooldown is a denial hint, never execution authority. Keep one
// local reset boundary instead of polling GitHub or scheduling waiting Actions.
export async function nativeGithubRateLimit(root,{now=Date.now()}={}){
 const value=await readPrivate(join(root,'github-rate-limit.json'),{missing:true});
 return value?.version===1&&Number.isSafeInteger(value.resetAt)&&value.resetAt>now?value:null;
}
let tail=Promise.resolve();
export function retainNativeGithubRateLimit(root,response,options={}){
 const next=tail.then(()=>retain(root,response,options));tail=next.catch(()=>{});return next;
}
async function retain(root,response,{now=Date.now()}={}){
 const h=Object.fromEntries(Object.entries(response?.headers??{}).map(([k,v])=>[k.toLowerCase(),String(v)]));let resetAt,resource;
 if(h['x-ratelimit-remaining']==='0'&&/^\d+$/.test(h['x-ratelimit-reset']??'')){
  resetAt=Number(h['x-ratelimit-reset'])*1000;resource=h['x-ratelimit-resource']==='core'?'core':'github';
 }else if([403,429].includes(response?.status)&&/^\d+$/.test(h['retry-after']??'')){
  resetAt=now+Number(h['retry-after'])*1000;resource='secondary';
 }
 if(!Number.isSafeInteger(resetAt)||resetAt<=now||resetAt>now+86400000)return;
 const path=join(root,'github-rate-limit.json'),old=await readPrivate(path,{missing:true});
 if(old?.resetAt>=resetAt)return;
 await writePrivate(path,{version:1,resource,resetAt,observedAt:now,reason:'github-api-rate-limit'});
}
