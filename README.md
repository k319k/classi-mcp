# classi-mcp

**非公式** Classi MCP サーバー — Go + resty、Playwright 完全撤去、純 REST API。

自称進学校の、自称進学校による、自称進学校のためのツールです。  
Classi の校内グループ・お知らせ・学習記録・カレンダーに AI エージェント（Hermes Agent 等）からアクセスできます。

> ⚠️ このツールは非公式であり、Benesse Corporation とは一切関係ありません。
> Classi の利用規約を確認の上、自己責任で使用してください。

## できること

- 校内グループ一覧・投稿閲覧
- お知らせ（未読数付き）
- 学習記録の取得・編集（GET/PUT）
- カレンダー予定取得
- Cookie ベース認証（ブラウザ不要）
- 機能ごとの有効/無効を環境変数で制御
- 投稿機能（デフォルト無効、誤爆防止）

## クイックスタート

### 1. ビルド

```bash
go build -o classi-mcp .
```

### 2. 環境変数

```env
CLASSI_ID=your_classi_id
CLASSI_PASSWORD=your_classi_password
CLASSI_ENABLE_POST=false
```

### 3. Hermes Agent に登録

`~/.hermes/config.yaml` に追加：

```yaml
mcp_servers:
  classi:
    command: /path/to/classi-mcp
    env:
      CLASSI_ID: your_id
      CLASSI_PASSWORD: your_password
      CLASSI_ENABLE_POST: "false"
      CLASSI_ENABLE_STUDY: "true"
      CLASSI_ENABLE_CALENDAR: "true"
```

### 4. 再起動

```bash
hermes gateway restart
```

## ツール一覧

| ツール名 | 説明 |
|---|---|
| `classi_groups` | 校内グループ一覧（未読数付き） |
| `classi_new_messages` | 全グループの最新投稿 |
| `classi_group_messages` | 特定グループの投稿一覧 |
| `classi_read_message` | 投稿詳細（コメント数、既読数含む） |
| `classi_notifications` | お知らせ + 未読総数 |
| `classi_calendar` | カレンダー予定 |
| `classi_study_form` | 学習記録の既存データ取得 |
| `classi_study_quick` | 学習記録をかんたん入力 |
| `classi_study_save` | 学習記録をJSONで保存 |
| `classi_post_message` | グループに投稿（要 `CLASSI_ENABLE_POST=true`） |

## 環境変数

| 変数 | 既定値 | 説明 |
|---|---|---|
| `CLASSI_ID` | (必須) | Classi ログインID |
| `CLASSI_PASSWORD` | (必須) | Classi パスワード |
| `CLASSI_ENABLE_POST` | `false` | 投稿機能（誤爆防止のため既定無効） |
| `CLASSI_ENABLE_STUDY` | `true` | 学習記録機能 |
| `CLASSI_ENABLE_CALENDAR` | `true` | カレンダー機能 |

## 技術スタック

- **Go** + [resty](https://github.com/go-resty/resty) — 純 REST API クライアント
- **MCP** (Model Context Protocol) — stdio JSON-RPC 2.0 トランスポート
- ブラウザ自動化不要 — Playwright / Chromium 不使用
- 認証は `id-api.classi.jp` の REST API で完結（CSRF トークン + Cookie）

## 動作の仕組み

```
classi-mcp (Go binary, ~8MB)
  │
  ├─ id-api.classi.jp ── ログイン (CSRF + Cookie)
  ├─ platform.classi.jp ─ グループ・お知らせ・カレンダー
  └─ study.classi.jp ─── 学習記録 (GET/PUT)
```

## 参考

- [Kokyaneko/classi-study-record-editor](https://github.com/Kokyaneko/classi-study-record-editor) — Classi学習記録エディタ（ブックマークレット）
  - 学習記録の API エンドポイントとデータ構造の参考にさせていただきました

## ライセンス

GPLv3

---

> 自称進学校の、自称進学校による、自称進学校のためのツールです。
> Classi と学習記録を強要された自称進学校の効率厨の皆さんにぜひとも使っていただきたいと思います。
