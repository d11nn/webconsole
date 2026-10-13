/* Actual React browser test. Requires playwright installed outside this repo.
   config file contains isolated demo credentials; never print or screenshot secrets. */
const fs = require('fs');
const path = require('path');
const { spawnSync } = require('child_process');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const config = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));

(async () => {
  const browser = await chromium.launch({headless: true});
  const page = await browser.newPage({viewport: {width: 1440, height: 1100}});
  const failures = [];
  page.on('pageerror', error => { failures.push(error.message); console.error('React runtime:',error.message); });
  page.on('response', response => { if (response.url().includes('/api/') && response.status()>=400) console.error('API failure:',new URL(response.url()).pathname,response.status()); });
  try {
    await page.goto(config.url + '/deployments');
    await page.locator('input[name="email"]').fill(config.owner);
    await page.locator('input[name="password"]').fill(config.password);
    await page.getByRole('button', {name: /sign in/i}).click();
    await page.waitForURL(config.url + '/deployments');
    await page.waitForFunction(() => localStorage.getItem('token') && localStorage.getItem('username'));
    await page.goto(config.url + '/deployments');
    await page.getByText('Agent 存取管理', {exact: true}).waitFor({timeout:10000}).catch(async error => { console.error('Page text:', (await page.locator('body').innerText()).slice(0,2000)); throw error; });
    await page.getByLabel('Agent 名稱', {exact: true}).fill('Browser test Agent');
    await page.getByLabel('授權目標主機（需與後端設定一致）').fill('isolated-test-host');
    await page.getByLabel('到期時間（最長 30 日）').fill(new Date(Date.now() + 3600000).toISOString().slice(0,16));
    const credentialResponse = page.waitForResponse(r => r.url().endsWith('/api/agent-access') && r.request().method() === 'POST');
    await page.getByRole('button', {name: '建立診斷 Agent 憑證'}).click();
    const credential = await (await credentialResponse).json();
    if (!credential.token) throw new Error('Agent creation failed');
    await page.getByRole('button', {name: '已儲存，隱藏憑證'}).click();
    const jwt = await page.evaluate(() => localStorage.getItem('token'));
    // Submit using the actual MCP stdio process, with token delivered via temp file.
    const tokenFile = path.join(config.state, 'browser-agent.token');
    fs.writeFileSync(tokenFile, credential.token + '\n', {mode: 0o600});
    const messages = [
      {jsonrpc:'2.0',id:1,method:'initialize',params:{protocolVersion:'2025-06-18'}},
      {jsonrpc:'2.0',method:'notifications/initialized'},
      {jsonrpc:'2.0',id:2,method:'tools/call',params:{name:'request_deployment',arguments:{interface:'eth0'}}},
    ];
    const rpc = spawnSync('python3', [path.join(__dirname,'deployment_mcp.py'),'--url',config.url,'--token-file',tokenFile],
      {input: messages.map(m => JSON.stringify(m)).join('\n')+'\n',encoding:'utf8'});
    if (rpc.status !== 0) throw new Error('MCP process failed');
    const response = JSON.parse(rpc.stdout.trim().split('\n').at(-1));
    if (response.result.isError) throw new Error('MCP request failed');
    const requested = JSON.parse(response.result.content[0].text);
    await page.goto(requested.approvalUrl);
    await page.getByRole('button', {name: '核准並部署', exact:true}).waitFor();
    await page.screenshot({path:path.join(config.evidence,'01-pending.png'),fullPage:true});
    // One click and no start API, terminal approval, or follow-up prompt.
    await page.getByRole('button', {name:'核准並部署',exact:true}).click();
    await page.getByText('工作：running',{exact:true}).waitFor({timeout:15000});
    await page.screenshot({path:path.join(config.evidence,'02-running.png'),fullPage:true});
    await page.getByText('工作：succeeded',{exact:true}).waitFor({timeout:25000});
    await page.getByText(/Exit code: 0/).waitFor();
    await page.screenshot({path:path.join(config.evidence,'03-succeeded.png'),fullPage:true});
    // A normal user can see their own requests but cannot see approval controls.
    const createUser = await fetch(config.url + '/api/tenant/' + credential.agent.tenant + '/user', {
      method:'POST',headers:{Token:jwt,'Content-Type':'application/json'},
      body:JSON.stringify({email:'browser-diagnostician',encryptedPassword:config.password})
    });
    if (!createUser.ok) throw new Error('Ordinary user fixture creation failed');
    const login = await fetch(config.url+'/api/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'browser-diagnostician',password:config.password})});
    const userJwt = (await login.json()).access_token;
    const req = await fetch(config.url+'/api/deployment-requests',{method:'POST',headers:{Token:userJwt,'Content-Type':'application/json'},body:JSON.stringify({interface:'eth0'})});
    const ordinaryRequest = (await req.json()).request;
    const normal = await browser.newPage({viewport:{width:1440,height:1100}});
    await normal.goto(config.url+'/login');
    await normal.evaluate(({token}) => {localStorage.setItem('username','browser-diagnostician');localStorage.setItem('token',token);},{token:userJwt});
    await normal.goto(config.url+'/deployments/'+ordinaryRequest.id);
    await normal.getByText('此身分可查看進度；核准需由主機 Admin 登入。',{exact:true}).waitFor();
    if (await normal.getByRole('button',{name:'核准並部署',exact:true}).count()) throw new Error('Unauthorized approval button shown');
    await normal.screenshot({path:path.join(config.evidence,'04-diagnostician.png'),fullPage:true});
    const denied = await fetch(config.url+'/api/deployment-requests/'+ordinaryRequest.id+'/approve-and-start', {
      method:'POST',headers:{Token:userJwt,'Content-Type':'application/json'},body:JSON.stringify({planHash:ordinaryRequest.planHash})
    });
    if (denied.status!==403) throw new Error('Direct API bypass accepted');
    if (failures.length) throw new Error('React runtime errors: '+failures.join('; '));
    console.log('PASS: browser login, Agent creation, MCP request, ONE approval click, automatic running/result polling, hidden unauthorized controls and direct API denial.');
  } finally { await browser.close(); }
})().catch(error => {console.error(error.message);process.exit(1);});
