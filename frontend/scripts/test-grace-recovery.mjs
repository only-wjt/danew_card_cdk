import assert from 'node:assert/strict'
import { readFileSync, mkdtempSync, rmSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { parse, compileScript } from '@vue/compiler-sfc'
import { build } from 'esbuild'
import { effectScope } from 'vue'
const root=fileURLToPath(new URL('..',import.meta.url))
const storage=new Map()
globalThis.localStorage=globalThis.sessionStorage={getItem:k=>storage.get(k)||null,setItem:(k,v)=>storage.set(k,v),removeItem:k=>storage.delete(k)}
let confirmation=true, answer, resolveResponse, calls=[]
globalThis.__graceTest={confirm:async()=>confirmation}
globalThis.fetch=async(path,init)=>{
 calls.push({path,body:JSON.parse(init.body)})
 assert.equal(path,'/api/v1/public/cdk/recover-subscription','recovery must never redeem or use a renewal endpoint')
 const body=answer==='wait'?await new Promise(r=>{resolveResponse=r}):answer
 return {ok:true,text:async()=>JSON.stringify({code:0,data:body})}
}
const mocks={
 'vue-router':'export const useRoute=()=>({query:{}})',
 'vue-i18n':'export const useI18n=()=>({t:k=>k})',
 '../../lib/dialog':'export const dialog={confirm:globalThis.__graceTest.confirm,toast(){}}',
 '../../components/LanguageToggle.vue':'export default {}',
 '../../components/ThemeToggle.vue':'export default {}',
 '../../components/RedeemModeTabs.vue':'export default {}',
}
const {descriptor}=parse(readFileSync(resolve(root,'src/views/user/RechargeView.vue'),'utf8'))
const compiled=compileScript(descriptor,{id:'grace-test'})
const tmp=mkdtempSync(resolve(root,'.grace-test-')),scope=effectScope()
try{
 await build({stdin:{contents:compiled.content,resolveDir:resolve(root,'src/views/user'),loader:'ts'},outfile:resolve(tmp,'component.mjs'),bundle:true,platform:'node',format:'esm',external:['vue'],plugins:[{name:'mocks',setup(b){b.onResolve({filter:/.*/},a=>a.path in mocks?{path:a.path,namespace:'mock'}:undefined);b.onLoad({filter:/.*/,namespace:'mock'},a=>({contents:mocks[a.path],loader:'js'}))}}]})
 const {default:component}=await import(pathToFileURL(resolve(tmp,'component.mjs')).href)
 const vm=scope.run(()=>component.setup({}, {expose(){}}))
 const ready=()=>{vm.invalidatePreflight();vm.account.value={...vm.account.value,checked:true,currentPlan:'plus',subscriptionHasActive:true,subscriptionIsDelinquent:true};vm.redemptionToken.value='synthetic-redemption';vm.preflightToken.value='synthetic-old';vm.step.value=3}
 const refreshed={preflight_token:'synthetic-new',currentPlan:'free',subscription_has_active:false,subscription_is_delinquent:false,subscription_recovery_required:false}
 ready();confirmation=false;await vm.recoverGraceSubscription();assert.equal(calls.length,0)
 confirmation=true;answer={status:'cleared',preflight:refreshed};await vm.recoverGraceSubscription()
 assert.equal(vm.preflightToken.value,'synthetic-new');assert.equal(vm.account.value.currentPlan,'free');assert.equal(vm.step.value,3);assert.equal(vm.recoveryPending.value,false)
 assert.deepEqual(calls[0].body,{redemption_token:'synthetic-redemption',preflight_token:'synthetic-old',confirmed:true})
 ready();answer={status:'pending',preflight:{...refreshed,currentPlan:'plus',subscription_has_active:true,subscription_is_delinquent:true}};await vm.recoverGraceSubscription();assert.equal(vm.recoveryPending.value,true)
 const count=calls.length;await vm.doRedeem();assert.equal(calls.length,count,'pending state must not redeem')
 ready();answer='wait';const pending=vm.recoverGraceSubscription();await Promise.resolve();await Promise.resolve()
 const current=calls.length;await vm.recoverGraceSubscription();assert.equal(calls.length,current)
 vm.sessionRaw.value='another-synthetic-session';resolveResponse({status:'cleared',preflight:refreshed});await pending
 assert.equal(vm.preflightToken.value,'');assert.equal(vm.account.value.checked,false)
 console.log('CDK grace UI: consent, refreshed state, no redemption, pending, double click, stale response PASS')
} finally {scope.stop();rmSync(tmp,{recursive:true});delete globalThis.__graceTest;delete globalThis.fetch;delete globalThis.localStorage;delete globalThis.sessionStorage}
