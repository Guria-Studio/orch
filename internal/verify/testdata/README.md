# verify testdata

`playwright/<version>/report.json` is a real capture of Playwright's JSON
reporter (`reporter: [['line'], ['json', { outputFile: 'e2e-report.json' }]]`,
with `screenshot: 'only-on-failure'`, `video: 'retain-on-failure'` and
`trace: 'retain-on-failure'`): one spec file, a `checkout` describe, one test
that passes and one that fails inside a `test.step` with all four
attachments. The capture directory was replaced by `/capture`; the tests put
their own temp dir there. Re-capture against a new Playwright rather than
editing one by hand.
