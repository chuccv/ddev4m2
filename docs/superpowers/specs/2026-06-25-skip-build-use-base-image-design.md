# Skip per-project build when the base image is enough

Date: 2026-06-25
Status: Design approved, pending implementation plan

## The Idea

This fork already shares the per-project "built" web/db images across projects
by tagging them with a content hash (`<base-image:tag>-<hash>-built`, see
`pkg/ddevapp/built_image.go`). This design goes one step further: when a
project adds **nothing** on top of the base image, skip the derived build
entirely and run the **base image directly** (it is already pulled by
`PullBaseContainerImages()`). No build, no extra tag, no extra disk.

## Why It Is Safe (the key insight)

The only thing in the generated build Dockerfile
(`WriteBuildDockerfile()`, `pkg/ddevapp/config.go:1198`) that is genuinely
**per-host** is baking the host `uid`/`gid` into the image — the
`groupadd`/`useradd` block and the many `chown "$uid:$gid"` calls. Everything
else (composer self-update, PHP setup, permission fixes) is identical for every
project on the same base + config, which is exactly why the fork can already
share those images.

Therefore the derived image is **functionally equivalent to the base** when:

1. There is no user customization, **and**
2. The host `uid`/`gid` already matches the user baked into the base image —
   in that case `useradd` collapses to `|| true` and every `chown "$uid:$gid"`
   chowns to the already-correct owner (a no-op).

When both hold, running the base image directly produces the same container as
running the built image.

### Accepted trade-off

The generated web Dockerfile always runs `composer self-update`. Skipping the
build means accepting the Composer already present in the base image. Modern
base images ship a recent Composer, so this is acceptable. If a project ever
needs "always latest Composer", that is a customization and would block the
skip (a future flag could force it).

## Detection Criteria

A new helper decides, **per service (web and db independently)**, whether the
base image can be used directly. It returns true only when **all** hold:

- **No user build files** in `.ddev/web-build` / `.ddev/db-build`:
  no `prepend.Dockerfile*`, `pre.Dockerfile*`, `post.Dockerfile*`, or
  `Dockerfile*` (ignoring `*.example`). This is how **add-ons that modify the
  image** inject steps, so this condition also covers them.
- **No `webimage_extra_packages` / `dbimage_extra_packages`** in
  `config.yaml` (`app.WebImageExtraPackages` / `app.DBImageExtraPackages`).
  This is the other, independent way a project adds apt packages
  (`config.go:1300-1305`), separate from add-ons.
- **(web only)** PHP version is preinstalled
  (`nodeps.PreinstalledPHPVersions`), so no `install_php_extensions.sh` /
  alternatives setup is injected.
- **(db only)** Not the bitnami-mysql `ENV HOME=""` special case
  (MySQL 8.0 / 8.4), and no Postgres-client injection.
- **`uid`/`gid` match:** `os.Getuid()` / `os.Getgid()` equal the user baked
  into the base image. The base image's user is read via
  `docker inspect` (`Config.User`); if it is a name rather than a number, it is
  resolved to numeric `uid`/`gid` by running `id` in the base image **once**,
  cached by base image ID.

> Service add-ons that only add **their own container** (redis, varnish,
> memcached, elasticsearch, …) do not write into `web-build`/`db-build`, so
> they never block the skip — and they keep working because they are separate
> containers with their own images, untouched by this change.

## Mechanism

1. **`canUseBaseImageDirectly(buildContextDir, baseImage)`** — new function in
   `pkg/ddevapp/built_image.go` implementing the criteria above.
2. **`WebBuiltImage()` / `DBBuiltImage()`** — when the corresponding service can
   use the base directly, return the **base** image
   (`app.WebImage` / `app.GetDBImage()`) instead of the `-built` tag. Because
   `DDEV_WEBIMAGE_BUILT` / `DDEV_DBIMAGE_BUILT` (`ddevapp.go:2981,2984`) are
   derived from these, the compose service `image:` automatically points at the
   base. **No change to `app_compose_template.yaml`.**
3. **Build step** (`ddevapp.go:1888-1925`): build only the services that are
   **not** skipped. Pass an explicit services list to `composeBuild()`. If both
   web and db are skipped, skip the build block entirely. `docker compose up`
   does not build images that already exist, and the base images are present
   via `PullBaseContainerImages()`.
4. **Image-existence check** (`ddevapp.go:1861-1873`): when a service uses the
   base directly, "built image exists locally" must check the **base** image,
   which `WebBuiltImage()`/`DBBuiltImage()` now return — so this keeps working
   with no special-casing.
5. **Legacy/`-built` tag cleanup** (`ddevapp.go:1939-1944`) is unchanged.
   Removing a tag never touches volumes or data.

## Out of Scope (unchanged)

- Volumes, the compose project name, and the `.build-hash` /
  `buildContextFingerprint()` share/rebuild logic for services that **do** have
  customizations.
- Router, ssh-agent, xhgui, and all add-on service containers.

## Testing

- **Unit tests** (`pkg/ddevapp/built_image_test.go`):
  - `canUseBaseImageDirectly` returns false when any single condition fails
    (a `web-build/Dockerfile`, a non-empty `extra_packages`, a non-preinstalled
    PHP version, a mismatched `uid`/`gid`).
  - `canUseBaseImageDirectly` returns true for a clean project with matching
    `uid`/`gid`.
  - `WebBuiltImage()`/`DBBuiltImage()` return the base image when skippable and
    the `-built` content-addressed tag otherwise.
- **Integration** (Docker available): a clean project starts with no
  `*-built` image created and the web/db services running the base image; a
  project with `webimage_extra_packages` still builds a `-built` image and
  installs the package; a project with a redis add-on starts redis correctly in
  both cases.

## Validation

```bash
make
go test -v -run TestCanUseBaseImageDirectly ./pkg/ddevapp
make staticrequired
```
