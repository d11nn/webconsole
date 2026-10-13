import React, { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Alert, Box, Button, Chip, CircularProgress, Paper, TextField, Typography, Dialog, DialogTitle, DialogContent, DialogActions } from "@mui/material";
import HubOutlinedIcon from "@mui/icons-material/HubOutlined";
import BuildOutlinedIcon from "@mui/icons-material/BuildOutlined";
import TroubleshootIcon from "@mui/icons-material/Troubleshoot";
import PlayArrowIcon from "@mui/icons-material/PlayArrow";
import StopCircleOutlinedIcon from "@mui/icons-material/StopCircleOutlined";
import Dashboard from "../Dashboard";
import axios from "../axios";

type Agent = { id: string; name: string; role: string; description: string; permissions: string[]; tools: string[] };
type TeamEvent = { seq: number; type: string; agentId: string; at: string; data: any };
type Run = { id: string; prompt: string; status: string; createdAt: string; updatedAt: string; events?: TeamEvent[]; summary?: string; error?: string };
type Catalog = { version: string; mode: string; agents: Agent[]; tools: {name: string; description: string; permission: string; inputSchema: any}[] };
const labels: Record<string,string> = {
  run_started:"啟動真實模型", agent_started:"Agent 啟動", agent_thread_ready:"Codex 對話已連線", toolbox_discovered:"MCP 工具清單已取得", task_delegated:"Coordinator 分派任務", tool_call_started:"實際呼叫工具", tool_call_completed:"工具回傳結果", tool_call_failed:"工具呼叫失敗", authorization_denied:"RBAC 拒絕", codex_tool_observed:"Codex 工具事件", agent_message:"Agent 訊息", agent_completed:"Agent 完成", agent_failed:"Agent 失敗", run_completed:"團隊工作完成", run_failed:"工作失敗", run_needs_input:"需要檢閱／補充需求"
};
const samples = [
  "請依官方指南準備 free5GC 的 MongoDB 與 Go 安裝計畫。即使已安裝也說明差異，選擇對應工具，不執行安裝。",
  "請檢查目前 free5GC NF 與 MongoDB 的實際狀態，根據可取得的證據提出排錯建議，不重啟服務。",
  "請先準備 MongoDB 安裝計畫，再檢查目前 NF 與 MongoDB 狀態。分派適當專家，不進行系統變更。"
];
const colorFor = (agent: string) => agent === "coordinator" ? "#6366f1" : agent === "install-agent" ? "#0891b2" : "#d97706";

