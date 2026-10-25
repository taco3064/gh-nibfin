import fs from 'node:fs/promises';
import {join} from 'node:path';
import {userInfo} from 'node:os';
import {spawn} from 'node:child_process';
import {createHash,generateKeyPairSync,randomUUID} from 'node:crypto';
import {createInterface} from 'node:readline/promises';
import {setTimeout as delay} from 'node:timers/promises';
import {safePath} from './register.mjs';
const sha = b => createHash('sha256').update(b).digest('hex');
const id = v => typeof v === 'string' && /^[1-9][0-9]*$/.test(v);
const fail = () => {throw Error('Automatic registration unavailable or unconfirmed; reconcile private binding/request state before retry. Use official manual registration with an authorized manager if protected integration is unavailable.');};
export function automaticBinding(b) {
  return b.method === 'auto' && b.configuration?.state === 'completed' && b.configuration.runnerId === b.runnerId &&
    id(b.runnerId) && b.remote?.state === 'unknown' && b.status === null && b.busy === null;
}
export async function confirmRegistration(message) {
  if (!process.stdin.isTTY || !process.stdout.isTTY) return false;
  const rl = createInterface({input:process.stdin,output:process.stdout});
  try {return (await rl.question(message+' [y/N] ')).trim().toLowerCase() === 'y';}
  finally {rl.close();}
}
// Owner-approved local installation contract, never downloaded response authority.
export async function automaticConfiguration(repository, repositoryId) {
  const path = join(userInfo().homedir,'.config/nibfin/registration.json');
  await safePath(path,{directory:false});
  const stat = await fs.stat(path); if (stat.size > 16384 || stat.mode & 0o077) fail();
  const c = JSON.parse(await fs.readFile(path,'utf8'));
  if (c.contract !== 'n17-dispatch-v1' || c.repository !== repository || c.repositoryId !== repositoryId ||
      !id(c.workflowId) || !/^\.github\/workflows\/[a-zA-Z0-9_-]+\.ya?ml$/.test(c.path) ||
      !/^[a-zA-Z0-9_-]{1,80}$/.test(c.ref) || !/^[a-f0-9]{40}$/.test(c.headSha) || !/^[a-f0-9]{64}$/.test(c.workflowSha256)) fail();
  return c;
}
// gh owns authenticated API redirects; capture bounded binary bytes, discard stderr.
// Stored execution-account login only, no checkout configuration or Actions token.
export function registrationArchive({endpoint,signal,maxBytes}) {
  if (!/^\/repos\/[a-z0-9-]+\/[a-z0-9_.-]+\/actions\/artifacts\/[1-9][0-9]*\/zip$/.test(endpoint)) fail();
  return new Promise((resolve,reject) => {
    const env = {PATH:process.env.PATH,HOME:userInfo().homedir,GH_PROMPT_DISABLED:'1',GH_NO_UPDATE_NOTIFIER:'1'};
    const child = spawn('gh',['api','--hostname','github.com','--header','Accept: application/vnd.github+json',
      '--header','X-GitHub-Api-Version: 2026-03-10','https://api.github.com'+endpoint],{env,stdio:['ignore','pipe','ignore'],signal});
    const chunks=[];let size=0,overflow=false;
    child.stdout.on('data',b=>{size+=b.length;if(size>maxBytes){overflow=true;child.kill('SIGKILL');}else chunks.push(b);});
    child.on('error',()=>reject(Error('Artifact download unavailable')));
    child.on('close',code=>code===0 && !overflow?resolve({status:200,bytes:Buffer.concat(chunks)}):reject(Error('Artifact download unavailable')));
  });
}
export function validateSource(run,c,requestId,member) {
  if (!id(run?.id) || run.run_attempt !== 1 || run.workflow_id !== c.workflowId || run.path !== c.path ||
      run.head_sha !== c.headSha || run.head_branch !== c.ref || run.event !== 'workflow_dispatch' ||
      run.display_title !== requestId || run.actor?.id !== member || run.triggering_actor?.id !== member ||
      run.repository?.id !== c.repositoryId || run.repository?.full_name?.toLowerCase() !== c.repository) fail();
  return run;
}
export async function requestAutomatic(core,{provider,repository,repositoryId,member,instance,state,publish,signal,
  configuration=automaticConfiguration,confirm=confirmRegistration,archive=registrationArchive,wait=delay,now=Date.now,waitMs=120000}) {
  let privateKey;
  try {
    const c = await configuration(repository,repositoryId);
    // Validate injected configurations by the same shape as the installation file.
    if (c.repository !== repository || c.repositoryId !== repositoryId || c.contract !== 'n17-dispatch-v1' ||
        !id(c.workflowId) || !/^\.github\/workflows\/[a-zA-Z0-9_-]+\.ya?ml$/.test(c.path) ||
        !/^[a-zA-Z0-9_-]{1,80}$/.test(c.ref) || !/^[a-f0-9]{40}$/.test(c.headSha) || !/^[a-f0-9]{64}$/.test(c.workflowSha256)) fail();
    const root = `/repos/${repository}`, dispatch = `${root}/actions/workflows/${c.workflowId}/dispatches`;
    const read = async endpoint => {
      const r = await provider.request({endpoint,target:repositoryId,operation:'auto_read',correlation:'auto',signal});
      if (r.outcome !== 'read') fail();return r.data;
    };
    const workflow = await read(`${root}/actions/workflows/${c.workflowId}`);
    const ref = await read(`${root}/git/ref/heads/${c.ref}`);
    const source = await read(`${root}/contents/${c.path}?ref=${c.headSha}`);
    if (workflow.id !== c.workflowId || workflow.path !== c.path || workflow.state !== 'active' || ref.object?.sha !== c.headSha || ref.object.type !== 'commit' ||
        source.encoding !== 'base64' || typeof source.content !== 'string' || source.content.length > 100000 ||
        sha(Buffer.from(source.content,'base64')) !== c.workflowSha256) fail();
    if (!await confirm(`Register existing instance ${instance.id} at https://github.com/${repository} (repository level, Owner-enabled policy)?`))
      return {cancelled:true};
    const keys = generateKeyPairSync('rsa',{modulusLength:2048});privateKey=keys.privateKey;
    const publicKey = keys.publicKey.export({type:'spki',format:'pem'}), requestId=randomUUID();
    const recipient=sha(keys.publicKey.export({type:'spki',format:'der'}));
    state.request={requestId,workflowId:c.workflowId,headSha:c.headSha,phase:'dispatching'};
    await publish(state); // Durable uncertainty blocks redispatch even after response loss.
    const sent = await provider.request({method:'POST',endpoint:dispatch,target:repositoryId,operation:'auto_dispatch',correlation:requestId,
      body:{ref:c.ref,inputs:{requestId,level:'repository',publicKey}},signal});
    if (sent.outcome !== 'submitted') fail();
    state.request.phase='waiting';await publish(state);
    process.stdout.write(`Waiting up to ${Math.ceil(waitMs/1000)} seconds for the protected registration response.\n`);
    const deadline=now()+waitMs;
    const readArtifact=core.registrationArtifactReader({provider,archive,now});
    while (!signal.aborted && now()<deadline) {
      const runs = await provider.collection({endpoint:`${root}/actions/workflows/${c.workflowId}/runs?event=workflow_dispatch&per_page=100`,
        envelope:'workflow_runs',target:repositoryId,operation:'auto_wait',correlation:requestId,signal,
        allowPage:path=>{
          const u=new URL(path,'https://api.github.com');
          return u.origin==='https://api.github.com' && u.pathname===`${root}/actions/workflows/${c.workflowId}/runs` &&
            [...u.searchParams.keys()].every(k=>['event','per_page','page'].includes(k)) &&
            u.searchParams.get('event')==='workflow_dispatch' && u.searchParams.get('per_page')==='100';
        }});
      if (runs.outcome !== 'complete') fail();
      const matches=runs.data.filter(r=>r.display_title===requestId);
      if (matches.length>1) fail();
      if (matches.length===1) {
        const listed=validateSource(matches[0],c,requestId,member);
        const run=validateSource(await read(`${root}/actions/runs/${listed.id}`),c,requestId,member);
        if(run.id !== listed.id) fail();
        state.request.runId=run.id;state.request.attempt='1';await publish(state);
        if (run.status==='completed' && run.conclusion!=='success') fail();
        if (run.status==='completed') {
          const expected={requestId,requesterId:member,repository,repositoryId,level:'repository',registrationId:repositoryId,
            runId:run.id,attempt:'1',recipient};
          let encrypted;
          try {encrypted=await readArtifact({ref:{repository,repositoryId,runId:run.id,attempt:'1',
            name:`registration-${requestId}-1`,artifactId:null},signal});}
          catch { // Publication may lag run completion; bounded reads only, never redispatch.
            await wait(1000,undefined,{signal});continue;
          }
          const opened=core.openRegistrationResponse(encrypted.bytes,{privateKey,expected,now});
          if (opened.outcome!=='available') fail();
          state.request.phase='consumed';await publish(state);
          return opened;
        }
      }
      await wait(1000,undefined,{signal});
    }
    fail();
  } catch {fail();}
  finally {privateKey=undefined;}
}
