# 可視化 Codex Agent Team：真實決策與 MCP，安裝僅產生計畫

在 `/agent-team` 直接輸入 Prompt。Webconsole 啟動真實 Codex app-server，Coordinator 透過 MCP `delegate_task` 分派工作，專家 Agent 使用自己的 toolbox，事件與結果保存在 MongoDB。沒有預先寫好的模型答案、假模型或假環境結果。

與舊 `/deployments` mock worker 頁分開：Agent Team 使用真實模型和真實 MCP。`install_mongo`、`install_go` 是可呼叫的計畫型工具，回傳 `executed=false`，沒有安裝執行分支。其他工具讀取實際環境；找不到日誌或服務時回傳實際不可用狀態，不編造 healthy。

## 操作

1. 用首次初始化的 owner 登入 Webconsole，從左側 **AGENT TEAM** 進入。
2. 輸入 Prompt，或選「安裝計畫」、「實際排錯」、「混合任務」。
3. 按 **開始 Agent Team**。
4. 觀察 Coordinator 的決策摘要、分派箭頭、專家卡片、toolbox、被選工具與右側時間軸。
5. 展開事件，查看參數、permission、實際回傳、官方來源/hash 和 Codex thread/model。
6. 完成後可重新開啟 run URL，或從歷史紀錄查看；關閉網頁不會中止工作。

決策摘要來自模型公開回覆及 `delegate_task.reason`；工具清單來自本次真正的 MCP `tools/list`；呼叫與結果由 MCP server 記錄，另保存 app-server 的 `mcpToolCall` 觀察事件。不轉送私密 reasoning items。

`completed` 代表模型工作回合完成，不代表安裝成功，也不保證模型選擇正確。正確性需由工具事件、環境證據與測試案例判定。没有派送專家的回合標為 `needs_input`，不能假裝已完成派送。

## 身分、RBAC 與角色

| Agent | Role | 可見 MCP 工具 |
|---|---|---|
| Codex Coordinator | Coordinator | `list_team_capabilities`、`delegate_task` |
| Install Agent | Installer | `read_installation_guide`、`inspect_dependencies`、`install_mongo`、`install_go` |
| Troubleshooting Agent | Troubleshooter | `get_nf_status`、`check_mongodb_status`、`read_redacted_logs` |

`tools/team_catalog.json` 是這版受信任的 role/permission/tool registry，Go 後端呈現，MCP broker 依同一 registry 驗證。角色與 toolbox 第一版為固定設定，尚未提供 UI 自訂工具或 Agent 自行發布程式碼。

每次 run 的每個 Agent 都有獨立、隨機 MCP 憑證和 app-server config/對話。身分由 private Unix broker 驗證憑證取得，`role=Admin` 或其他 caller input 不會變更身分。`tools/list` 和 `tools/call` 共用權限檢查。

人類 Admin 才有 `team:run`，可提交 Prompt 和停止工作；一般 Diagnostician 只有 `team:read`，只能讀取自己 owner/tenant 的紀錄，不能耗用擁有者模型額度。一般 Agent bearer token 不可呼叫人類 Team API。

這個團隊由後端管理角色身分，不需要把 Admin 登入 JWT 或先前手動建立的外部 Agent token 交給模型。先前 Agent 存取管理頁的憑證仍供外部 MCP client 使用，與 Team runtime 的工作憑證分開。

## Codex 登入與工具邊界

本版只接受 runtime OS 使用者既有的 **Codex ChatGPT 登入**，不需要另外提供 API key。`account/read` 實際確認 auth type，`model/list` 選擇帳號的 default model；事件記錄實際 model。登入失效、額度不足或模型錯誤會顯示失敗，不降級成 mock。

使用官方 app-server 的 stdio JSON-RPC，依 `initialize → initialized → thread/start → turn/start` 啟動工作。這是 Webconsole 建立與持有的獨立 Codex threads，不會接管另一個正在使用的 Codex 對話。

