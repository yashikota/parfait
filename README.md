# parfait

Markdownスライドからスピーカーノートを抽出してTTS音声ファイルを生成するツール

## インストール

```sh
go install github.com/yashikota/parfait@latest
```

## 使い方

```sh
parfait -lang ja slide.md
parfait -lang en -gemini slide.md
parfait -lang ja -output ./dist slide.md
```

## Gemini APIキーをコマンドで設定（グローバル）

Gemini APIを使う場合、環境変数だけでなく **コマンドでグローバル設定**できます。

```sh
parfait config set api-key YOUR_API_KEY
parfait config add api-key YOUR_API_KEY_2
parfait config list api-keys
parfait config path
```

設定後は `-gemini` 実行時に自動で読み込まれ、`GOOGLE_API_KEY` として利用されます（すでに環境変数が設定されている場合はそちらが優先されます）。

## フラグ

- `-lang`: 言語指定 (ja/en) **[必須]**
- `-gemini`: Gemini APIを使用 (デフォルト: ローカルTTS)
- `-output`: 出力ディレクトリ (デフォルト: 入力ファイルと同じディレクトリ)

## Markdownフォーマット

```markdown
---
title: プレゼンテーションタイトル
---

# スライド1

<!--
このコメントがTTSで読み上げられます
-->

---

# スライド2

<!--
2枚目のスライドのナレーション
-->
```

**出力:**
- `001.wav` (スライド1のコメント)
- `002.wav` (スライド2のコメント)

※ すべてのスライドにコメントが必要です（コメントがないスライドがあるとエラー）

## TTS (Text-to-Speech)

### デフォルト: ローカルTTS (KokoVox)

デフォルトではローカルTTSサービス ([KokoVox](https://github.com/yashikota/kokovox)) を使用します。

**前提条件:**

- KokoVoxが `http://localhost:5108` で起動していること（または `KOKOVOX_URL` 環境変数で指定）

**環境変数:**

- `KOKOVOX_URL`: KokoVoxサービスのURL (デフォルト: `http://localhost:5108`)

### オプション: Gemini API

Gemini APIを使用する場合は `-gemini` フラグを指定します。

```sh
parfait -lang ja -gemini slide.md
```

**前提条件:**

- `GOOGLE_API_KEY` 環境変数 / `.env` ファイル / `parfait config set api-key ...` のいずれかで設定
- 複数のAPIキーを使用する場合は `GOOGLE_API_KEY_1`, `GOOGLE_API_KEY_2` のように設定可能
