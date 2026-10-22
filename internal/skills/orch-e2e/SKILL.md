---
name: orch-e2e
description: Use when an orch task asks for end-to-end tests of a web app (an E2E task, usually the last of a phase, writing tests/e2e/*.spec.ts). Bring the app up, explore it in a real browser through its accessibility tree, and write Playwright specs with role-based locators that fail when a user-visible behaviour breaks — then run them against the app until they pass.
---

# orch-e2e — explore the app, leave Playwright specs behind

An E2E task tests what a person using the app would notice: they sign in, they
add to the cart, they pay. Unit tests already cover the functions; this task
covers the screens. What you leave behind is **specs in the repository**, which
orch's local verify runs on every later task, so a regression in a flow you
covered sends that task back to its agent.

## 1. Know what to cover

- The task's description and spec ref list the flows (*Done when*). Each flow
  is one `test()`; its acceptance criteria are its assertions.
- Read the specs and the PRD's requirements the flows come from
  (`docs/prd/`), so a test checks what the product promised, not what the
  code happens to do today. Where the app disagrees with the PRD, do not
  encode the bug as expected: block the task and say which requirement fails.

## 2. Bring the app up

Use the project's own way, in this order:

1. `verify.app` in `.orchestrator/config.yaml`: run its `start` command in your
   working dir; its `url` is where the app answers (the shell expands it —
   `ORCH_TASK_SLOT` is already in your environment, so your app does not take
   another task's port). Run its `stop` command when you are done.
2. A `webServer` block in `playwright.config.ts`: Playwright starts the app
   itself when the tests run.
3. Neither: start the dev server the README names, in the background, on a
   port derived from `ORCH_TASK_SLOT`, and add a `webServer` block so the
   tests can start it without you.

Wait until the URL answers before exploring. If the app does not start, that is
the finding: block the task with the error rather than writing tests against
nothing.

## 3. Explore through the accessibility tree

Do not guess selectors from the source. Look at what the browser exposes, the
way a screen reader would:

- With a browser tool (Playwright MCP's `browser_navigate` and
  `browser_snapshot`), open each screen of the flow and read its snapshot.
- Without one, write a throwaway script and run it with node:

  ```js
  import { chromium } from '@playwright/test'
  const browser = await chromium.launch()
  const page = await browser.newPage()
  await page.goto(process.env.ORCH_APP_URL ?? 'http://localhost:3000')
  console.log(await page.locator('body').ariaSnapshot())
  await page.getByRole('link', { name: 'Sign in' }).click()
  console.log(await page.locator('body').ariaSnapshot())
  await browser.close()
  ```

  Delete it when you are done; it is not a test.

The snapshot names the roles and accessible names you will locate by. A control
with no accessible name is worth a line in your done note — it is an
accessibility bug as well as a test that has nothing stable to hold.

## 4. Write the specs

- One file per area, under the directory the task's *Files* list
  (`tests/e2e/checkout.spec.ts`). Reuse the project's `playwright.config.ts`;
  create one only if there is none, with `baseURL: process.env.ORCH_APP_URL`,
  `screenshot: 'only-on-failure'`, `video: 'retain-on-failure'`,
  `trace: 'retain-on-failure'` and the JSON reporter writing the file
  `verify.e2e_report` names.
- **Locate by role, label and text** — `getByRole('button', { name: 'Pay' })`,
  `getByLabel('Email')` — never by CSS classes or generated ids, which change
  without the behaviour changing.
- **Assert what the user sees**: `await expect(page.getByRole('alert')).toHaveText(…)`,
  not that a request was sent. Web-first assertions wait on their own; never
  add fixed `waitForTimeout` sleeps.
- Wrap each user-meaningful action in `test.step('pay with a card', …)`: the
  step is what orch reports when a test fails, and what the dashboard shows.
- Each test sets up its own state (sign up a fresh user, seed through the
  app's API) and depends on no other test's leftovers or order.
- Cover the failure paths the spec names — a wrong password, an empty cart —
  not only the happy one.

## 5. Make them pass, honestly

Run the specs against the app (`npx playwright test tests/e2e/checkout.spec.ts`).
A spec must fail when its flow is broken: break the flow on purpose once
(comment out the handler, change the expected text) and watch it go red, then
restore it. A spec that cannot fail is not covering anything.

A flow that fails because the app is wrong is not yours to fix in this task
unless the task says so: keep the test, mark it `test.fixme()` with the reason,
and name it in your done note so the operator sees it.

## Reporting back

Your done note lists the specs and the flows they cover, which ones are
`fixme` and why, and anything the snapshot showed with no accessible name:

> tests/e2e/checkout.spec.ts — cart, pay with a card, declined card (3 tests,
> all passing against verify.app). The "Pagar" button had no accessible name
> until its icon got an aria-label.
