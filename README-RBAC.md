# Webconsole RBAC、Agent 存取與一鍵部署核准（mock MVP）

這版整合現有 Go/Gin、React/TypeScript 與 MongoDB。Agent 提出具體計畫，人類在 `/deployments/:id` 按一次「核准並部署」，後端自動排入工作並執行 mock worker。網頁每 2 秒更新；MCP 查詢同一份請求、job、日誌與驗證結果。不需要另開終端機核准，也不需要回覆 Agent「繼續」。

**本版只執行 mock。** 設定 `FREE5GC_DEPLOYMENT_MODE=real` 會拒絕啟動整合；沒有接上舊 PoC 的 root broker、沒有執行 quick-setup.sh，也沒有新增 sudoers、部署帳號或系統權限。Mock 成功只表示流程與授權測試通過。真實 worker 是後續獨立、明確 opt-in 的工作，不應把此功能用於正式主機安裝。

## 需要哪些既有相依項目

- 此 checkout 的 Go 1.26.2、MongoDB、Python 3 標準函式庫。
- 前端使用 repo 的 Yarn 4 與 Node；遵循既有 lockfile。不需重裝 free5GC。
- 本機 demo 使用既有 `mongod`，啟動獨立 port、獨立資料目錄；不操作現有 mongod/systemd。
- 瀏覽器測試額外使用 Playwright，與執行本功能所需相依項目分開。

## 快速操作：獨立本機 mock demo

先在 `frontend` 以既有相依項目執行 `yarn build`，再從 Webconsole repo 執行：

```bash
python3 tools/mock_demo.py --state-dir /tmp/free5gc-rbac-demo-local
```

第一次設定自己的 demo 擁有者與至少 12 字元密碼。預設網址 `http://127.0.0.1:5080/deployments`，MongoDB 為 `127.0.0.1:27020`，目標主機名稱為 `mock-demo-host`。若在另一台電腦操作，使用 SSH local forwarding 連到 VM 的 localhost。不要把未設 TLS 的管理介面公開到網路。

1. 登入後在 Agent 存取管理建立憑證，主機填 `mock-demo-host`，選擇最多 30 日到期時間。
2. 憑證只顯示一次；存入自己的 Agent token 檔案，限制為 0600。不要貼到 Prompt、commit 或管理員 session。
3. MCP 提交請求後，開啟回傳的核准連結。
4. 查看固定版本、網卡與變更內容，按「核准並部署」。
5. 工作自動進行，頁面顯示階段、日誌、exit code 與明確標示的 mock 驗證結果。
6. 在管理頁撤銷 Agent；下一次 API/MCP 存取立即被拒絕。

Ctrl-C 只停止這個 demo 與它啟動的獨立 mongod，保留資料、日誌與產物。用相同 `--state-dir` 重啟不重設擁有者或密碼。正式 Webconsole 不使用此 demo 初始化帳號。

## 在既有 Webconsole opt-in

從 free5GC 主 repo 執行 `bash scripts/rbac/build.sh`（僅編譯，不執行安裝腳本）。Webconsole repo 可分別建置：

```bash
go build -o bin/rbac-bootstrap ./cmd/rbac-bootstrap
go build -o bin/webconsole ./server.go
```

**首次本機擁有者初始化**：本人在受信任的終端機執行：

```bash
python3 tools/bootstrap_owner.py --mongo-uri mongodb://127.0.0.1:27017 --database free5gc
```

使用與 `config/webuicfg.yaml` 相同的 MongoDB database。密碼以隱藏輸入取得，只經子程序 stdin 傳遞，不進入 shell arguments、環境、HTTP、Prompt 或日誌。新名稱建立新擁有者；既有帳號需驗證原密碼，拒絕預設 `free5gc` 密碼。已有 host Admin 時初始化拒絕重做，不重置任何帳密。

資料庫憑證、初始化 binary 與服務檔案必須由可信擁有者管理；能任意改寫授權 DB 的 OS 使用者等同授權服務管理者。這不是能抵抗同一 OS 身分直接竄改 MongoDB 的邊界。