每個 Agent 使用 private 0700 暫存 CODEX_HOME、自己的 MCP 設定、0600 工作憑證和空白工作目錄。只將原 Codex 管理的 auth.json 連結給受信任 runtime；不在 Prompt、HTTP 或日誌傳送登入資料。若以另一個服務 OS 帳號執行，需先由擁有者替該帳號完成 Codex 登入。

不繼承使用者原 MCP 清單，尤其不載入既有真實部署 PoC。關閉 shell/unified exec、browser/computer use、apps、原生 multi-agent、hooks 等能力；使用 read-only sandbox。Codex code-mode host 保留以支援實際 MCP 呼叫；只對受控計畫／只讀 MCP 配置 auto approval。任何 commandExecution/fileChange 事件會使 run 失敗。

這是可信單人 VM 的隔離設計，不宣稱能抵抗與 runtime 相同 OS UID 的惡意程式直接竄改帳密、registry 或 broker。正式多人部署仍需服務 OS 帳號、檔案保護、模型配額與更完整的執行隔離。

## 啟動

既有 demo launcher 已整合 Team：

```bash
python3 tools/mock_demo.py --state-dir /tmp/free5gc-rbac-demo-local
```

`mock_demo.py` 的名稱沿用舊部署 demo；新的 Agent Team 區域是真實 app-server 模式。既有 owner、角色、憑證與 Mongo 資料保留，重啟不重設密碼。

現有完整 Webconsole 需要 opt-in：

```text
FREE5GC_TEAM_MODE=codex
FREE5GC_TEAM_RUNTIME=/absolute/path/webconsole/tools/agent_team.py
```

同時需啟用原 RBAC 初始化。可選 `FREE5GC_CODEX_BINARY` 指定可信 Codex binary，以及 `FREE5GC_CODEX_AUTH_HOME` 指定服務使用者的既有 Codex 登入目录。環境設定由服務管理者控制，不接受 HTTP 參數修改。

前端依 repo 現有流程建置並部署到實際服務所用靜態目錄。本機 demo 直接提供 `frontend/build`。

## 持久化、停止與限制

MongoDB 新增 `agentTeamRuns`，包含 owner/tenant、Prompt、狀態、公開事件、摘要及錯誤。API 需登入且檢查 scope。第一版單一 Webconsole process 同時允許一個 team run，最多四次專家委派、40 次 MCP tool calls、400 個保存事件，run timeout 12 分鐘。

停止按鈕終止這次工作自己的程序群組，不操作 free5GC、舊部署 broker 或其他 Codex 對話。重啟後未完成 run 標為 interrupted，不自動重播。此版依單一 team backend 使用，尚未實作多 instance lease／分散式佇列。

官方指南每次實際下載，記錄 URL、時間與 SHA256；MongoDB/Go 官方安裝文件目前另外列為 reference-only，平台支援與具體安裝指令仍需檢閱。工具不依下載文件的指令改變角色或執行系統操作。Go/MongoDB、NF process、systemd 狀態都是受限只讀查詢。NF process 存在不代表 NF 健康或 UE 已能上網。

## 測試

```bash
go test -race ./backend/team ./backend/WebUI ./backend/deployment
go vet ./backend/team ./backend/WebUI ./backend/deployment
python3 -m unittest discover -s tools -p 'test_*.py'
node frontend/node_modules/typescript/bin/tsc --noEmit -p frontend/tsconfig.json
```

`tools/test_team_browser.cjs` 在真實模型登入與隔離測試 DB 上測試整條路徑，會使用帳號模型額度。需提供 private 0600 fixture：url、owner、password、evidence，並設定 `PLAYWRIGHT_MODULE`。測試 assertion 依 server events，驗證兩個專家、工具 scope 與 Mongo/Go `executed=false`，不只檢查模型文字。

[真實 run 與截圖](docs/team-evidence/README.md) 記錄本次測試，測試資料不混入使用者 demo DB。

官方介面依據：[Codex app-server](https://learn.chatgpt.com/docs/app-server)、[MCP approval config](https://learn.chatgpt.com/docs/config-file/config-reference)、[free5GC 官方指南](https://free5gc.org/guide/quick-setup/)。
