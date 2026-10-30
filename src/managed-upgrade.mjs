import fs from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {join,resolve,relative} from 'node:path';
import {userInfo} from 'node:os';
import {withCurrentCore} from './current-core.mjs';
import {readPrivate,privatePath} from './local-state.mjs';
import {openFileHostStore} from './host-store.mjs';
import {coordinateUpgrade,rollbackUpgrade,upgradeOptions} from './upgrade.mjs';

const need=(value,code)=>{if(!value)throw Error('managed-upgrade:'+code);};
const sha=bytes=>createHash('sha256').update(bytes).digest('hex');
export const nativeHostRoot=()=>join(userInfo().homedir,'.local/share/nibfin/control');
const relativePath=value=>typeof value==='string'&&value.length<1024&&!value.includes('\\')
  &&value.split('/').every(p=>p!=='.'&&p!=='..'&&/^[a-zA-Z0-9_.@+-]+$/.test(p));

/** Private, finite native installation layout. A plan cannot supply paths,
 * commands, service names or executable JavaScript. Its inventories originate
 * in the maintained installer after authenticating its package distributions. */
export function managedUpgradeLayout(core,root,plan){
  need(resolve(root)===root,'root');
  need(plan&&Object.keys(plan).sort().join(' ')==='current identity next resources version'&&plan.version===1,'plan-schema');
  const {identity,...body}=plan;need(core.contentIdentity(body)===identity,'plan-integrity');
  need(Array.isArray(plan.resources)&&plan.resources.length>=2&&plan.resources.length<=129,'resource-set');
  const names=['runtime',...Array.from({length:plan.resources.length-1},(_,i)=>'runner-'+(i+1))];
  const services=['nibfin-control.service',...names.slice(1).map(n=>'nibfin-'+n+'.service')];
  need(plan.resources.every((r,i)=>r.name===names[i]&&Object.keys(r).sort().join(' ')==='current name next'),'resource-set');
  const inventories=new Map();
  for(const resource of plan.resources){
    for(const version of ['current','next']){
      const inventory=resource[version];need(Array.isArray(inventory)&&inventory.length>0&&inventory.length<=10000,'inventory');
      const paths=new Set();
      for(const f of inventory){
        need(f&&['file','link'].includes(f.kind)&&relativePath(f.path)&&!paths.has(f.path)
          &&/^[a-f0-9]{64}$/.test(f.sha256),'inventory-entry');paths.add(f.path);
        if(f.kind==='file')need(Object.keys(f).sort().join(' ')==='kind mode path sha256'&&Number.isInteger(f.mode)
          &&f.mode>=0&&f.mode<=0o777&&(f.mode&0o022)===0,'file-mode');
        else need(Object.keys(f).sort().join(' ')==='kind path sha256 target'&&typeof f.target==='string'
          &&f.target.length<1024&&!f.target.startsWith('/')&&!/[\x00-\x1f\x7f\\]/.test(f.target),'link-target');
      }
      inventories.set(resource.name+':'+version,inventory);
    }
  }
  const resources=names.map(name=>({name,current:join(root,name),next:join(root,name+'-next'),previous:join(root,name+'-previous')}));
  return {resources,services:[...services],verify:async(resource,position)=>{
    need(names.includes(resource.name),'resource-name');
    const key=['current','previous'].includes(position)?'current':'next',path=resource[position==='activated'?'current':position];
    await privatePath(path,{directory:true});
    const inventory=inventories.get(resource.name+':'+key),files=new Map(inventory.map(f=>[f.path,f])),seen=new Set();
    const walk=async(relativeName='')=>{
      for(const entry of await fs.readdir(join(path,relativeName),{withFileTypes:true})){
        const name=relativeName?relativeName+'/'+entry.name:entry.name,target=join(path,name),s=await fs.lstat(target),expected=files.get(name);
        need(s.uid===userInfo().uid&&(s.mode&0o022)===0||s.isSymbolicLink()&&s.uid===userInfo().uid,'resource-ownership');
        if(s.isDirectory()){
          need([...files.keys()].some(p=>p.startsWith(name+'/')),'unknown-directory');await walk(name);continue;
        }
        need(expected&&!seen.has(name),'unknown-file');seen.add(name);
        if(expected.kind==='file')need(s.isFile()&&s.nlink===1&&(s.mode&0o777)===expected.mode,'file-type-or-mode');
        else {
          need(s.isSymbolicLink()&&await fs.readlink(target)===expected.target,'link-drift');
          const actual=await fs.realpath(target),rel=relative(path,actual);
          need(rel!==''&&!rel.startsWith('..')&&!rel.startsWith('/'),'link-escape');
        }
        need(sha(await fs.readFile(target))===expected.sha256,'file-drift');
      }
    };await walk();need(seen.size===files.size,'missing-file');return true;
  }};
}

export async function upgradeManagedInstallation(args,dependencies={}){
  const {operation}=upgradeOptions(args);
  if(!dependencies.core)return withCurrentCore((core,context)=>upgradeManagedInstallation(args,{...dependencies,core,context}));
  const {core,context}=dependencies,root=dependencies.root??nativeHostRoot();
  await privatePath(root,{directory:true});
  const plan=await readPrivate(join(root,'upgrade-plan.json'),{limit:4000000}),layout=managedUpgradeLayout(core,root,plan);
  need(core.canonicalJSON(plan.next)==core.canonicalJSON({packageVersion:context.identity.packageVersion,pins:context.identity.pins}),
    'staged-package-does-not-match-this-client');
  const store=await openFileHostStore(core,{directory:join(root,'state')});
  try{
    const options={core,directory:root,store,current:plan.current,next:plan.next,...layout,
      ...(dependencies.manager?{manager:dependencies.manager}:{})};
    return await (operation==='rollback'?rollbackUpgrade(options):coordinateUpgrade({...options,check:operation==='check'}));
  }finally{await store.close();}
}
