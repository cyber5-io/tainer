# html/ is app/ : Enhance and WordPress checklist (2026-11-09)

Manual steps for Leni. The code side of the rename is done in every repo; these are the things only you can switch.

## 1. Release order

1. Push `kompozi` main first (the kompozi image clones it).
2. Push `tainer-images` main. CI builds and publishes on push, so do this only on release day. Wait for that tainer-images CI run to finish before step 3.
3. Tag and push tainer-control from main: `git tag v1.0.2 && git push origin main v1.0.2`. `publish-image.yml` builds on `v*` tags, on top of `tainer-control-app:node-22` from tainer-images, so the images must be published first. The image carries min-tainer 1.5.0, so it needs the new tainer.
4. Install the new tainer pkg.
5. Run `tainer update --all`.

The tainer-control, comms, ops, website and oneputtpro main pushes (the rename commits themselves, not the v1.0.2 tag) are safe at any time.

## 2. tainer-id on Enhance

No `live` tag exists yet and the rename commit is untagged, so do it in this order.

1. Tag the rename release and push it: `git tag v1.1.4 && git push origin main v1.1.4`.
2. In one coordinated step on Enhance:
   - Start command: `node --env-file=../../tainer-id.env app/build`
   - Docroot: `deploys/current/app/build/client`
   - Build script (from tainer-id README.md, "Build script"):
     ```sh
     set -e
     cd app
     npm ci --include=dev
     npm run build
     set -a; . "$HOME/tainer-id.env"; set +a
     npx drizzle-kit migrate
     ```
   - Repository ref: switch from the `live` branch to the `live` tag.
3. `scripts/promote v1.1.4` (creates and pushes the `live` tag; Enhance deploys it).
4. After that first tag deploy succeeds, delete the origin `live` branch: `git push origin --delete live`.

Until step 4 deletes the branch, the ref `live` exists twice on origin (branch and tag). Before deleting, check that Enhance deployed the tag at step 3, not the branch.

## 3. c5test on Enhance

- Build: `cd app && npm ci`
- Start: `node app/server.js`
- Change these paths before its next promote.
- The ref is already the `live` tag. `scripts/promote` was copied in from tainer-id.

## 4. comms server cron

The cron line `cd /path/to/comms/html && npm run email:reconcile ...` becomes `cd /path/to/comms/app && ...`. The log directory moves with it (`/path/to/comms/app/logs`). `docs/DEPLOY.md` has the new text. Also move the production `.env` to `app/.env`.

## 5. ops production deploy paths

Review the Enhance paths against `docs/DEPLOY.md` in the ops repo (Node version file, `.env`, `cd app`) at the next ops deploy.

## 6. WordPress, after each pod migrates (oneputtpro, mywp, opp)

1. `tainer exec <pod> app -- wp search-replace '/var/www/html' '/var/www/app'`
2. Check `wp-config.php` for absolute-path constants that still say `/var/www/html`.

## 7. One-time effects after installing the pkg

- Every pod recreates once.
- Projects auto-rename `html/` to `app/` at their next start.
- The control pod pulls 1.0.2.
- Stray `html/` cache folders (such as `html/.vite`, recreated by a pod still running from before the rename) are ignored by start with a notice and are safe to delete at any time.
