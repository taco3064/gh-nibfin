import{writePrivate}from'./local-state.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-source-refresh:'+code);};
// Administrative init only. Keep the original PR and managed-file intent when
// protected main advances. GitHub performs a CAS-guarded branch update; fresh
// parentage, complete diff and every managed byte must verify before new CI.
export async function refreshNativeSource(core,{proposal,pr,path,desired,repository,repositoryId,provider,signal}={}){
 const request=(suffix,options={})=>provider.request({endpoint:'/repos/'+repository+suffix,target:repositoryId,operation:'installation',correlation:'native-source-refresh',signal,...options});
 const get=async suffix=>{const value=await request(suffix);need(value.outcome==='read','observation');return value.data;};
 const save=async value=>{await writePrivate(path,value);proposal=value;};
 const main=(await get('/git/ref/heads/main')).object.sha;
 if(!proposal.refresh&&main===proposal.base)return{proposal,pr};
 need(pr.id===proposal.pr.id&&pr.state==='open'&&!pr.merged&&pr.draft===false&&pr.head?.repo?.id===repositoryId&&pr.head.ref===proposal.branch,'original-pr');
 if(!proposal.refresh){
  need(pr.head.sha===proposal.commit,'original-head');
  const change=await get('/compare/'+proposal.base+'...'+main);
  need(change.base_commit?.sha===proposal.base&&change.merge_base_commit?.sha===proposal.base&&change.status==='ahead'
   &&Array.isArray(change.files)&&change.files.length<300&&change.files.every(f=>!desired.files.has(f.filename)&&!desired.files.has(f.previous_filename)),'managed-base-conflict');
  const latest=await get('/pulls/'+proposal.pr.number);need(latest.head?.sha===proposal.commit&&(await get('/git/ref/heads/main')).object.sha===main,'fresh-base');
  const refresh={version:1,fromBase:proposal.base,fromHead:proposal.commit,toBase:main,submittedAt:Date.now()};
  await save({...proposal,phase:'refresh-intent',refresh:{...refresh,identity:core.contentIdentity(refresh)}});
  const result=await request('/pulls/'+proposal.pr.number+'/update-branch',{method:'PUT',body:{expected_head_sha:proposal.commit}});
  const value={...refresh,submission:{outcome:result.outcome,kind:result.diagnostic?.kind??null,status:result.diagnostic?.status??null}};
  await save({...proposal,refresh:{...value,identity:core.contentIdentity(value)}});
  pr=await get('/pulls/'+proposal.pr.number);
 }
 const{identity,...refresh}=proposal.refresh;
 need(core.contentIdentity(refresh)===identity&&refresh.version===1&&refresh.fromBase===proposal.base&&refresh.fromHead===proposal.commit,'original-refresh');
 if(pr.head.sha===refresh.fromHead)return{proposal,pr,pending:true};
 const commit=await get('/commits/'+pr.head.sha),comparison=await get('/compare/'+refresh.toBase+'...'+pr.head.sha);
 need(commit.sha===pr.head.sha&&Array.isArray(commit.parents)&&commit.parents.length===2&&commit.parents[0].sha===refresh.fromHead&&commit.parents[1].sha===refresh.toBase,'parentage');
 need(comparison.base_commit?.sha===refresh.toBase&&comparison.merge_base_commit?.sha===refresh.toBase&&comparison.status==='ahead'
  &&Array.isArray(comparison.files)&&comparison.files.length>0&&comparison.files.length<=desired.files.size
  &&comparison.files.every(f=>desired.files.has(f.filename)&&['added','modified'].includes(f.status)&&!f.previous_filename),'managed-diff');
 for(const[name,body]of desired.files){const file=await get('/contents/'+name+'?ref='+pr.head.sha);
  need(file.type==='file'&&file.path===name&&file.encoding==='base64'&&Buffer.from(file.content,'base64').equals(Buffer.from(body)),'managed-bytes');}
 const revision={...proposal.refresh,verifiedHead:pr.head.sha,readback:core.contentIdentity({commit,comparison}),verifiedAt:Date.now()};
 const next={...proposal,phase:'pr',base:refresh.toBase,commit:pr.head.sha,revisions:[...(proposal.revisions??[]),revision]};delete next.refresh;delete next.ci;
 await save(next);return{proposal,pr,pending:(await get('/git/ref/heads/main')).object.sha!==refresh.toBase};
}
