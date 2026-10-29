# The demo GIF — what is real, what is not

`orch-demo.gif` (also `site/assets/orch-demo.gif`) was recorded on 2026-10-29
from [`GIF-SCRIPT.md`](GIF-SCRIPT.md), on a headless Linux host, with the
**released `v0.29.0` binary** (`orch version v0.29.0`). 29 s, 979×694, 12 fps,
331 KiB, infinite loop. Everything needed to rebuild it is in
[`gif-source/`](gif-source/).

## Real

- Every command on screen ran for real, in the order shown, in one take:
  `orch init billing-api --template python-api`, `orch tasks`,
  `orch estimate`, `orch run`, `gh pr list`, `gh pr checks 4`.
- `orch run` dispatched both template tasks to **`claude`**
  (`claude/claude-sonnet-4-6`, Claude Code 2.1.312), each in its own worktree,
  each opened a PR on the throwaway repo
  [`hectorcanaimero/billing-api`](https://github.com/hectorcanaimero/billing-api)
  (#4, #5), and both PRs passed `orch-ci` on GitHub Actions. The run ended
  `2 done, 0 blocked`.
- The client page frames are screenshots of `orch dashboard --profile
  stakeholder` serving that same project, taken every 3 s by headless Chrome
  while the run went on (one page load; the page refreshed itself). The
  percentages (0 % → 50 % → 100 %) and the ETA are what it showed.
- Cost of the take that is in the GIF: **$0.57** as orch prices it (F0.T1
  $0.3023, F1.T1 $0.2711, `orch status`). It ran on a Claude Max subscription,
  so nothing was billed per token. Three earlier takes (below) dispatched about
  six more attempts of the same size; their state was discarded with them.

## Compressed (uniformly, never cut)

| Segment | Real | In the GIF |
|---|---|---|
| cut 1 `orch init` | 6.8 s | 3.5 s (1.4×, pauses capped at 1 s) |
| cut 2 `orch tasks`, `orch estimate` | ~4 s | 3 s (1.4×) |
| cut 3 `orch run` | 181 s | 9 s (20.2×) |
| cut 4 `gh pr list`, `gh pr checks` | 17.7 s | 3.4 s (1.4×, pauses capped at 0.8 s; `gh` spent most of it on "Working…") |
| cut 5 client page | ~5 min of screenshots, 105 frames | 5 s + 1.5 s hold on the last frame |

`gif-source/merge.py` does the retiming; its output is the table above.

## Set up off camera (all on the throwaway project, none in orch)

1. `git init`, the GitHub repo, first push — as the script says.
2. `.orchestrator/config.yaml`: `report_findings.enabled: false` (so no agent
   could file a public issue on orch from a demo), `verify.enabled: false`
   (it would add a review by a second installed provider), `budget.per_dispatch_usd: 2.0`
   (a cap; never reached), `dashboard.language: en` (the default is Spanish).
3. **The two spec files the template points at** (`specs/f0-foundation.md`,
   `specs/f1-health.md`). The template ships without them (`orch validate`
   warns `spec.missing`); writing them is the user's job. They name the
   setuptools backend and tell the agent to check its work in a throwaway venv.
4. `.claude/settings.json`: allowed `python3 -m venv`, `.venv/bin/pip`,
   `.venv/bin/pytest`, `.venv/bin/python`, so the agent could run the tests
   before pushing — without it claude's `pip`/`pytest` calls are denied.
5. `orch-ci.yml`: `pytest || [ $? -eq 5 ]` — a workaround for **#500** (see
   take 3). With the smoke test the specs ask for, F0.T1 would likely pass
   without it; it was not re-shot to find out.

## What the script asked for that this GIF does not show

- **Three CLIs** (the 1.0 gate). Only `claude` is installed and logged in on
  the recording host (`agy` is installed; `codex`, `opencode`, `gemini` are
  not). One provider is also what the script's own "Provider and model"
  section prescribes. A three-CLI take needs those CLIs logged in.
- **Two dispatch lines in the same second.** The `python-api` template's DAG is
  serial (F1.T1 depends on F0.T1), so the parallelism beat cannot happen with
  it. And `orch run` v0.29 prints no per-dispatch `dispatch`/`success`/
  `pr_created` lines at the default log level — the screen shows "waiting on
  CI" and the closing summary with both PR URLs. A parallel take needs a
  template (or a third task) with two independent tasks.
- The script's `orch --dry-run` and `orch --max-tasks 2` no longer exist; the
  script now says `orch tasks` / `orch estimate` and `orch run`.

## The takes that are not in it

1. **Take 1** — private repo: every `orch-ci` job "failed" in 2 s with *"The
   job was not started because recent account payments have failed or your
   spending limit needs to be increased"*. orch read that as a red CI,
   re-dispatched the agent, then blocked the task → **#501**. The demo repo was
   made public (Actions are free there) and the take thrown away.
2. **Take 2** — public repo, no specs: the agent, denied `pip`, shipped an
   invalid build backend (`setuptools.backends.legacy:build`); the CI retry
   fixed it, and then `pytest` exited 5 (no tests in a scaffold task) →
   **#500**. Blocked.
3. **Take 3** — with the exit-5 workaround: the install step failed again
   (same blind `pip`). Blocked.
4. **Take 4** — specs written, venv-only `pip`/`pytest` allowed: both tasks
   done, both PRs green. This is the GIF.

The honesty rule held: no take was edited into another, and the GIF is take 4
end to end.

## Rebuild

```bash
# needs: asciinema 3.x, agg 1.9, ffmpeg, uv (for playwright), google-chrome
cd docs/media/gif-source
# record: cut1.sh, cut23.sh, cut4.sh under `asciinema rec --headless --window-size 100x30`,
# with `orch dashboard --profile stakeholder --token demo-token` and
# `uv run --with playwright python shoot.py frames 2400 3` running during cut 3,
# then convert each cast with `asciinema convert -f asciicast-v2`.
python3 merge.py 9                     # → terminal.cast + the timing table
agg --font-family "Liberation Mono" --font-size 16 --theme monokai \
    --fps-cap 12 --last-frame-duration 1.2 terminal.cast terminal.gif
i=0; for f in $(ls frames/*.png | sort); do cp "$f" bseq/$(printf %04d $i).png; i=$((i+1)); done
ffmpeg -y -i terminal.gif -framerate 21 -i bseq/%04d.png -filter_complex \
  "[0:v]fps=12,scale=979:694:flags=lanczos,setsar=1[t];\
   [1:v]crop=1100:780:170:0,scale=979:694:flags=lanczos,fps=12,tpad=stop_mode=clone:stop_duration=1.5,setsar=1[b];\
   [t][b]concat=n=2:v=1:a=0,split[s0][s1];[s0]palettegen=max_colors=128:stats_mode=diff[p];\
   [s1][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" -loop 0 orch-demo.gif
```

The casts in `gif-source/` are take 4's, already converted to asciicast v2.
