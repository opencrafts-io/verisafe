---
title: "Security: leaked Apple private key — rotated and purged"
labels: [security, critical, tech-debt, resolved]
---

## Status
- [x] Removed from tracked files on `fix/remove-leaked-apple-key`
- [x] Purged from git history on all branches (`git filter-repo` + coordinated force-push to `origin`, completed 2026-07-11)
- [x] Key rotated: old key revoked in the Apple Developer portal, replacement deployed to `.env` (2026-07-11)
- [ ] All maintainers' local clones resynced (checklist below)

## What happened
`keys/AuthKey_ZJ3TXALZ66.p8` — an OpenSSH-format private key named in Apple's `AuthKey_<KEYID>.p8` convention (Sign in with Apple / APNs) — was committed to the repository and remained readable by anyone with clone access until caught during a tech-debt review.

Introduced in commit `c90f158`, authored by @IamMuuo, in a commit titled `fix: fixes a typo on logs publis to publish` — the key file was bundled in alongside an unrelated one-line logging fix (6 lines added), consistent with a `git add .` sweeping up a stray local file. `keys/` was later added to `.gitignore`, but ignore rules aren't retroactive — they only stop new commits from re-adding a path, so the key remained in history regardless.

Flagging for awareness: be deliberate with `git add .` around `keys/`, `.env`, or other local-config directories.

## Impact
Anyone with read access to the repository, or any clone/fork taken before the purge, had the key material. It was live in `.env` as `APPLE_PRIVATE_KEY_BASE64` for runtime use (`internal/config/config.go` decodes it, consumed in `internal/auth/auth.go`; the `.p8` file itself was never read by the app — leftover from setup). Removing it from git does not undo the exposure; only revocation does.

## Resolution
1. Untracked the file, confirmed `.gitignore` covers `keys/` (`90e47d6` on `fix/remove-leaked-apple-key`, rebuilt as `72b5043` after the rewrite).
2. History purge: `git filter-repo --path keys/AuthKey_ZJ3TXALZ66.p8 --invert-paths` against a mirror clone, force-pushed to every branch on `origin`. Verified: `git rev-list --objects --remotes=origin | grep -i AuthKey_ZJ3TXALZ66` returns nothing on any origin ref.
3. Old key revoked in the Apple Developer portal; replacement key generated and deployed to the relevant `.env`.

## Outstanding

### Maintainers must resync local clones
History was rewritten — every commit SHA on every branch changed. A plain `git pull` will not work cleanly and can resurrect the old history on next push.

Simplest option: re-clone fresh, if you have no unpushed local commits.

If you have unpushed local work, don't delete anything:
```bash
git fetch origin --prune
# pure mirror branches (no local-only commits):
git branch -f <branch-name> origin/<branch-name>

# branches with unpushed local commits on top of the old history:
git rebase --onto origin/<branch-name> <old-branch-base-sha> <branch-name>
```
Check for unpushed work before resetting:
```bash
git log origin/<branch-name>..<branch-name> --oneline
```
Empty output: safe to fast-forward with `git branch -f`. Non-empty: rebase instead, so the work isn't orphaned.

- [ ] @IamMuuo
- [ ] @eiidoubleyuwes (Baraka Mnjala Mbugua)
- [ ] @Eugene600 (Eugene Wachira)

### Prevent recurrence
Add secrets scanning (gitleaks or trufflehog) as a pre-commit hook and/or CI step. There's a stale, unmerged `feature/gitleaks` branch with a `gitleaks-scan.yml` workflow from a prior attempt — revive rather than start over.

## Priority
Critical exposure is resolved. Remaining items (clone resync, scanning) are follow-through, not open risk.
