---
name: system-update-release
description: Use when choosing a release version or drafting GitHub release notes shown in the system update dialog.
---

# System Update Releases

Use this skill when preparing a GitHub release tag or its release notes. The system update dialog displays the GitHub release tag and body. Production image releases that use commit identifiers follow the repository's separate release workflow.

## Versioning

- Check the latest published stable release and existing release tags before proposing a version. Do not infer the release baseline from the checked-out commit or local package versions.
- Unless the user explicitly specifies a version or release level, use the next stable version in `vMAJOR.MINOR.PATCH` form. Keep the major version unchanged.
- Treat `vX.Y.0` as the baseline for a minor series. Default progression is `v1.0.0`, `v1.0.1`, ..., `v1.0.9`, then `v1.1.0`. In general, increment the patch through `.9`; after `.9`, increment the minor and reset the patch to `.0`.
- Pre-release tags such as `-alpha`, `-beta`, and `-rc.N` do not count toward the stable release cadence. When explicitly promoting a pre-release to stable, remove its pre-release suffix while keeping the same `X.Y.Z` version.
- Before creating a tag, check that it does not already exist. If release history is unavailable or ambiguous, report the candidate and the uncertainty instead of guessing.

## Release Notes

- Keep the notes brief and user-facing. Use `## 中文` and `## English`, with one to three short bullets per language; state the main change or benefit without listing implementation details.
- Keep both languages equivalent. Do not paste commit lists, include empty categories, or describe internal refactors users will not notice.
- Include a short `[!IMPORTANT]` alert only for an urgent security issue or required upgrade action. Keep critical compatibility facts accurate in both languages.

## Release Operations

Follow the repository's `AGENTS.md` and `TEAM_WORKFLOW.md` requirements for release operations. This skill guides version and release-note choices; it does not authorize pushing tags, publishing releases, or deploying.
