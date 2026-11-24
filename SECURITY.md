# Security policy

## Supported versions

Only the latest release gets security fixes.

## Reporting a vulnerability

Please do not open a public issue. Report it privately through
[GitHub security advisories](https://github.com/huntly-digital/plandillo/security/advisories/new).

Include what you found, how to reproduce it and which version you tested. Expect a first reply within a week.

## Scope

Plandillo is built to run on your own machine or behind your own HTTPS reverse proxy. Of particular interest: bypassing
the Host/Origin guard, reaching `/mcp` or another user's data without the right token or session, and anything that
leaks a token or password into logs or responses.