export default function AgentTeam() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [catalog,setCatalog] = useState<Catalog | null>(null);
  const [prompt,setPrompt] = useState("");
  const [run,setRun] = useState<Run | null>(null);
  const [history,setHistory] = useState<Run[]>([]);
  const [error,setError] = useState("");
  const [busy,setBusy] = useState(false);
  const [canRun,setCanRun] = useState(false);
  const [selectedTool,setSelectedTool] = useState<string | null>(null);
  const [refresh,setRefresh] = useState(0);
  const events = run?.events || [];
  const working = run?.status === "queued" || run?.status === "running";
  const fail = (e: any) => setError(e.response?.data?.error || "無法連線至 Agent Team runtime");
  useEffect(() => {
    let active=true;
    Promise.all([axios.get("/api/agent-team/catalog"),axios.get("/api/rbac/me")]).then(([c,m]) => {
      if (active) {setCatalog(c.data);setCanRun(m.data.permissions.includes("team:run"));}
    }).catch(e => {if(active)fail(e);});
    return () => {active=false;};
  }, []);
  useEffect(() => {
    let active=true;let timer: ReturnType<typeof setTimeout>;
    const poll=async () => {
      try {
        const h=await axios.get("/api/agent-team/runs");
        const selected=id ? (await axios.get(`/api/agent-team/runs/${id}`)).data : null;
        if (active) {setHistory(h.data);setRun(selected);}
      } catch(e) {if(active)fail(e);}
      if(active)timer=setTimeout(poll,1000);
    };
    poll();return () => {active=false;clearTimeout(timer);};
  }, [id,refresh]);
  const start=async () => {
    setBusy(true);setError("");
    try {const response=await axios.post("/api/agent-team/runs",{prompt});navigate(`/agent-team/${response.data.id}`);}
    catch(e){fail(e);}finally{setBusy(false);}
  };
  const roleState=(agent: string) => {
    const relevant=events.filter(e => e.agentId===agent && ["agent_started","agent_completed","agent_failed"].includes(e.type));
    const last=relevant[relevant.length-1];return last?.type === "agent_started" ? (working ? "工作中" : "已停止") : last?.type === "agent_completed" ? "已完成" : last?.type === "agent_failed" ? "失敗" : "待命";
  };
  const agentCard=(agent: Agent) => {
    const agentEvents=events.filter(e => e.agentId===agent.id);
    const discovery=agentEvents.filter(e => e.type==="toolbox_discovered").slice(-1)[0];
    const used=new Set(agentEvents.filter(e => e.type==="tool_call_started").map(e=>e.data.tool));
    const activeCall=agentEvents.filter(e => e.type==="tool_call_started" || e.type==="tool_call_completed" || e.type==="tool_call_failed").slice(-1)[0];
    const delegated=events.filter(e=>e.type==="task_delegated" && e.data.targetAgent===agent.id).slice(-1)[0];
    const state=roleState(agent.id);const color=colorFor(agent.id);
    const thread=agentEvents.filter(e=>e.type==="agent_thread_ready").slice(-1)[0];
    return <Paper key={agent.id} variant="outlined" sx={{minWidth:0,p:2.5,borderColor:state==="工作中"?color:"#dce3ed",borderWidth:state==="工作中"?2:1,borderRadius:3,position:"relative",boxShadow:state==="工作中"?`0 0 0 4px ${color}12`:"none"}}>
      <Box sx={{display:"flex",alignItems:"center",gap:1.5,mb:1}}>
        <Box sx={{p:1,display:"flex",borderRadius:2,bgcolor:color+"12",color}}>{agent.id==="coordinator"?<HubOutlinedIcon/>:agent.id==="install-agent"?<BuildOutlinedIcon/>:<TroubleshootIcon/>}</Box>
        <Box sx={{flex:1}}><Typography fontWeight={700}>{agent.name}</Typography><Typography variant="caption" sx={{color}}>{agent.role}</Typography></Box>
        {state==="工作中"&&<CircularProgress size={14} sx={{color}}/>}<Chip size="small" label={state} color={state==="已完成"?"success":state==="失敗"?"error":"default"}/>
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{mb:2}}>{agent.description}</Typography>
      {thread&&<Typography variant="caption" color="text.secondary" sx={{display:"block",mb:1}}>{thread.data.model} · ChatGPT 登入 · 獨立 Codex thread</Typography>}
      <Typography variant="caption" fontWeight={700}>{discovery?"本次實際 MCP tools/list":"RBAC 授予的 toolbox（尚未連線）"}</Typography>
      <Box sx={{display:"flex",flexWrap:"wrap",gap:.75,mt:1}}>{(discovery?.data.tools || agent.tools).map((tool:string)=><Chip key={tool} label={tool} onClick={()=>setSelectedTool(tool)} size="small" variant={used.has(tool)?"filled":"outlined"} sx={{fontFamily:"monospace",fontSize:11,bgcolor:used.has(tool)?color+"18":undefined,color:used.has(tool)?color:undefined,borderColor:used.has(tool)?color:undefined}}/>)}</Box>
      {activeCall&&<Box sx={{mt:1.5,p:1,bgcolor:color+"08",borderRadius:1}}><Typography variant="caption">{activeCall.type==="tool_call_started"&&working?"正在呼叫":"最近選擇"}：<b>{activeCall.data.tool}</b></Typography></Box>}
      {delegated&&<Box sx={{mt:1.5,borderTop:"1px solid #edf0f5",pt:1.5}}><Typography variant="caption" fontWeight={700}>分派依據</Typography><Typography variant="body2">{delegated.data.decisionSummary}</Typography><Typography variant="caption" color="text.secondary" sx={{display:"block",overflow:"hidden",textOverflow:"ellipsis",whiteSpace:"nowrap"}}>任務：{delegated.data.task}</Typography></Box>}
      <details style={{marginTop:12}}><summary style={{fontSize:12,color:"#64748b",cursor:"pointer"}}>Role permissions</summary><Typography variant="caption" component="pre" sx={{whiteSpace:"pre-wrap"}}>{agent.permissions.join("\n")}</Typography></details>
    </Paper>;
  };
  return <Dashboard compactHeader title="Agent Team · 決策與工具" refreshAction={()=>setRefresh(value=>value+1)}>
    <Box sx={{p:{xs:2,md:3},display:"grid",gap:2.5}}>
      <Box sx={{display:"flex",justifyContent:"space-between",alignItems:"center",flexWrap:"wrap",gap:1}}>
        <Box><Typography variant="overline" color="primary">FREE5GC AGENT CONTROL ROOM</Typography><Typography variant="h4" fontWeight={700}>一個 Prompt，追蹤整個團隊</Typography></Box>
        <Box sx={{display:"flex",gap:1}}><Chip label="真實 Codex app-server" color="primary" variant="outlined"/><Chip label="Plan only · 不執行安裝" color="success" variant="outlined"/></Box>
      </Box>
      {error&&<Alert severity="error" onClose={()=>setError("")}>{error}</Alert>}
      <Paper sx={{p:2.5,borderRadius:3}}>
        <Typography fontWeight={700} sx={{mb:1.5}}>交付任務給 Coordinator</Typography>
        <TextField fullWidth multiline minRows={3} maxRows={8} label="在這裡輸入 Prompt" value={prompt} onChange={e=>setPrompt(e.target.value)} inputProps={{maxLength:4000}} placeholder="例如：依官方指南準備 MongoDB、Go 安裝計畫，並檢查目前 NF 狀態。"/>
        <Box sx={{display:"flex",gap:1,flexWrap:"wrap",mt:1.5}}>{["安裝計畫","實際排錯","混合任務"].map((label,index)=><Chip key={label} label={label} onClick={()=>setPrompt(samples[index])} variant="outlined"/>)}</Box>
        <Box sx={{display:"flex",alignItems:"center",gap:2,mt:2,flexWrap:"wrap"}}><Button variant="contained" startIcon={busy?<CircularProgress size={16}/>:<PlayArrowIcon/>} disabled={!canRun||busy||working||!prompt.trim()} onClick={start}>開始 Agent Team</Button>
          {working&&canRun&&<Button color="error" startIcon={<StopCircleOutlinedIcon/>} onClick={()=>axios.post(`/api/agent-team/runs/${run?.id}/cancel`).catch(fail)}>停止這次工作</Button>}
          <Typography variant="caption" color="text.secondary">使用現有 ChatGPT 登入；模型真的決策、MCP 真的呼叫。顯示決策摘要與證據。</Typography>
        </Box>
      </Paper>
      {run&&<Paper variant="outlined" sx={{p:2,borderRadius:2}}><Box sx={{display:"flex",gap:1,alignItems:"center",mb:1}}><Chip label={run.status} color={working?"primary":run.status==="completed"?"success":run.status==="failed"?"error":"default"}/><Typography variant="caption">Run {run.id} · {events.length} 個事件</Typography></Box><Typography sx={{whiteSpace:"pre-wrap"}}>{run.prompt}</Typography></Paper>}
      <Box sx={{display:"grid",gridTemplateColumns:{xs:"1fr",lg:"minmax(0,1.6fr) minmax(340px,1fr)"},gap:2.5,alignItems:"start"}}>
        <Box>
          <Typography variant="h6" sx={{mb:1.5}}>可視化 Agent Team</Typography>
          {catalog&&<Box data-testid="team-graph" sx={{p:{xs:1,md:2},borderRadius:3,bgcolor:"#f7f9fe",border:"1px solid #e2e8f0"}}>
            <Box sx={{maxWidth:550,mx:"auto"}}>{agentCard(catalog.agents[0])}</Box>
            <svg viewBox="0 0 800 65" width="100%" height="65" aria-label="Coordinator 分派到兩個專家" style={{display:"block"}}>
              <defs><marker id="team-arrow" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto"><path d="M0 0 L8 4 L0 8" fill="#94a3b8"/></marker></defs>
              <path d="M400 0 V25 H200 V58" fill="none" stroke={events.some(e=>e.type==="task_delegated"&&e.data.targetAgent==="install-agent")?"#0891b2":"#cbd5e1"} strokeWidth="2" markerEnd="url(#team-arrow)"/>
              <path d="M400 25 H600 V58" fill="none" stroke={events.some(e=>e.type==="task_delegated"&&e.data.targetAgent==="troubleshooting-agent")?"#d97706":"#cbd5e1"} strokeWidth="2" markerEnd="url(#team-arrow)"/>
              <text x="400" y="48" textAnchor="middle" fill="#64748b" fontSize="12">delegate_task · 真實分派</text>
            </svg>
            <Box sx={{display:"grid",gridTemplateColumns:{xs:"minmax(0,1fr)",md:"repeat(2,minmax(0,1fr))"},gap:2}}>{catalog.agents.slice(1).map(agentCard)}</Box>
          </Box>}
          {run?.summary&&<Paper sx={{p:2.5,mt:2,borderRadius:3}}><Typography variant="h6">團隊回覆</Typography><Typography sx={{whiteSpace:"pre-wrap",mt:1,fontSize:14}}>{run.summary}</Typography></Paper>}
          {run?.error&&<Alert severity="error" sx={{mt:2}}>{run.error}</Alert>}
        </Box>
        <Paper data-testid="team-timeline" variant="outlined" sx={{borderRadius:3,overflow:"hidden"}}>
          <Box sx={{p:2,borderBottom:"1px solid #e2e8f0",display:"flex",justifyContent:"space-between"}}><Typography fontWeight={700}>決策與工具時間軸</Typography>{working&&<Chip size="small" label="LIVE" color="primary"/>}</Box>
          <Box sx={{maxHeight:820,overflowY:"auto",p:2}}>
            {!events.length&&<Typography variant="body2" color="text.secondary">送出 Prompt 後，這裡會顯示真實分派、MCP 工具清單、呼叫與回傳。沒有預先編排的動畫。</Typography>}
            {events.filter(e=>e.type!=="codex_tool_observed" || (e.data.phase==="item/completed" && e.data.status!=="completed")).map(event=><Box key={event.seq} sx={{borderLeft:`2px solid ${colorFor(event.agentId)}40`,pl:2,pb:2,position:"relative"}}>
              <Box sx={{position:"absolute",left:-5,top:4,width:8,height:8,bgcolor:colorFor(event.agentId),borderRadius:"50%"}}/>
              <Box sx={{display:"flex",gap:1,alignItems:"center",flexWrap:"wrap"}}><Typography variant="caption" color="text.secondary">#{event.seq} · {new Date(event.at).toLocaleTimeString()}</Typography><Typography variant="caption" sx={{color:colorFor(event.agentId)}}>{event.agentId}</Typography></Box>
              <Typography variant="body2" fontWeight={700}>{labels[event.type]||event.type}{event.data.tool&&<span style={{fontFamily:"monospace",fontWeight:500}}> · {event.data.tool}</span>}</Typography>
              {event.type==="task_delegated"&&<Typography variant="body2">→ {event.data.targetAgent}<br/>{event.data.decisionSummary}</Typography>}
              {event.type==="toolbox_discovered"&&<Typography variant="caption" sx={{overflowWrap:"anywhere"}}>{event.data.tools?.join(" · ")}</Typography>}
              {event.type==="agent_message"&&<Typography variant="body2" sx={{whiteSpace:"pre-wrap",fontSize:12,color:"#475569"}}>{event.data.text}</Typography>}
              {event.type==="tool_call_started"&&<Chip size="small" color="success" variant="outlined" label={`RBAC allowed · ${event.data.permission}`} sx={{mt:.5,fontSize:10}}/>}
              {event.type==="tool_call_completed"&&event.data.result?.executed===false&&<Chip size="small" label="計畫已產生 · 未執行安裝" color="info" variant="outlined" sx={{mt:.5}}/>}
              {event.data.error&&<Typography color="error" variant="body2">{event.data.error}</Typography>}
              <details style={{marginTop:5}}><summary style={{fontSize:11,color:"#64748b",cursor:"pointer"}}>參數、來源與實際證據</summary><pre style={{fontSize:11,whiteSpace:"pre-wrap",overflowWrap:"anywhere",background:"#f8fafc",padding:8}}>{JSON.stringify(event.data,null,2)}</pre></details>
            </Box>)}
          </Box>
        </Paper>
      </Box>
      <Dialog open={selectedTool!==null} onClose={()=>setSelectedTool(null)} maxWidth="sm" fullWidth>
        <DialogTitle sx={{fontFamily:"monospace"}}>{selectedTool}</DialogTitle>
        <DialogContent>
          <Typography sx={{mb:2}}>{catalog?.tools.find(t=>t.name===selectedTool)?.description}</Typography>
          <Chip label={catalog?.tools.find(t=>t.name===selectedTool)?.permission || "unknown"} size="small" color="primary" variant="outlined"/>
          {selectedTool?.startsWith("install_")&&<Alert severity="info" sx={{mt:2}}>此工具只產生安裝計畫，回傳 executed=false。</Alert>}
          <Typography variant="subtitle2" sx={{mt:2}}>MCP input schema</Typography>
          <pre style={{fontSize:12,whiteSpace:"pre-wrap"}}>{JSON.stringify(catalog?.tools.find(t=>t.name===selectedTool)?.inputSchema,null,2)}</pre>
        </DialogContent>
        <DialogActions><Button onClick={()=>setSelectedTool(null)}>關閉</Button></DialogActions>
      </Dialog>
      <Paper variant="outlined" sx={{p:2,borderRadius:3}}><Typography fontWeight={700} sx={{mb:1}}>最近的團隊工作</Typography><Box sx={{display:"grid",gap:1}}>{history.map(item=><Button key={item.id} variant={item.id===id?"outlined":"text"} sx={{justifyContent:"flex-start",textAlign:"left"}} onClick={()=>navigate(`/agent-team/${item.id}`)}><Chip size="small" label={item.status} sx={{mr:1}}/>{item.prompt.slice(0,100)}</Button>)}</Box></Paper>
    </Box>
  </Dashboard>;
}
