/* Real inference + real MCP integration. No mocked model or tool results. */
const fs=require('fs');
const path=require('path');
const assert=require('assert');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'playwright');
const config=JSON.parse(fs.readFileSync(process.argv[2],'utf8'));
(async()=>{
 const browser=await chromium.launch({headless:true});
 const page=await browser.newPage({viewport:{width:1800,height:1250}});
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 try {
  await page.goto(config.url+'/agent-team');
  await page.locator('input[name="email"]').fill(config.owner);
  await page.locator('input[name="password"]').fill(config.password);
  await page.getByRole('button',{name:/sign in/i}).click();
  await page.waitForURL(config.url+'/agent-team');
  await page.getByText('一個 Prompt，追蹤整個團隊',{exact:true}).waitFor();
  await page.getByLabel('在這裡輸入 Prompt').fill('請依官方指南準備 free5GC 的 MongoDB 與 Go 安裝計畫，並檢查目前 NF 與 MongoDB 的實際狀態。請分派安裝與排錯兩種專家，使用各自適當的工具。僅產生計畫與讀取狀態，不執行安裝或重啟服務。');
  await page.screenshot({path:path.join(config.evidence,'01-team-ready.png'),fullPage:true});
  const started=page.waitForResponse(r=>r.url().endsWith('/api/agent-team/runs')&&r.request().method()==='POST');
  await page.getByRole('button',{name:'開始 Agent Team',exact:true}).click();
  const run=await(await started).json();assert(run.id);
  await page.waitForURL(config.url+'/agent-team/'+run.id);
  const jwt=await page.evaluate(()=>localStorage.getItem('token'));
  let snapshot;let captured=false;const deadline=Date.now()+600000;
  while(Date.now()<deadline){
   const response=await fetch(config.url+'/api/agent-team/runs/'+run.id,{headers:{Token:jwt}});
   assert(response.ok);snapshot=await response.json();
   if(!captured&&snapshot.events.some(e=>e.type==='task_delegated')){
    await page.screenshot({path:path.join(config.evidence,'02-team-delegating.png'),fullPage:true});captured=true;
   }
   if(!['queued','running'].includes(snapshot.status))break;
   await new Promise(resolve=>setTimeout(resolve,1500));
  }
  assert.strictEqual(snapshot.status,'completed',snapshot.error||'Team did not complete');
  const delegated=snapshot.events.filter(e=>e.type==='task_delegated').map(e=>e.data.targetAgent);
  assert(delegated.includes('install-agent'));assert(delegated.includes('troubleshooting-agent'));
  const calls=snapshot.events.filter(e=>e.type==='tool_call_completed');
  for(const tool of ['install_mongo','install_go','get_nf_status','check_mongodb_status'])assert(calls.some(e=>e.data.tool===tool),'Missing actual tool '+tool);
  for(const name of ['install_mongo','install_go']){
   const event=calls.find(e=>e.data.tool===name);assert.strictEqual(event.agentId,'install-agent');assert.strictEqual(event.data.result.executed,false);
  }
  const boxes=snapshot.events.filter(e=>e.type==='toolbox_discovered');
  assert(boxes.find(e=>e.agentId==='install-agent').data.tools.includes('install_go'));
  assert(!boxes.find(e=>e.agentId==='troubleshooting-agent').data.tools.includes('install_go'));
  assert(snapshot.events.some(e=>e.type==='agent_thread_ready'&&e.data.authMode==='chatgpt'));
  await page.getByText('團隊回覆',{exact:true}).waitFor({timeout:10000});
  await page.screenshot({path:path.join(config.evidence,'03-team-completed.png'),fullPage:true});
  // Save only scrubbed server events; never save login/session tokens.
  fs.writeFileSync(path.join(config.evidence,'real-team-run.json'),JSON.stringify(snapshot,null,2));
  assert(!JSON.stringify(snapshot).includes(jwt));assert(!JSON.stringify(snapshot).includes(config.password));assert.deepStrictEqual(errors,[]);
  console.log('PASS: actual Webconsole Prompt -> real Codex Coordinator -> both specialists -> role-scoped MCP list -> Mongo/Go plans and actual NF/Mongo observations. Run '+run.id);
 }finally{await browser.close();}
})().catch(e=>{console.error(e.message);process.exit(1);});
