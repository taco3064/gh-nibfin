// Compatibility with already-authorized Core pins: upstream retains the actual
// role summary instead of requiring predetermined prose. Only this descriptive
// field may be observed; verdict, authority, inputs and replay gates stay owned
// by the original lifecycle. Raw terminal evidence is retained first.
export function nativeRoleSummaries(core,{retain}){
 const bindings=new Map(),summaries=new Map(),same=(a,b)=>core.canonicalJSON(a)===core.canonicalJSON(b);
 return Object.freeze({
  bind(value){
   const id=value.input.execution.id;if(bindings.has(id))throw Error('native-role-summary:duplicate');bindings.set(id,value);
   return{...value,current:()=>{const current=structuredClone(value.current());
    if(summaries.has(id))current.expected.result.summary=summaries.get(id);return current;}};
  },
  async retain(record){
   const reference=await retain(record);
   if(typeof reference==='string'&&reference&&record.kind==='role-collection'&&record.exitCode===0){
    try{
     const value=bindings.get(record.execution.id),e=record.evidence,expected=value.current().expected;
     const result=core.validateStageResult(e.result,{...value.resultContext?.(),execution:value.input.execution,binding:value.input.binding});
     if(same(record.execution,value.input.execution)&&same(record.binding,value.input.binding)
       &&e.stage===value.input.execution.role&&same(e.execution,value.input.execution)&&same(e.binding,value.input.binding)
       &&same(e.inputs,value.inputs)&&same({...result,summary:expected.result.summary},expected.result))summaries.set(record.execution.id,result.summary);
    }catch{/* Preserve original malformed output for the original rejecting owner. */}
   }
   return reference;
  }
 });
}
