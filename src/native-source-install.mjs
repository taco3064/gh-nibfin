import fs from'node:fs/promises';import{join,dirname,resolve}from'node:path';import{execFileSync}from'node:child_process';
import{readPrivate,writePrivate,sha,privatePath}from'./local-state.mjs';import{publicCheckout}from'./native-role-host.mjs';
const need=(ok,code)=>{if(!ok)throw Error('native-source-install:'+code);};

// Owner init is an administrative source installation, not an Issue delivery.
// Reuse Core's workspace/Git byte owners without fabricating an Agent result,
// Accept receipt, Issue completion or permission to bypass protected PR checks.
export async function installNativeSource(core,{root,repository,repositoryId,desired,observations,previous,provider,signal,checkout=publicCheckout,publicationGitFactory=options=>core.createPublicationGit(options)}={}){
 const request=async(endpoint,{method='GET',body}={})=>provider.request({endpoint,method,...(body?{body}:{}),target:repositoryId,
  operation:'installation',correlation:'native-source-install',signal});
 const get=async path=>{const r=await request('/repos/'+repository+path);need(r.outcome==='read','observation');return r.data;};
 const repo=await get('');need(repo.id===repositoryId&&repo.private===false&&repo.permissions?.admin===true&&repo.default_branch==='main','owner');
 const member=await request('/user');need(member.outcome==='read'&&/^[1-9][0-9]*$/.test(member.data.id)&&/^[A-Za-z0-9-]+$/.test(member.data.login),'member');
 const files=Object.fromEntries([...desired.files].map(([path,body])=>[path,sha(body)]));
 const identity=core.contentIdentity({version:1,repository,repositoryId,files}),path=join(root,'managed-source-pr.json');
 let proposal=await readPrivate(path,{missing:true});
 if(proposal)need(proposal.identity===identity,'preserve-original-proposal');
 const differs=[...desired.files].some(([name,body])=>observations.get(name)?.state!=='present'||!observations.get(name).bytes.equals(Buffer.from(body)));
 if(!differs&&!proposal)return {status:'installed'};
 if(!proposal){
  for(const[name]of desired.files){const observed=observations.get(name);need(observed&&observed.state!=='unknown','source-observation');
   need(observed.state!=='present'||previous?.files?.[name]===sha(observed.bytes),'user-modification-preserved');}
  const base=(await get('/git/ref/heads/main')).object.sha;need(/^[a-f0-9]{40}$/.test(base),'base');
  proposal={version:1,kind:'administrative-installation',identity,repository,repositoryId,files,base,
   branch:'nibfin/install-'+identity.slice(0,24),createdAt:Date.now(),phase:'planning'};
  await writePrivate(path,proposal,{exclusive:true});
 }
 if(proposal.phase==='planning'){
  const base=proposal.base;let directory=proposal.directory,fresh=false;
  if(!directory){directory=await fs.mkdtemp(join(root,'.native-source-'));fresh=true;proposal={...proposal,directory};await writePrivate(path,proposal);}
  need(resolve(directory)===directory&&dirname(directory)===root&&directory.split('/').at(-1).startsWith('.native-source-'),'original-source-directory');
  await privatePath(directory,{directory:true});
  const git=args=>execFileSync('/usr/bin/git',['-c','core.hooksPath=/dev/null',...args],{cwd:directory,encoding:'utf8',timeout:30000,maxBuffer:1048576,
   env:{PATH:'/usr/bin:/bin',HOME:directory,LANG:'C',GIT_CONFIG_NOSYSTEM:'1',GIT_CONFIG_GLOBAL:'/dev/null',GIT_TERMINAL_PROMPT:'0'}}).trim();
  let publication,settled=false;
  try{
   if(fresh)await checkout(repository,base,directory,signal);
   need(git(['rev-parse','HEAD'])===base,'original-source-base');
   for(const[name,body]of desired.files){
    need(/^(?:\.github\/workflows\/nibfin-[a-z-]+\.yml|\.nibfin\/[A-Za-z0-9_./-]+)$/.test(name)&&!name.split('/').includes('..'),'managed-path');
    let parent=directory;for(const part of name.split('/').slice(0,-1)){parent=join(parent,part);
     try{const s=await fs.lstat(parent);need(s.isDirectory()&&!s.isSymbolicLink(),'source-parent');}catch(e){if(e.code!=='ENOENT')throw e;await fs.mkdir(parent,{mode:0o700});}}
    const destination=join(directory,name),observed=observations.get(name);let before=null;
    try{const s=await fs.lstat(destination);need(s.isFile()&&!s.isSymbolicLink()&&s.nlink===1,'source-file');before=await fs.readFile(destination);}catch(e){if(e.code!=='ENOENT')throw e;}
    need(before?.equals(Buffer.from(body))||(observed.state==='absent'?before===null:before?.equals(observed.bytes)),'source-drift');
    const h=await fs.open(destination,fs.constants.O_WRONLY|fs.constants.O_CREAT|fs.constants.O_TRUNC|fs.constants.O_NOFOLLOW,0o600);
    try{await h.writeFile(body);await h.sync();}finally{await h.close();}
   }
   const scopeBody='Owner init: install the reviewed Nibfin managed source. This is administrative source data, not an Issue delivery.',
    shapeBody=['Candidate artifacts applicability: Only managed tracked source is installed; no generated artifacts are required.',
     'Candidate hooks applicability: Owner installation does not execute repository hooks.','```nibfin-candidate','{"artifacts":[],"hooks":[]}','```'].join('\n');
   const roots=git(['ls-tree','--name-only',base]).split('\n').filter(Boolean);
   const permittedPaths=[...new Set([...roots,...[...desired.files.keys()].map(p=>p.split('/')[0])])];
   need(permittedPaths.every(p=>/^[A-Za-z0-9_.-]+$/.test(p)&&!['.','..','.git'].includes(p)),'source-boundary');
   const workspace=core.captureWorkspace({root:directory,base,scopeBody,shapeBody,permittedPaths,
    binding:{repository,cycle:'install-'+identity.slice(0,32),policy:identity,scopeHash:core.bodyDigest(scopeBody),shapeHash:core.bodyDigest(shapeBody)}});
   need(workspace.candidate.paths.every(path=>desired.files.has(path)),'unmanaged-source-change');
   publication=publicationGitFactory({repository,repositoryId}).open(workspace,Buffer.from('Install reviewed Nibfin native project source\n'),
    {name:member.data.login,email:member.data.id+'+'+member.data.login+'@users.noreply.github.com',timestamp:Math.floor(proposal.createdAt/1000)},{signal});
   proposal={...proposal,phase:'push-intent',commit:publication.commit};await writePrivate(path,proposal);
   const refs={head:proposal.branch,base:'main'},observed=publication.observe(refs);need(observed.base===base&&observed.head===null,'branch-conflict');
   try{publication.push(refs);}catch{/* The readback resolves the original exact commit. */}
   need(publication.observe(refs).head===proposal.commit,'push-reconciliation-required');
   publication.close();publication=null;settled=true;proposal={...proposal,phase:'branch'};delete proposal.directory;await writePrivate(path,proposal);
  }finally{
   if(publication)try{publication.close();}catch{settled=false;}
   // Preserve an interrupted source acquisition/push until its processes and
   // original effect are reconciled; never replace it with a new proposal.
   if(settled)await fs.rm(directory,{recursive:true});
  }
 }
 need(['branch','pr-intent','pr','merge-intent','merged','installed'].includes(proposal.phase),'original-proposal-reconciliation-required');
 if(proposal.phase==='installed')return {status:'installed',pr:proposal.pr.url};
 if(!proposal.pr){
  const endpoint='/repos/'+repository+'/pulls?state=all&per_page=100';
  const observePRs=async()=>{const result=await provider.collection({endpoint,target:repositoryId,operation:'installation',correlation:'native-source-install',signal,allowPage:p=>p.split('?')[0]===endpoint.split('?')[0]});
   need(result.outcome==='complete'&&Array.isArray(result.data),'pr-observation');return {data:result.data.filter(pr=>pr.head?.ref===proposal.branch&&pr.head?.repo?.id===repositoryId&&pr.base?.ref==='main')};};
  let list=await observePRs();
  if(list.data.length===0){
   need(proposal.phase==='branch','original-pr-reconciliation-required');proposal={...proposal,phase:'pr-intent'};await writePrivate(path,proposal);
   await request('/repos/'+repository+'/pulls',{method:'POST',body:{title:'Install native Nibfin project execution',head:proposal.branch,base:'main',
    body:'Install the reviewed project-local Nibfin workflow, Skills and runtime policy. Shared physical Runner capacity remains independent of this repository.\n\nManaged installation: '+identity}});
   list=await observePRs();
  }
  need(list.data.length===1&&list.data[0].head?.sha===proposal.commit&&list.data[0].head?.ref===proposal.branch
   &&list.data[0].head.repo?.id===repositoryId&&list.data[0].base?.ref==='main','original-pr-reconciliation-required');
  const pr=list.data[0];proposal={...proposal,phase:'pr',pr:{id:pr.id,number:String(pr.number),url:pr.html_url}};await writePrivate(path,proposal);
 }
 let pr=await get('/pulls/'+proposal.pr.number);need(pr.id===proposal.pr.id&&pr.head?.sha===proposal.commit,'pr-drift');
 if(!pr.merged){
  need(pr.state==='open'&&pr.draft===false,'pr-not-open');
  const checks=await core.observeGithubCI({provider,expected:{repository,repositoryId,prId:proposal.pr.id,pr:Number(proposal.pr.number),
   head:proposal.commit,baseSha:pr.base.sha,baseRepositoryId:repositoryId,baseBranch:'main'}});
  if(!checks.qualifies||checks.status!=='satisfied')return {status:'pending',pr:proposal.pr.url,reason:'protected-installation-ci-'+checks.status};
  const protection=await get('/branches/main/protection');
  need(protection.enforce_admins?.enabled===true&&protection.required_status_checks?.strict===true&&checks.policy.requirements.length>0,'protected-pr-required');
  proposal={...proposal,phase:'merge-intent',ci:checks};await writePrivate(path,proposal);
  await request('/repos/'+repository+'/pulls/'+proposal.pr.number+'/merge',{method:'PUT',body:{sha:proposal.commit,merge_method:'rebase'}});
  pr=await get('/pulls/'+proposal.pr.number);
 }
 need(pr.merged===true&&/^[a-f0-9]{40}$/.test(pr.merge_commit_sha),'merge-reconciliation-required');
 proposal={...proposal,phase:'installed',mergedCommit:pr.merge_commit_sha};await writePrivate(path,proposal);
 return {status:'installed',pr:proposal.pr.url};
}
