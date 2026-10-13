# Mock 驗收紀錄（2026-10-13）

使用隔離 MongoDB、實際 Go/Gin login/API、React production build、Python stdio MCP 和 Playwright Chromium。沒有執行 quick-setup、sudo、free5GC 安裝或系統權限變更。

| 驗證 | 結果 |
|---|---|
| Go unit/API tests + race detector | 通過，含權限、跨 owner/tenant/Agent、grant 消耗、拒絕/到期/撤銷、並行核准、host 排他、計畫竄改、heartbeat crash、mock 失敗與 panic redaction |
| Go vet / server build | 通過 |
| TypeScript `tsc --noEmit` | 通過 |
| React Vite production build | 通過；既有單一 bundle 仍有大小提示 |
| MCP lifecycle/tool tests | 2 項通過；無 approve、set_role 或 shell 工具 |
| Mongo + REST + MCP E2E | 通過；8 個並行核准只建立 1 job，自動背景工作、真實登入、重啟保留身分/工作、scope、revocation、秘密不外洩 |
| 舊 API 的帳號接管負向測試 | 通過；限制跨 tenant、拒絕重設 owner 密碼、禁止刪除有效 owner、拒絕已知預設密碼升為 Admin |
| 實際瀏覽器 | 通過；UI 建立 Agent → stdio MCP 建立請求 → 一次點擊核准 → 自動 running/succeeded → 普通使用者無按鈕且 API 403 |

截圖只有測試身分，不包含密码、Agent token 或 JWT：

- [pending](01-pending.png)：不可變計畫與「核准並部署」。
- [running](02-running.png)：後端自動工作與更新階段。
- [succeeded](03-succeeded.png)：mock 日誌、exit code 0、明確的 mock 驗證範圍。
- [Diagnostician](04-diagnostician.png)：無核准控制項。

所有 Mongo 測試資料與子程序日誌位於輸出的 `/tmp/free5gc-rbac-tests-*`，不接觸原 free5GC、PoC DB 或部署產物。瀏覽器所需的額外函式庫與字型只解開到 `/tmp`，沒有安裝系統套件。

本驗收不包含新功能的真實 privileged worker、官方腳本執行、NF 啟動或 UE 上網。舊 PoC 的 `req-043fc3344924` 曾回報 succeeded/exit 0，是不同工作的唯讀查詢證據。
