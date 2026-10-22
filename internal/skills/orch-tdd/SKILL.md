---
name: orch-tdd
description: Use when implementing an orch task test-first (dispatch.tdd on, or a task whose Done when lists tests). One failing test per acceptance criterion, seen failing for the right reason, then the least code that makes it pass, then cleanup — with the repository's own test framework — and a done note that names the tests.
---

# orch-tdd — test-first work on an orch task

The task's spec ref and description say what "done" means. This skill says how
to get there so the tests prove it: **red, green, clean**, one behaviour at a
time.

## Before writing anything

1. Read the spec ref and the description. List the acceptance criteria — the
   *Done when* line, if the spec has one. Each criterion becomes one or more
   tests. A criterion you cannot turn into a test is a question for the
   operator: block the task and ask, rather than guess.
2. Find the test framework **the repository already uses**, and how to run a
   single test with it. Never add a second framework. If the repository has
   none, the architecture under `docs/arch/` names one; if it does not either,
   block and ask.

| The repository has | Tests run with | One test |
| --- | --- | --- |
| `go.mod` | go test | `go test ./pkg/auth -run TestLogin` |
| `package.json` with vitest | vitest | `pnpm vitest run src/auth/login.test.ts` |
| `package.json` with jest | jest | `npx jest src/auth/login.test.ts -t "wrong password"` |
| `pyproject.toml` / `pytest.ini` | pytest | `pytest tests/test_login.py -k wrong_password` |
| `Cargo.toml` | cargo test | `cargo test login::wrong_password` |

Use the package manager the lockfile says (`pnpm-lock.yaml` → pnpm,
`uv.lock` → uv, …), and the test file naming the repository already follows.

## The loop

For each criterion, smallest first:

1. **Red.** Write one test for it. Run that test alone. It must fail, and fail
   **for the reason you expect** — the function is missing, the status is 200
   instead of 401. A test that fails on a typo, an import error or a missing
   fixture has not told you anything yet: fix that and run it again. A test
   that passes before you wrote the code is testing nothing new — change it
   until it fails, or drop it.
2. **Green.** Write the least code that makes that test pass. Not the next
   criterion's code, not the abstraction you expect to need. Run the test.
3. **Clean.** With the test green, tidy what you just wrote: names,
   duplication, dead branches. Run the package's tests again; they stay green.

Then the next criterion. At the end run the whole suite the way the repository
does (its `make test`, `pnpm test`, …) — your change must not break a test that
was passing.

## Bugs and existing behaviour

- **A bug fix starts with a test that reproduces the bug** and fails on the
  current code. Then fix it. A fix without that test can come back unnoticed.
- **Changing behaviour that has no tests**: first write a test that pins what
  the code does today and passes, then change the test to the new behaviour
  and watch it fail, then change the code.

## What not to do

- Do not weaken, skip or delete an existing test to get green. If a test is
  wrong, say so in your done note and why.
- Do not assert on mocks of the thing under test; assert on what a caller sees.
- Do not write all the tests first and all the code after: one criterion at a
  time keeps each failure readable.
- Stay inside the task's *Files you may write*. The test files the task needs
  are normally listed there; if one is not, the dispatch prompt says how to
  name a file outside the list.

## Reporting back

Your done note is what the tasks that depend on this one are shown. Make it
say what the tests prove:

> 3 tests in src/auth/login.test.ts — correct password, wrong password,
> unknown email — failed with "login is not a function", now pass. login()
> answers 401 without saying which part was wrong.

When orch's local verify is on, it runs the repository's checks and a review
before your work is published, and with `dispatch.tdd` on it sends back a
change that touches no test file. If it sends the task back, the prompt points
you at the file that says what failed: fix that, test-first, rather than
starting over.
