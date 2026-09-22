# heyemoji 🏆 👏 ⭐
# 

[![Go Report Card](https://goreportcard.com/badge/github.com/bitovi/heyemoji)](https://goreportcard.com/report/github.com/bitovi/heyemoji) [![Doc](https://godoc.org/github.com/bitovi/heyemoji?status.svg)](http://godoc.org/github.com/bitovi/heyemoji) [![License](http://img.shields.io/:license-mit-blue.svg)](http://doge.mit-license.org)

The `heyemoji` bot (Slack-facing name: **HeyBitovi**) is a self-hosted slack reward system that allows team members to recognize eachother for anything awesome they may have done.  This is accomplished with the `/heybitovi give` slash command, naming a **@username** along with a pre-configured **reward emoji** and an optional **reason** for what they did.  The emoji points bestowed to users can be tracked via leaderboards.

## Table of Contents

- [Usage](#basic-usage)
- [Setup](#setup)
- [Web UI Setup](#web-ui-setup)
- [Configuration](#configuration)

## Basic Usage

#### Give a single user emoji points

`/heybitovi give @michael.bolton :star: Great job filling out those TPS reports`

#### Give multiple users emoji points

`/heybitovi give @michael.bolton @samir :star: Thanks for coming in this weekend`

#### Give multiple emojis to a user

`/heybitovi give @petergibbons :star: :trophy: :clap: :clap: Found my red stapler!`

### Bot Commands

| Name                                                | Description                                                |
|------------------------------------------------------|------------------------------------------------------------|
| `/heybitovi give @user :emoji: [reason]`               | give someone recognition, with an optional reason           |
| `/heybitovi leaderboard <day\|week\|month\|quarter\|year\|all>` | see the top point earners for a period            |
| `/heybitovi points`                                    | see how many emoji points you have left to give            |
| `/heybitovi help`                                      | get help with how to send recognition emoji                |

## Setup

`heyemoji` talks to Slack over **Socket Mode**, so it needs a Bot User OAuth Token (`xoxb-...`) and an app-level token (`xapp-...`) — no public URL or Request URL is required to run it locally.

### 1. Create the Slack app

The fastest way is from a manifest, which sets up the bot user, slash command, scopes, and Socket Mode all at once.

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and click **Create New App** > **From an app manifest**.
2. Pick the workspace you want to test in (use a personal/sandbox workspace or a dedicated test workspace if you don't want this showing up for everyone yet).
3. Choose **YAML**, paste the manifest below (rename `name` / `display_name` if you're creating more than one test app), and click **Next** > **Create**.

   ```yaml
   display_information:
     name: HeyBitovi
     description: Give teammates recognition with emoji points
     background_color: "#4A154B"
   features:
     app_home:
       home_tab_enabled: false
       messages_tab_enabled: true
       messages_tab_read_only_enabled: false
     bot_user:
       display_name: HeyBitovi
       always_online: true
     slash_commands:
       - command: /heybitovi
         description: Give recognition, check the leaderboard, and more
         usage_hint: "give @user :emoji: [reason] | leaderboard <day|week|month|quarter|year|all> | points | help"
         should_escape: false
   oauth_config:
     scopes:
       bot:
         - chat:write
         - commands
         - users:read
   settings:
     org_deploy_enabled: false
     socket_mode_enabled: true
     token_rotation_enabled: false
   ```

   Don't want to use a manifest? Click through it manually instead: **Slash Commands** > create `/heybitovi` (leave Request URL blank — Socket Mode delivers it); **OAuth & Permissions** > **Scopes** > add the `chat:write`, `commands`, and `users:read` **Bot Token Scopes**; **Socket Mode** > toggle it on; **App Home** > **Show Tabs** > enable **Messages Tab** and check **"Allow users to send Slash commands and messages from the messages tab"** (without this, DMing the bot shows "Sending messages to this app has been turned off" and you can't type anything into the DM, slash commands included).

   Renaming an app you already created? **Basic Information** > **Display Information** > update **App Name**; **App Home** > **Your App's Presence in Slack** > update the bot's **Display Name**; **Slash Commands** > edit the existing command's **Command** field from `/heyemoji` to `/heybitovi` (Slack lets you rename a command in place, no need to delete and recreate it).

### 2. Generate your tokens

1. **App-level token** (for `HEY_SLACK_APP_TOKEN`): **Basic Information** > **App-Level Tokens** > **Generate Token and Scopes**. Give it any name, add the `connections:write` scope, click **Generate**, and copy the `xapp-...` token.
2. **Bot token** (for `HEY_SLACK_BOT_TOKEN`): **OAuth & Permissions** > **Install to Workspace** (or **Install App**, if this is a reinstall) > **Allow**. Copy the **Bot User OAuth Token** (`xoxb-...`) from the top of that page.

### 3. Configure and run

1. Create a `.env` file in the `heyemoji` root folder:
   ```
   HEY_SLACK_BOT_TOKEN=xoxb-...
   HEY_SLACK_APP_TOKEN=xapp-...
   DATABASE_URL=postgres://<user>:<password>@postgres:5432/heyemoji?sslmode=disable
   ```
   (user/password are `heyemoji`/`heyemoji`, matching the Postgres service already defined in `docker-compose.yaml` — no separate setup needed. `DATABASE_URL` is the one setting without a `HEY_` prefix: in production it's injected by the platform's Postgres dependency under this exact, fixed name.)
2. Run `docker-compose up --build`. The app will run its Postgres migrations on startup and connect to Slack over Socket Mode.
3. In Slack, invite the bot to a channel you want to test in: `/invite @HeyBitovi`.
4. Try it out: `/heybitovi help`, then `/heybitovi give @someone :star: nice work`, then `/heybitovi leaderboard`.

Each teammate testing separately should create their own app this way (or share one app's tokens) — a Slack app, its slash command, and its tokens are scoped to whichever workspace you installed it into in step 1.

## Web UI Setup

A small read-only web UI (leaderboard + recognition feed) is served from the same container as the bot, at whatever port `HEY_HTTP_PORT` is set to (default `8080`). It's gated by **Google Sign-In** (not Slack) — Bitovi already has Google OAuth set up, so this reuses that instead of building a separate Slack login flow. The web server simply doesn't start until all three of `HEY_GOOGLE_CLIENT_ID`, `HEY_GOOGLE_CLIENT_SECRET`, and `HEY_SESSION_SECRET` are set (you'll see `web UI disabled: ...` in the logs until then) — the Slack bot itself works fine either way.

**For local testing without setting up Google OAuth at all**, set `HEY_WEB_AUTH_DISABLED=true` — this starts the web UI with no sign-in required, reachable by anyone who can reach the port. It logs a loud warning on startup so it can't accidentally stay on unnoticed. Never use this for anything other than local testing.

### 1. Create a Google OAuth Client

1. Go to the [Google Cloud Console credentials page](https://console.cloud.google.com/apis/credentials) for the project you want this under (an existing Bitovi project, or a new one).
2. If you haven't already, configure the **OAuth consent screen** (APIs & Services > OAuth consent screen): User type **Internal** if this is a Google Workspace org (restricts sign-in to your org automatically) or **External** otherwise; app name `HeyBitovi` is fine.
3. **Create Credentials** > **OAuth client ID** > Application type **Web application**.
4. Under **Authorized redirect URIs**, add `{HEY_BASE_URL}/auth/google/callback` — for local testing with the defaults below, that's `http://localhost:8080/auth/google/callback`.
5. Create it, then copy the **Client ID** and **Client Secret**.

### 2. Configure and run

Add to your `.env`:
```
HEY_GOOGLE_CLIENT_ID=...
HEY_GOOGLE_CLIENT_SECRET=...
HEY_SESSION_SECRET=some-long-random-string
HEY_GOOGLE_ALLOWED_DOMAIN=bitovi.com
```
`HEY_SESSION_SECRET` just needs to be a long random string (e.g. `openssl rand -hex 32`) — it signs the login session cookie. `HEY_GOOGLE_ALLOWED_DOMAIN` is optional but recommended: if set, only Google Workspace accounts on that domain can sign in (anyone else gets a 403); leave it unset to allow any Google account (fine if you already locked this down with an **Internal** OAuth consent screen in step 1).

Restart (`docker-compose up --build`), then visit `http://localhost:8080` — it redirects straight into Google Sign-In, and lands on the leaderboard once you're signed in.

## Configuration

| ENV Var | Default  | Required | Note |
|:---:|:---:|:---:|:---:|
| HEY_BOT_NAME  | HeyBitovi | No | The display name of the bot (currently unused by the app itself; the name shown in Slack is whatever you set on the Slack app) |
| DATABASE_URL | | Yes | Postgres connection string. No `HEY_` prefix — in production this is injected by the platform's Postgres dependency under this exact, fixed name. |
| HEY_SLACK_BOT_TOKEN | | Yes | The Bot User OAuth Token for the Slack API |
| HEY_SLACK_APP_TOKEN | | Yes | The app-level token used for Socket Mode |
| HEY_SLACK_EMOJI | star:1 | No | Comma delimited set of emoji "name:value" pairs |
| HEY_SLACK_DAILY_CAP | 5 | No | The max number of emoji points that can be given out in a day |
| HEY_MAX_LEADER_ENTRIES  | 10 | No |  Max number of entries contained in the leaderboards |
| HEY_TEST_MODE | false | No | When `true`, disables the daily give cap entirely (everyone has unlimited points). Every place that would normally show the cap as a number instead says "unlimited (test mode)". Not for production use. |
| HEY_HTTP_PORT | 8080 | No | Port the web UI listens on |
| HEY_BASE_URL | http://localhost:8080 | No | Public URL the app is reachable at; used to build the Google OAuth redirect URI |
| HEY_SESSION_SECRET | | Only for web UI | Random string used to sign the web UI's login session cookie |
| HEY_GOOGLE_CLIENT_ID | | Only for web UI | Google OAuth Client ID |
| HEY_GOOGLE_CLIENT_SECRET | | Only for web UI | Google OAuth Client Secret |
| HEY_GOOGLE_ALLOWED_DOMAIN | | No | If set, restricts web UI sign-in to Google Workspace accounts on this domain (e.g. `bitovi.com`) |
| HEY_WEB_AUTH_DISABLED | false | No | When `true`, the web UI starts with no sign-in required at all, bypassing Google OAuth entirely. For local testing only - never for anything reachable by anyone but you. |


### Specifying Custom Reward Emoji

The `HEY_SLACK_EMOJI` setting lets you specify multiple different reward emoji as well as different point values for each. So, if you wanted the following emoji and reward values:

| Emoji         | Value  |
|---------------|--------|
| ⭐             | 1      |
| 👏             | 2      |
| 🏆             | 3      |

You would specify the `HEY_SLACK_EMOJI` as: `star:1,clap:2,trophy:3`

## Deploy

`heyemoji` runs on Bitovi's internal Kubernetes platform (GitOps via Argo CD). The whole app is described by [`deploy/values.yaml`](deploy/values.yaml) — workload, its Postgres dependency, and its domain (`heybitovi.bitovi-tools.com`). Secrets (Slack/Google tokens, session secret) live in the `bitovi-platform/heyemoji-secrets` 1Password item, synced in via External Secrets Operator; see the comments at the top of `deploy/values.yaml` for the exact field labels it expects.

Pushing a `vX.Y.Z` tag runs [`.github/workflows/deploy.yml`](.github/workflows/deploy.yml): tests, then builds and pushes the image, then writes the new tag back into `deploy/values.yaml`, which Argo CD picks up and rolls out. Every pull request against `main` runs [`.github/workflows/test.yml`](.github/workflows/test.yml).