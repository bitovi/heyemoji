# HeyBitovi docs

Diagrams are self-contained HTML files (inline SVG). Open them in a browser. `give-flow.png` is a static export of `give-flow.html` for embedding.

| Diagram | Shows |
|---|---|
| [architecture.html](architecture.html) | The heyemoji pod on Bitovi Platform: the slash-command bot (Slack Socket Mode), the web UI (Google sign-in), and their shared Postgres. |
| [give-flow.html](give-flow.html) | `/heybitovi give` end to end: daily-cap check, the #general announcement (threaded per recipient per day), the copy in the source channel, the DM confirmation, and the cap-exceeded reply. |
| [web-flow.html](web-flow.html) | Viewing the web UI: Google OAuth restricted to bitovi.com, the 7-day session cookie, then pages built from Postgres plus Slack name lookups. |

`leaderboard`, `points`, and `help` aren't diagrammed. Each one is a single private reply in Slack.
