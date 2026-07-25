# InstantDB backend — cloud character library

Backs the **multi-user** mode of the standalone web app
(`dist/character-builder-app.html`, served from GitHub Pages).

- **App:** IDD Character Builder
- **App ID:** `5b6fa3d8-d86a-4142-ace3-6ab913121ef3` — this is the *public*
  client id and is embedded in the web app on purpose. The **admin token is a
  secret**: it is not stored in this repo, and the client never needs it
  (all access is governed by the permission rules below).

## Model

`characters` — one row per saved character, linked `owner → $users`:

| field | why |
|---|---|
| `data` (json) | the full build object the app produces (`buildJSON()`) — the source of truth |
| `name`, `heritage`, `faction` | denormalized so the library list renders without parsing every blob |
| `valid`, `cpSpent`, `cpAvailable` | list badges ("legal", `33/35 CP`) |
| `createdAt`, `updatedAt` (indexed) | sorting |

## Access model — private libraries

`instant.perms.ts` allows `view/create/update/delete` only when
`auth.id in data.ref('owner.id')`. Every player sees and edits **only their own**
characters; there is no shared/party view. Signed-out visitors can still use the
builder offline (import/export/share-link) — they just get no cloud library.

Sign-in is **email magic code** (no passwords, no OAuth setup).

## Applying changes

Edit `instant.schema.ts` / `instant.perms.ts`, then push (the CLI reads the
files in this directory):

```bash
cd instant
npx instant-cli push schema --app 5b6fa3d8-d86a-4142-ace3-6ab913121ef3 -p core --yes
npx instant-cli push perms  --app 5b6fa3d8-d86a-4142-ace3-6ab913121ef3 --yes
```

The InstantDB MCP server (`claude mcp add instant -s user -t http
https://mcp.instantdb.com/mcp`) can also read schema/perms, but it delegates
pushes to this same CLI. Note its OAuth token carries schema/perms scopes only —
not `data-read`/`data-write` — so it cannot query or seed rows.

## Client

The app loads `@instantdb/core` (framework-agnostic — the app is vanilla JS, not
React) as an ES module from a CDN. If that import fails, the cloud button hides
itself and the offline builder keeps working.
