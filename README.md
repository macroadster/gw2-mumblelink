# gw2-mumblelink

Read-only Guild Wars 2 MumbleLink decoder. PoC only.

The game writes a 5460-byte snapshot. This tool decodes a copy of that block. It does not open `Gw2-64.exe`, inject anything, or send input. `processId` in the block is a label, not a handle.

GW2 sets `context_len` to 48 (the part Mumble uses to tell shards apart) and still writes compass, continent coords, and `mountIndex` after that. The decoder reads those anyway.

## What you need on the game machine

Windows named mapping: `MumbleLink`. On Windows, `read` opens that mapping in the same session as `Gw2-64.exe`. On macOS, Guild Wars 2 under Wine (Sikarugir, CrossOver, Whisky) keeps that name inside the wineserver. `read` finds the running `Gw2-64.exe`, builds a Windows reader, and runs it in that prefix. Be in the world on a character. `-mumble 0` turns the link off. Run the tool as the same user as the game.

A copied 5460-byte file still works with `dump`.

## Use

```
go test ./...
go run ./cmd/gw2-mumblelink read
go run ./cmd/gw2-mumblelink where
go run ./cmd/gw2-mumblelink dump ./path/to/MumbleLink
go run ./cmd/gw2-mumblelink mcp
GW2ML_TOKEN=choose-a-secret go run ./cmd/gw2-mumblelink serve -listen 0.0.0.0:8765
```

On Windows the same commands are `gw2-mumblelink.exe read`, `where`, `dump`, `mcp`, and `serve`. Build it from this tree with:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o gw2-mumblelink.exe ./cmd/gw2-mumblelink
```

`gw2-mumblelink.exe` in this directory is that build. Copy it to the Windows machine that is running Guild Wars 2. `set GW2ML_TOKEN=choose-a-secret` before `serve` when the listen address is not loopback.

`mcp` speaks MCP on stdin. `serve` speaks the same tools over Streamable HTTP at `POST /mcp`, so another model on another machine can connect. Listening beyond loopback requires `GW2ML_TOKEN`. The client sends `Authorization: Bearer <token>`.

```json
{
  "mcpServers": {
    "gw2-mumblelink": {
      "url": "http://THIS_MAC:8765/mcp",
      "headers": { "Authorization": "Bearer choose-a-secret" }
    }
  }
}
```

Tools: `get_link` (one frame) and `where_am_i` (map name, nearest landmarks, turn). Optional argument `path`, or set `MUMBLELINK_PATH`. `where_am_i` also takes `limit`.

Server address bytes are left out of the JSON. Mount names match the wiki enum (0 none, 1 jackal, 2 griffon, 3 springer, 4 skimmer, 5 raptor, 6 roller beetle, 7 warclaw, 8 skyscale, 9 skiff, 10 siege turtle).
