import React, { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Box, Button, Chip, Paper, TextField, Typography } from "@mui/material";
import Dashboard from "../Dashboard";
import axios from "../axios";

type Plan = { host: string; interface: string; commit: string; scriptHash: string; profile: string; workdir: string; mockOnly: boolean; changes: string[] };
type Request = { id: string; owner: string; tenant: string; agentId: string; status: string; createdAt: string; expiresAt: string; approvedBy?: string; planHash: string; plan: Plan };
type Job = { id: string; status: string; stage: string; updatedAt: string; logs: string[]; exitCode?: number; verification?: string };
type Agent = { id: string; name: string; owner: string; tenant: string; role: string; host: string; expiresAt: string; revoked: boolean };

export default function Deployments() {
  const { id } = useParams();
  const [requests, setRequests] = useState<Request[]>([]);
  const [detail, setDetail] = useState<{request: Request; job: Job | null} | null>(null);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [permissions, setPermissions] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [iface, setIface] = useState("");
  const [agentName, setAgentName] = useState("");
  const [host, setHost] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [token, setToken] = useState("");
  const [assignment, setAssignment] = useState({userId: "", tenantId: "", role: "Diagnostician"});
  const [roles, setRoles] = useState<any>(null);
  const canApprove = permissions.includes("deployment:approve");
  const canManage = permissions.includes("agent:manage");
  const report = (e: any) => setError(e.response?.data?.error || "無法連線至 Webconsole");

  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      try {
        const me = await axios.get("/api/rbac/me");
        const list = await axios.get("/api/deployment-requests");
        const selected = id ? (await axios.get(`/api/deployment-requests/${id}`)).data : null;
        const managed = me.data.permissions.includes("agent:manage") ? (await axios.get("/api/agent-access")).data : [];
        const assignments = me.data.permissions.includes("role:manage") ? (await axios.get("/api/roles")).data : null;
        if (active) { setPermissions(me.data.permissions); setRequests(list.data); setDetail(selected); setAgents(managed); setRoles(assignments); }
      } catch (e) { if (active) report(e); }
      if (active) timer = setTimeout(refresh, 2000);
    };
    refresh();
    return () => { active = false; clearTimeout(timer); };
  }, [id, refresh]);

  const action = async (fn: () => Promise<any>) => {
    setBusy(true); setError("");
    try { await fn(); } catch (e) { report(e); } finally { setBusy(false); }
  };
  const createAgent = () => action(async () => {
    if (!expiresAt) throw new Error("expiry required");
    const response = await axios.post("/api/agent-access", {name: agentName, host, expiresAt: new Date(expiresAt).toISOString()});
    setToken(response.data.token);
  });

  return <Dashboard title="部署核准與 Agent 存取" refreshAction={() => setRefresh(value => value + 1)}><Box sx={{p: 3, display: "grid", gap: 2}}>
    <Typography variant="h4">部署核准與 Agent 存取</Typography>
    <Alert severity="info">Mock 模式：按一次「核准並部署」後自動執行模擬工作。不安裝 free5GC，不修改系統。日誌每 2 秒更新。</Alert>
    {error && <Alert severity="error" onClose={() => setError("")}>{error}</Alert>}
    {!id && <Paper sx={{p: 2}}>
      <Typography variant="h6">提出部署計畫</Typography>
      <TextField label="DN 網卡" value={iface} onChange={e => setIface(e.target.value)} />
      <Button disabled={busy || !iface || !permissions.includes("deployment:request")} onClick={() => action(async () => {
        const response = await axios.post("/api/deployment-requests", {interface: iface});
        window.location.assign(response.data.approvalUrl);
      })}>建立請求</Button>
    </Paper>}
    {detail && <Paper sx={{p: 2, display: "grid", gap: 1}}>
      <Link to="/deployments">所有部署請求</Link>
      <Typography variant="h6">部署計畫 <Chip label={detail.request.status} /></Typography>
      <Typography>申請者：{detail.request.owner} · Agent：{detail.request.agentId || "人類"}</Typography>
      <Typography>主機：{detail.request.plan.host} · 網卡：{detail.request.plan.interface}</Typography>
      <Typography>建立：{detail.request.createdAt} · 核准有效期限：{detail.request.expiresAt}</Typography>
      {detail.request.plan.changes.map(change => <Typography key={change}>{change}</Typography>)}
      <details><summary>固定版本與計畫技術資訊</summary><pre style={{whiteSpace: "pre-wrap", overflowWrap: "anywhere"}}>{JSON.stringify(detail.request.plan, null, 2)}{"\n"}Plan hash: {detail.request.planHash}</pre></details>
      {canApprove && detail.request.status === "pending" && <Box>
        <Button variant="contained" disabled={busy} onClick={() => action(() => axios.post(`/api/deployment-requests/${id}/approve-and-start`, {planHash: detail.request.planHash}))}>核准並部署</Button>
        <Button color="error" disabled={busy} onClick={() => action(() => axios.post(`/api/deployment-requests/${id}/reject`))}>拒絕</Button>
      </Box>}
      {!canApprove && <Typography>此身分可查看進度；核准需由主機 Admin 登入。</Typography>}
      {detail.job && <Box aria-live="polite">
        <Typography variant="h6">工作：{detail.job.status}</Typography>
        <Typography>階段：{detail.job.stage} · 更新：{detail.job.updatedAt}</Typography>
        <pre style={{whiteSpace: "pre-wrap"}}>{detail.job.logs.join("\n")}</pre>
        {detail.job.verification && <Alert severity={detail.job.exitCode === 0 ? "success" : "error"}>Exit code: {detail.job.exitCode} · {detail.job.verification}</Alert>}
      </Box>}
    </Paper>}
    {!id && requests.map(request => <Paper key={request.id} sx={{p: 2}}>
      <Link to={`/deployments/${request.id}`}>{request.id}</Link> <Chip label={request.status} />
      <Typography>申請者：{request.owner} · Agent：{request.agentId || "人類"}</Typography>
      <Typography>{request.plan.host} / {request.plan.interface} · commit: {request.plan.commit}</Typography>
      <Typography>建立：{request.createdAt} · 到期：{request.expiresAt}</Typography>
    </Paper>)}
    {canManage && !id && <Paper sx={{p: 2, display: "grid", gap: 2}}>
      <Typography variant="h6">Agent 存取管理</Typography>
      <TextField label="Agent 名稱" value={agentName} onChange={e => setAgentName(e.target.value)} />
      <TextField label="授權目標主機（需與後端設定一致）" value={host} onChange={e => setHost(e.target.value)} />
      <TextField label="到期時間（最長 30 日）" type="datetime-local" InputLabelProps={{shrink: true}} value={expiresAt} onChange={e => setExpiresAt(e.target.value)} />
      <Button disabled={busy || !agentName || !host || !expiresAt} onClick={createAgent}>建立診斷 Agent 憑證</Button>
      {token && <Alert severity="warning">憑證僅顯示一次，請儲存於自己的 0600 token 檔案。<pre style={{overflowWrap: "anywhere", whiteSpace: "pre-wrap"}}>{token}</pre><Button onClick={() => setToken("")}>已儲存，隱藏憑證</Button></Alert>}
      {agents.map(agent => <Box key={agent.id}><Typography>{agent.name} · {agent.owner} · {agent.role} · {agent.host} · 到期 {agent.expiresAt} · {agent.revoked ? "已撤銷" : "有效"}</Typography>
        <Button disabled={busy || agent.revoked} onClick={() => action(() => axios.post(`/api/agent-access/${agent.id}/revoke`))}>撤銷</Button></Box>)}
      <Typography>MCP：使用 tools/deployment_mcp.py --url &lt;Webconsole URL&gt; --token-file &lt;0600 token 檔案&gt;。只提供申請、狀態與有限等待，不提供核准工具。</Typography>
    </Paper>}
    {roles && !id && <Paper sx={{p: 2, display: "grid", gap: 1}}>
      <Typography variant="h6">人類角色管理（僅 Admin）</Typography>
      <Typography>Admin 可核准主機部署；TenantAdmin 僅管理自己的 tenant；SubscriberAdmin 延續全域 Subscriber 管理；Agent 維持診斷權限，單次部署由計畫核准。</Typography>
      <TextField label="現有 userId" value={assignment.userId} onChange={e => setAssignment({...assignment, userId: e.target.value})} />
      <TextField label="tenantId" value={assignment.tenantId} onChange={e => setAssignment({...assignment, tenantId: e.target.value})} />
      <TextField select SelectProps={{native: true}} label="人類角色" value={assignment.role} onChange={e => setAssignment({...assignment, role: e.target.value})}>
        {["Diagnostician", "TenantAdmin", "SubscriberAdmin", "Admin"].map(role => <option key={role}>{role}</option>)}
      </TextField>
      <Button disabled={busy || !assignment.userId || !assignment.tenantId} onClick={() => action(() => axios.post("/api/roles", assignment))}>更新委派角色</Button>
      <pre style={{whiteSpace: "pre-wrap"}}>{JSON.stringify(roles.assignments, null, 2)}</pre>
    </Paper>}
  </Box></Dashboard>;
}
