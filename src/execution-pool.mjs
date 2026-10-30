// Owner installation input only. Repository runtime files and workflow/event
// payloads are descriptive data, never sources of protected pool membership.
export async function observeExecutionPool(core,{selection,previous,repo,provider,memberId,signal}={}){
  const reject=code=>{const error=Error('Execution pool: '+code);error.code=code;throw error;};
  const normalize=value=>{
    if(value?.version!==2)reject('legacy-central-pool-reconciliation-required');
    const valid=core.validateExecutionPool(value);
    return core.validateExecutionPool({...valid,targets:[...valid.targets].sort((a,b)=>a.repository.localeCompare(b.repository,'en'))});
  };
  let prior;
  if(previous!==undefined)try{prior=normalize(previous);}catch{reject('invalid-protected-pool');}
  if(!selection&&!prior)return undefined;
  if(selection?.source!==undefined)reject('central-execution-repository-not-supported');
  const names=selection?.targets??prior.targets.map(t=>t.repository);
  const id=selection?.id??prior?.id,capacity=selection?.capacity??prior?.capacity;
  if(typeof id!=='string'||!/^[a-z][a-z0-9-]{0,47}$/.test(id)||!Number.isSafeInteger(capacity)||capacity<1||capacity>128
    ||!Array.isArray(names)||!names.length||names.length>128||new Set(names).size!==names.length||!names.includes(repo))reject('invalid-pool-selection');
  for(const name of names){
    try{if(typeof name!=='string'||name.split('/').length!==2||core.repositoryCoordinates(...name.split('/')).key!==name
      )throw Error();}catch{reject('invalid-pool-selection');}
  }
  const identities=[];
  for(const name of [...names].sort()){
    const result=await provider.request({endpoint:'/repos/'+name,target:memberId,operation:'installation',correlation:'execution-pool',signal});
    const value=result.data;
    if(result.outcome!=='read'||value?.private!==false||value.visibility!=='public'||value.full_name?.toLowerCase()!==name
      ||!/^([1-9][0-9]*)$/.test(value.id??'')||!/^([1-9][0-9]*)$/.test(value.owner?.id??'')||!['User','Organization'].includes(value.owner?.type))reject('pool-identity-unavailable');
    if(value.permissions?.admin!==true)reject('pool-repository-administration-required');
    identities.push({version:1,repository:name,repositoryId:value.id,ownerId:value.owner.id,ownerType:value.owner.type});
  }
  let pool;
  try{pool=normalize({version:2,id,capacity,targets:identities});}
  catch{reject('pool-owner-or-identity-mismatch');}
  if(prior&&JSON.stringify(prior)!==JSON.stringify(pool))reject('protected-pool-transition-required');
  return pool;
}
