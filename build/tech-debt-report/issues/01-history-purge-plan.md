# History purge plan for keys/AuthKey_ZJ3TXALZ66.p8

Status: **drafted, not executed.** This is the higher-risk half of issue 01 — it rewrites every
commit SHA in the repository and requires a force-push to every remote branch. Do not run this
until:

1. The key has been rotated/revoked in the Apple Developer portal (tree-removal alone, done on
   `fix/remove-leaked-apple-key`, does not un-leak a key that may already be cloned elsewhere).
2. Everyone with a local clone is ready to re-clone or hard-reset afterward (`git pull` will not
   cleanly reconcile a rewritten history).
3. You've picked a maintenance window — this touches `main`, `staging`, and every open branch/PR.

## Why tree-removal isn't enough
`git rm --cached` (done in commit `90e47d6` on `fix/remove-leaked-apple-key`) stops the file from
being tracked *going forward*, but the blob is still reachable from history at commit `c90f158`
(and every commit since). Anyone can still `git checkout c90f158 -- keys/AuthKey_ZJ3TXALZ66.p8`
and get the key back. Only a history rewrite removes it from the repository entirely.

## Steps

### 1. Install git-filter-repo
Not currently installed in this environment.
```bash
# Arch:
sudo pacman -S git-filter-repo
# or via pip:
pip install --user git-filter-repo
```

### 2. Make a fresh mirror clone (never run filter-repo on your normal working copy)
```bash
git clone --mirror git@github.com:opencrafts-io/verisafe.git verisafe-purge.git
cd verisafe-purge.git
```

### 3. Strip the file from all history
```bash
git filter-repo --path keys/AuthKey_ZJ3TXALZ66.p8 --invert-paths
```
This rewrites every commit that ever touched the path, across every branch and tag in the mirror.

### 4. Verify it's gone
```bash
git log --all --oneline -- keys/AuthKey_ZJ3TXALZ66.p8   # should print nothing
git rev-list --objects --all | grep -i AuthKey_ZJ3TXALZ66 # should print nothing
```

### 5. Force-push the rewritten history (the actual destructive step)
```bash
git push --force --all origin
git push --force --tags origin
```
**Do not run this step without a final explicit go-ahead in the moment** — it overwrites `main`,
`staging`, and every other branch on the remote. Confirm with the team first; anyone with an
existing clone will need to `git fetch && git reset --hard origin/<branch>` (or re-clone) rather
than `git pull`, or they'll resurrect the old history on their next push.

### 6. Post-purge
- Re-open the mirror as a normal clone or point your working copy at the rewritten remote.
- Confirm branch protection rules / required reviews still apply after the force-push (some GitHub
  settings can be reset by a force-push to a protected branch).
- Let any external collaborators/forks know the history changed, if applicable.

## Not in scope here
Actually rotating the key with Apple — that has to happen in the Apple Developer portal by
whoever owns that account. This plan only covers getting the leaked key out of the git repository.
