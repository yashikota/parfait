# parfait

## Install

```sh
go install github.com/yashikota/parfait@latest
```

## Usage

```sh
parfait ./hoge/
```

## Flags

- `--marp`: Generate Marp files only
- `--tts`: Generate TTS only
- `--video`: Create videos only
- `--gemini`: Use Gemini API for TTS generation (default: use local TTS)

## TTS (Text-to-Speech)

### Default: Local TTS (KokoVox)

By default, parfait uses a local TTS service ([KokoVox](https://github.com/yashikota/kokovox)).

**Prerequisites:**

- KokoVox service must be running at `http://localhost:5108` (or set `KOKOVOX_URL` environment variable)

**Environment Variables:**

- `KOKOVOX_URL`: KokoVox service URL (default: `http://localhost:5108`)

The KokoVox service healthcheck is executed at startup by calling `{KOKOVOX_URL}/health`. If the service is not available, an error is returned.

### Option: Gemini API

To use Gemini API, specify the `--gemini` flag.

```sh
parfait --gemini ./hoge/
```

**Prerequisites:**

- Set `GOOGLE_API_KEY` environment variable or in `.env` file
- Multiple API keys can be set using `GOOGLE_API_KEY_1`, `GOOGLE_API_KEY_2`, etc.