由主 repo 啟動 `bash scripts/rbac/mock-webconsole.sh`，或設定下列環境後啟動現有 Webconsole：

```text
FREE5GC_DEPLOYMENT_MODE=mock
FREE5GC_DEPLOYMENT_HOST=<主機識別>
FREE5GC_DEPLOYMENT_COMMIT=<40 字元固定 commit>
FREE5GC_DEPLOYMENT_SCRIPT_HASH=<64 字元 quick-setup.sh SHA256>
```

主 repo launcher 取目前 checkout 的固定 HEAD 與腳本 hash，作為**mock 計畫參考版本**；沒有下載或執行 main。真實模式尚未實作「解析 upstream main、取得並驗證受保護固定版本」流程。

既有 Webconsole 從 `public` 提供前端；請依 repo 的正常流程將 `frontend/build` 部署到服務所用 `public`。`scripts/rbac/build.sh` 只建置，避免覆蓋已部署的靜態產物。本機 demo 直接提供 `frontend/build`。

主 repo 的 systemd/env 範本僅供檢閱，沒有自動安裝；Webconsole 以非 root、無 sudo 的服務帳號執行。首次安裝若尚無 Webconsole/MongoDB，仍需獨立 bootstrap，clone 不會取得 root。

## MCP 註冊與日常 Prompt

這是 MCP 2025-06-18 **stdio**，連線到 Webconsole 的內部 REST API；沒有宣稱為 Streamable HTTP 或完整 OAuth server。初始化、版本協商及 initialized notification 遵循 [MCP lifecycle](https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle)。

MCP client 啟動以下 command/args；將路徑替換成實際位置：

```text
command: python3
args:
  /home/ubuntu/free5gc/webconsole/tools/deployment_mcp.py
  --url
  http://127.0.0.1:5080
  --token-file
  /home/ubuntu/.config/free5gc-webconsole/agent.token
```

遠端 REST 連線要求 HTTPS；bridge 不跟隨 HTTP redirect，也不在錯誤中印出憑證。每個 Agent/session 建立獨立憑證，以 server 驗證的 agentId、owner、tenant 與 host 範圍隔離。

工具只有：

| 工具 | 行為 |
|---|---|
| `request_deployment(interface)` | 建立 pending 計畫，回傳核准網頁連結 |
| `get_deployment_status(request_id)` | 回傳同一 request/job、日誌與結果 |
| `wait_for_job(request_id, timeout)` | 最多等待 30 秒，每秒查詢一次，包含等待核准階段 |

沒有 approve、set_role、shell 或 start 工具。`/deploy` 尚未安裝為 slash Skill；可直接輸入「幫我提出 free5GC mock 部署計畫，網卡是 …，提供網頁核准連結，並有限度等待工作」。Agent 可在當前對話回合重複有限等待；MCP 不會在已結束回合自行喚醒 Agent。網頁即使 Agent 回合結束仍會持續自動更新。

## 角色與遷移

| 角色 | 權限 |
|---|---|
| 人類 Admin | 主機核准、Agent/角色管理、稽核、診斷及既有 Subscriber 管理 |
| SubscriberAdmin | 延續舊全域 Subscriber 管理功能，不能修改 host Admin 帳密；無 host approval |
| TenantAdmin | 限自己的 tenant 使用者/Subscriber 管理；無跨 tenant 或 host approval |
| Diagnostician | 建立自己的請求、讀取自己的授權範圍 |
| Deployer | 計畫綁定的單次部署語意；不能透過角色 API 永久指派給人類/Agent |

角色不進 JWT claims 作為權限來源，每次後端查詢目前角色。普通使用者不能自改角色。只有 Admin 看到角色管理欄位；直接呼叫 API 仍獨立驗證。不能移除最後一位 Admin。舊 tenant/user CRUD 不能刪除有效 host Admin；SubscriberAdmin 不能重設 host Admin 密碼；修改密碼 API 僅限 JWT sub 對應的登入者。角色 API 拒絕將仍使用預設密碼的帳號升為 Admin。

