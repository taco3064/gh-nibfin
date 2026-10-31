import fs from'node:fs/promises';import{join}from'node:path';
const need=(v,c)=>{if(!v)throw Error('native-clean-base:'+c);};

// The upstream repair owner prepares the current base before restoring edits.
// This narrow branch handles a retained Writer with no edits. Dirty work must
// go through a real repair owner; it is never overwritten or called accepted.
export async function prepareNativeCleanBase(core,{directory,sourceDirectory,base,state,snapshot,reference,retain}){
 const head=(await fs.readFile(join(directory,'.git/HEAD'),'utf8')).trim();
 need(/^[a-f0-9]{40}$/.test(head),'detached-original-required');if(head===base)return;
 need(snapshot.kind==='native-unaccepted-workspace'&&snapshot.status==='UNACCEPTED'&&reference&&typeof retain==='function','original-custody');
 const input={permittedPaths:state.paths,scopeBody:state.scopeBody,shapeBody:state.shapeBody,
  binding:{repository:state.target.repository,cycle:state.cycle,policy:core.contentIdentity(state.policy),
   scopeHash:state.scopeHash,shapeHash:core.bodyDigest(state.shapeBody)}};
 const original=core.captureWorkspace({...input,root:directory,base:head});
 need(original.diff.length===0&&original.sources.length===0,'unaccepted-edits-require-repair');
 const objects=new Map(original.objects.map(o=>[o.oid,o])),tracked=new Set();
 function tree(id,prefix=''){
  const bytes=objects.get(id)?.bytes;need(bytes,'tree-object');let at=0;
  while(at<bytes.length){const space=bytes.indexOf(32,at),nul=bytes.indexOf(0,space),mode=bytes.subarray(at,space).toString(),name=bytes.subarray(space+1,nul).toString(),oid=bytes.subarray(nul+1,nul+21).toString('hex'),path=prefix+name;
   if(mode==='40000')tree(oid,path+'/');else tracked.add(path);at=nul+21;
  }
 }
 tree(original.metadata.tree);
 need(snapshot.entries.every(e=>e.kind==='directory'||e.path.startsWith('.git/')||tracked.has(e.path)),'untracked-bytes-require-repair');
 const fresh=core.captureWorkspace({...input,root:sourceDirectory,base});
 need(fresh.diff.length===0&&fresh.sources.length===0,'fresh-source-changed');
 const saved=await fs.mkdtemp(directory+'-original-base-');await fs.rename(directory,saved);
 try{
  await fs.cp(sourceDirectory,directory,{recursive:true,dereference:false,verbatimSymlinks:true});
  const copied=core.captureWorkspace({...input,root:directory,base});
  need(core.canonicalJSON(copied.expected)===core.canonicalJSON(fresh.expected),'copy-changed');
  await retain({kind:'native-clean-workspace-base-transition',status:'UNACCEPTED',execution:snapshot.execution.id,
   original:reference,oldBase:head,base,previous:original.expected,current:copied.expected,accepted:false});
 }catch(error){await fs.rm(directory,{recursive:true,force:true});await fs.rename(saved,directory);throw error;}
 // Exact original bytes already remain in immutable private custody. This
 // extra local copy has served rollback of the preparation operation only.
 await fs.rm(saved,{recursive:true});
}
