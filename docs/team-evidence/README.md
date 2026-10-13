# 真實 Codex Agent Team 驗收（2026-10-13）

透過 Webconsole 的 Prompt 表單，使用 Codex app-server 0.162.1、既有 ChatGPT 登入與實際 default model `gpt-6.1-sol`。使用隔離 MongoDB 與測試人類帳號，沒有假模型／fake MCP 結果，沒有安裝、重啟 NF 或修改系統權限。

Run：`227c5931-9dc8-4ccd-a6a6-7d89091583e8`，結果 completed，共 98 個保存事件。

Prompt 要求準備 MongoDB、Go 安裝計畫，並檢查 NF／MongoDB 狀態。實際觀察到：

- Coordinator 透過 `delegate_task` 分派 Installer 與 Troubleshooter。
- Installer 的實際 `tools/list` 只有官方指南、相依檢查、MongoDB／Go 計畫工具。
- Troubleshooter 的實際 `tools/list` 只有 NF 狀態、MongoDB 狀態與遮蔽日誌工具，不含 install 工具。
- Installer 實際呼叫 `install_mongo`、`install_go`，server 結果均為 `plan_created`、`executed=false`。
- Troubleshooter 實際呼叫 `get_nf_status`、`check_mongodb_status` 和各元件的 `read_redacted_logs`。
- 第一個 Installer 回合曾遇到真實模型 capacity error；Coordinator 先取得排錯結果，再重新委派 Installer。失敗、理由、後續選擇均保留，沒有用 fake 成功掩蓋。
- 瀏覽器顯示即時分派、工具選擇與完整回覆，無 React runtime error。重新啟動測試 backend 後，可讀取同一份已完成 run 並重新截圖，沒有再呼叫模型。

證據：

- [表單與團隊待命](01-team-ready.png)
- [即時分派](02-team-delegating.png)
- [完成畫面](03-team-completed.png)
- [三個角色及實際 toolbox](04-role-toolboxes.png)
- [決策時間軸](05-decision-timeline.png)
- [完整公開事件及工具結果 JSON](real-team-run.json)

另通過 Go/race、Go vet、TypeScript、production build、7 項 Python MCP/RBAC tests 和 Team HTTP 權限測試。Role 隱藏、直接越權 call、身分偽造、未知參數與任意日誌路徑均被拒絕。unit tests 有隔離的 test double；此份端到端紀錄的模型與工具事件為真實執行。

completed 只代表團隊回合完成。工具選擇由測試 assertion 及公開證據檢查，不宣稱已安裝 free5GC、NF 健康、UE 已可上網，或所有可能 Prompt 都會選擇正確工具。
