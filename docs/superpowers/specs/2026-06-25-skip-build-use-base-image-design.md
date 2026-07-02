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

## Important finding: the web build is never a structural no-op

`WriteBuildDockerfile()` for **web** always receives extra content that injects
steps regardless of user customization (`config.go:1082-1084`):

```
RUN log-stderr.sh mariadb-compat-install.sh || true
RUN log-stderr.sh mariadb-skip-ssl-wrapper-install.sh || true
```

plus `composer self-update` and permission fixes. So the web `-built` image
always adds the mariadb-compat wrappers etc. on top of the base. Using the base
directly for web is therefore only correct when the **base image already bakes
those in**. This cannot be detected statically, so web requires an explicit
marker (below). For **db with MariaDB**, by contrast, no extra content is
injected (`extraDBContent` is Postgres-only), so a clean MariaDB db build is
just the `useradd` block — a genuine no-op when uid/gid match, with no marker
needed.

## Detection Criteria

A new helper decides, **per service (web and db independently)**, whether the
base image can be used directly. It returns true only when **all** hold:

- **(web only) the base image carries the `com.ddev.prebaked="true"` label.**
  This is how a prebaked base declares it already includes the mariadb-compat
  wrappers, composer, and permission setup the web build would otherwise add.
  Db does not need this marker.

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

## Mechanism — tag the base as the built image (no build)

The chosen mechanism keeps `WebBuiltImage()`/`DBBuiltImage()` returning the
content-addressed `-built` tag unchanged, and instead **tags the base image with
that `-built` tag** when the build can be skipped. This is a zero-disk alias, so
every downstream consumer (compose `image:`, the existence check, cleanup) works
with no special-casing.

1. **`webUsesBaseImageDirectly()` / `dbUsesBaseImageDirectly()`** in
   `pkg/ddevapp/built_image.go`:
   - `serviceBuildAddsNothing(buildSubdir, extraPackages, isWeb)` — pure,
     Docker-independent customization check (no user build files, no
     `extra_packages`, web PHP preinstalled, db not mysql-8.x / not Postgres).
   - web additionally requires `baseImageIsPrebaked()` (the `com.ddev.prebaked`
     label).
   - both require `baseImageMatchesHostUser()` — the base image's user uid/gid
     (from `docker inspect` `Config.User`, resolving a username via one cached
     `id` run) equals `GetContainerUser()`.
   - On any error or uncertainty the helpers return false → normal build.
2. **Build step** (`ddevapp.go`, inside `if buildNeeded`): for each service that
   can use the base directly, `dockerutil.TagImage(base, builtTag)` instead of
   building; build only the remaining services via `composeBuild(services...)`.
   If both web and db use the base, no build runs at all.
3. **No change** to `WebBuiltImage()`/`DBBuiltImage()`, the env vars
   (`DDEV_WEBIMAGE_BUILT`/`DDEV_DBIMAGE_BUILT`), or `app_compose_template.yaml`.
   `pull_policy: build` is safe because the `-built` tag exists locally (as the
   alias), so `up` neither pulls nor rebuilds.
4. **Cleanup** (`ddevapp.go`) is unchanged and safe: removing the `-built` alias
   tag only untags it; the base keeps its own tag, and volumes/data are never
   touched.
5. **New dockerutil helpers**: `ImageConfigUser`, `ImageConfigLabel`, `TagImage`.

## Required action on the base image (web only)

For web skipping to activate, the prebaked web base image must set:

```dockerfile
LABEL com.ddev.prebaked="true"
```

It declares the base already includes the mariadb-compat wrappers, composer, and
permission setup. Without the label, web always builds as before (safe default).
Db (MariaDB) needs no label.

## Out of Scope (unchanged)

- Volumes, the compose project name, and the `.build-hash` /
  `buildContextFingerprint()` share/rebuild logic for services that **do** have
  customizations.
- Router, ssh-agent, xhgui, and all add-on service containers.

## Testing

- **Unit tests** (`pkg/ddevapp/built_image_test.go`, Docker-independent):
  - `TestServiceBuildAddsNothing` — false when any customization input is
    present (a `web-build/Dockerfile`, non-empty `extra_packages`, a
    non-preinstalled PHP version, mysql-8.x / Postgres db); true for a clean
    web/MariaDB-db project.
  - `TestHasUserBuildFiles` — only user-provided files count (example files and
    the managed `README.txt` do not; a subdirectory does).
  - `TestBaseImageUIDGIDNumeric` / `TestIsAllDigits` — numeric uid/gid parsing.
  - The Docker-dependent helpers (`baseImageMatchesHostUser`,
    `baseImageIsPrebaked`) are covered by integration only — the ddevapp test
    package's `TestMain` requires the full Docker + ddev harness, so these unit
    tests are run there, not in a standalone container.
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
