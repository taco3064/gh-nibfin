import fs from 'node:fs/promises';
import {createReadStream} from 'node:fs';
import {createHash} from 'node:crypto';
import {join} from 'node:path';
import {tmpdir} from 'node:os';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {safePath} from './register.mjs';
export const runnerDistribution = Object.freeze({version:'2.331.0',commit:'39f6c643d3032f8795fa71f1482731ca90c8e038',
  sha256:'5fcc01bd546ba5c3f1291c2803658ebd3cedb3836489eda3be357d41bfcf28a7',
  archive:'actions-runner-linux-x64-2.331.0.tar.gz'});
const hash = async path => {
  const h=createHash('sha256');for await(const b of createReadStream(path))h.update(b);return h.digest('hex');
};
// Verify the immutable official archive BEFORE extraction; compare every shipped
// file to the application actually invoked. No download, installer or lifecycle.
export async function verifyRunnerDistribution(directory,{signal}={}) {
  let scratch;
  try {
    if(process.platform!=='linux' || process.arch!=='x64')throw Error();
    const archive=join(directory,runnerDistribution.archive);await safePath(archive,{directory:false});
    if((await fs.stat(archive)).size>500000000)throw Error();
    scratch=await fs.mkdtemp(join(tmpdir(),'nibfin-official-runner-'));
    const snapshot=join(scratch,'distribution.tgz');await fs.copyFile(archive,snapshot);
    if(await hash(snapshot)!==runnerDistribution.sha256)throw Error();
    const extracted=join(scratch,'extracted');await fs.mkdir(extracted);
    await promisify(execFile)('tar',['--extract','--gzip','--file',snapshot,'--directory',extracted,'--no-same-owner'],
      {signal,timeout:30000,maxBuffer:4096,env:{PATH:process.env.PATH,LANG:'C'}});
    const compare=async relative=>{
      for(const entry of await fs.readdir(join(extracted,relative),{withFileTypes:true})) {
        const name=join(relative,entry.name),expected=join(extracted,name),actual=join(directory,name);
        if(entry.isDirectory()){await safePath(actual);await compare(name);}
        else if(entry.isFile()){await safePath(actual,{directory:false});if(await hash(expected)!==await hash(actual))throw Error();}
        else throw Error();
      }
    };
    await compare('');return runnerDistribution;
  } catch {throw Error('Automatic registration requires the pinned official Linux x64 Runner v2.331.0 archive, matching extracted application bytes, and local tar. No registration was requested; prepare the reviewed distribution or use the authorized manual route.');}
  finally {if(scratch)await fs.rm(scratch,{recursive:true,force:true});}
}
