# Security Policy

## Supported versions

The latest release on the `main` branch is the only supported version. Patch
releases are cut as needed for security fixes.

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Instead, report
vulnerabilities privately through GitHub's
[private vulnerability reporting](https://github.com/agmonetti/picklock/security/advisories)
for this repository.

When reporting, include:

- The affected version(s).
- A description of the vulnerability and how to reproduce it.
- Impact and any suggested fix, if known.

Reports are acknowledged and triaged as soon as possible. You will receive a
response with the next steps, and details are kept confidential until a fix is
released.

## Security-relevant notes for picklock

`picklock` is a local, single-user terminal tool. Relevant security properties and
accepted tradeoffs are documented in the design documents:

- `docs/design/06-security.md` — threat model and decisions for maintainers.
- `docs/design/07-user-security.md` — quick guide for end users (saved
  passwords, TLS, destructive queries).
