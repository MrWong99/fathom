# S14: practitioner survey

## Goal

Order the check backlog, confirm or refute OpenShift-first, decide GitLab
adapter timing and the `plain` reader priority, and measure whether a form has
a residual over the editor path, from the people who deploy the company's
software: consultants, supporters, developers and platform staff.

## Pass criterion (tracker)

Orders the backlog; confirms OpenShift-first; decides GitLab timing and the
form's residual

## Method

- `SURVEY.md` is the questionnaire (Parts 1 to 4), the hands-on protocol for
  the form-versus-editor arm (Part 5), and a sender annex with the decision
  rules fixed before any answer arrives.
- `tally/` computes those rules from a CSV export (`go run ./cmd/tally
  responses.csv`); `testdata/responses.csv` is a synthetic five-row example in
  the export shape, used only to test the tally. It is not data.
- The hands-on arm needs the S9 prototype and the kind fixture from the
  calibration spikes, so it runs in weeks 5 to 6.

## Status

- 2026-09-17: draft 1 of the survey written; tally implemented and tested.
  Waiting for the owner to send it. RESULT.md is written when the survey
  closes (two weeks after sending) and appended after the hands-on sessions.