`SetAdmin` 不再刪除/重建 `admin`，也不再寫入預設密碼。Opt-in 後，既有 `admin` 的 userId/tenantId/hash 保留，只遷移為 `SubscriberAdmin`，不自動取得主機部署權限。現有 Subscriber/tenant collections 不變；整合關閉時保留既有 Subscriber 管理身分相容行為。

新增 `deploymentAuthorization` collection 的單一 versioned host document：role assignments、受限 Agent 委派/憑證雜湊、不可變 deployment requests、綁定 request/job/planHash 的 Deployer 單次 grant（記錄 consumedAt）、jobs、audit events。密碼仍使用原 `userData` 的 bcrypt。Agent token DB 只存 SHA256，HTTP 列表不回傳 token/hash。

MVP 以單一 document 的 compare-and-swap 原子提交，支援 standalone MongoDB，避免核准與 queue 分開寫入造成 crash 空窗。適合單主機低流量；MongoDB 的 16 MiB document 上限仍適用。大量長期稽核需改為獨立 collections、archive 與 transaction/outbox，尚未做容量壓力測試。請使用定期 DB backup；此版不自動刪除歷史 evidence。

## 工作一致性與撤銷

- `pending → rejected / expired`；核准原子轉為 `queued` 並建立唯一 job，audit 記錄核准者、時間及固定計畫 hash。
- `queued → running → succeeded / failed`；同主機最多一個 queued/running job。
- 重複 approve 回傳原 job，完成或失敗後都不建立第二個 job。
- 核准前與 worker claim 前检查到期、Agent 撤銷與計畫 hash。修改版本/網卡/hash 的 API input 被拒絕；儲存計畫被竄改也不執行。
- queue 在重啟後繼續 claim；running worker heartbeat 超過 1 分鐘未更新則 failed，保留證據、不自動重跑。
- 撤銷阻止 queued 工作；已 running 的工作完成後才結束授權，避免中途終止非冪等系統操作。本版只有 mock，但已固定此語意。
- 日誌只有固定 mock 階段與結果，不含 subscriber、密碼或憑證。HTTP logging 不記錄 query/header/body；panic 也不 dump request。
- 不提供任意 shell，不把 Agent 角色當成人類核准，也不依 Codex 的 OS 權限選項決定部署授權。

## 測試與證據

```bash
go test -race ./backend/deployment ./backend/WebUI ./cmd/rbac-bootstrap ./cmd/rbac-demo
python3 -m unittest discover -s tools -p 'test_deployment_mcp.py'
node frontend/node_modules/typescript/bin/tsc --noEmit -p frontend/tsconfig.json
```

可直接執行 `python3 tools/run_mock_tests.py`：會建置測試 binary、啟動專用 Mongo 程序、執行測試，最後只停止自己建立的程序並保留 evidence。若 port 已有人使用則拒絕，不接管既有服務。前端先依上方指引建置；安裝 Playwright 的開發環境可加 `--browser` 產生四張截圖。

`tools/test_mock_e2e.py` 使用專用 Mongo port，建立唯一 `rbac_test_*` DB，真實驗證 login、MCP、八個並行核准、背景 mock、重啟、舊 admin 身分/密碼保留與角色遷移、tenant scope、帳號接管拒絕、撤銷與秘密不外洩。需要先建置測試 binary，參數可覆寫；預設 binary 位於 `/tmp/free5gc-rbac-{bootstrap,demo}`。不清除 evidence DB。

`tools/test_browser.cjs` 驗證實際 React 操作：Agent 管理、stdio MCP 申請、一次點擊、自動 running/result polling、無核准權限時隱藏按鈕，以及直接 API 呼叫被拒絕。需先启动隔離 demo 並提供 0600 JSON fixture（url、owner、password、state、evidence）與 `PLAYWRIGHT_MODULE`。fixture 不提交到 repo。

尚未驗證新功能實際執行官方腳本、privileged worker、NF 啟動或 UE end-to-end。不能把舊 PoC 的部署成功或這版 mock 結果當成新 Webconsole 真實部署驗證。
