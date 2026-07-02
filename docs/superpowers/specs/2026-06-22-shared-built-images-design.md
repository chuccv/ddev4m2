# Design: Shared (content-addressed) per-build Docker images

Date: 2026-06-22
Status: Approved design — pending spec review
Fork: `chuccv/ddev4m2` (fork of `ddev/ddev`)

## Problem

Upstream DDEV builds a derived image **per project**:

- web: `${DDEV_WEBIMAGE}-${DDEV_SITENAME}-built`
- db:  `${DDEV_DBIMAGE}-${DDEV_SITENAME}-built`

The tag is keyed on the **project name** (`DDEV_SITENAME`), not on the image
*content*. So N projects with identical build inputs produce N separate tagged
images. Observed symptoms (all confirmed by the user):

1. Many `...-<project>-built` images in `docker images`.
2. `docker system df` grows with project count (real layer/image bytes, not just tags).
3. Dangling `<none>` images accumulate after base-image updates.
4. Every project rebuilds even when its config is identical to another project's.

## Goal

Make the derived image tag **content-addressed**: keyed on a hash of the build
inputs instead of the project name. Projects with identical build inputs then
share one image and build it once. Disk drops from "one image per project" to
"one image per distinct build configuration".

## Hard Constraint — No data loss

The user runs DDEV on many real projects and **must not lose databases,
container data, or volumes** when switching to the custom binary.

This is safe by construction: in DDEV, **volumes and images are independent**.
DB/data volumes are named from the project / compose-project name
(`ddev-<project>-mariadb`, etc.), never from the image tag. This change touches
**only image tags and the references to them**. It must NOT change:

- `app.GetComposeProjectName()`
- `COMPOSE_PROJECT_NAME`
- any volume name or mount

These are explicitly out of scope and must remain byte-for-byte identical.

## Approach (chosen: A — content-addressed shared image)

Replace the `-<sitename>-built` suffix with `-<contentHash>-built`, where the
hash is **per service** so a web config shared between two projects still shares
the web image even if their DB versions differ.

- web hash  = `HashDirs(.webimageBuild, WebImage)`
- db hash   = `HashDirs(.dbimageBuild,  GetDBImage())`

The build-context dirs reuse the ingredients of the existing
`buildContextFingerprint()`
([ddevapp.go:1530-1541](../../../pkg/ddevapp/ddevapp.go#L1530-L1541)), which
already proves these inputs determine the built image. The only build-time input
not captured in the build-context dir is `uid`/`gid`, which is constant on a
single host, so there is no real-world collision risk for local dev. (Noted as a
known limitation, not a blocker.)

**Stability decision (important):** the base image *tag name* is hashed, NOT its
content ID (`dockerutil.ImageID`). `ImageID` returns the tag string before the
base image is pulled and the sha256 digest after — so using it makes the hash
(and therefore the image tag) change between the pre-pull and post-pull compose
renders during a single `Start()`. That instability caused two renders to
produce different built-image tags and an attempt to pull a not-yet-built tag
("manifest unknown"). Hashing the stable tag name fixes this. Detecting a changed
base-image *content* under the same tag is already handled separately by
`buildContextFingerprint()` / `.build-hash`, which triggers a rebuild that
overwrites this same shared tag — so correctness is preserved.

### Single source of truth

Add helper methods on `DdevApp` and route every existing construction/parse
through them:

```go
// builtSuffix returns "-<hash>-built" for a service ("web" or "db").
func (app *DdevApp) builtImageSuffix(service string) string

func (app *DdevApp) WebBuiltImage() string // app.WebImage      + builtImageSuffix("web")
func (app *DdevApp) DBBuiltImage() string  // app.GetDBImage()  + builtImageSuffix("db")

// stripBuiltSuffix removes a trailing "-<token>-built" from an image name,
// recovering the base image name regardless of what <token> is.
func stripBuiltSuffix(image string) string
```

## Touch points (exhaustive)

Construction sites — replace `app.WebImage + "-" + app.Name + "-built"` /
`app.GetDBImage() + "-" + app.Name + "-built"` with the helpers:

- [ddevapp.go:1863-1864](../../../pkg/ddevapp/ddevapp.go#L1863-L1864) — reuse-check existence test
- [ddevapp.go:1904](../../../pkg/ddevapp/ddevapp.go#L1904) — `log-stderr.sh` run on the built web image
- [ddevapp.go:3531-3534](../../../pkg/ddevapp/ddevapp.go#L3531-L3534) — `RemoveImage` on delete

Parse sites — replace `strings.TrimSuffix(img, fmt.Sprintf("-%s-built", app.Name))`
with `stripBuiltSuffix(img)`:

- [ddevapp.go:339](../../../pkg/ddevapp/ddevapp.go#L339) — describe (running container)
- [ddevapp.go:476-477](../../../pkg/ddevapp/ddevapp.go#L476-L477) — describe (compose service)

Pull site (discovered during implementation) — `FindServiceImages()` strips the
built suffix to recover the base image so DDEV pulls the base, never the
local-only built image. The old code stripped `-built` then `-<sitename>`; with
hash tags that second strip no longer matches, leaking a non-existent
`<image>-<hash>` tag into the pull list ("manifest unknown" warning). Fixed to
use `stripBuiltSuffix()` for web/db, falling back to the `-<sitename>` strip for
add-on services (opensearch, etc.) that still use the per-project scheme:

- [ddevapp.go:2395-2400](../../../pkg/ddevapp/ddevapp.go#L2395-L2400) — `FindServiceImages`

Compose template — render the built tag from new env vars instead of
`${DDEV_SITENAME}`, and add `pull_policy: build` to web/db:

- [app_compose_template.yaml](../../../pkg/ddevapp/app_compose_template.yaml) db — `image: ${DDEV_DBIMAGE_BUILT}` + `pull_policy: build`
- [app_compose_template.yaml](../../../pkg/ddevapp/app_compose_template.yaml) web — `image: ${DDEV_WEBIMAGE_BUILT}` + `pull_policy: build`

`pull_policy: build` is required because DDEV runs compose `up` with build enabled
([ddevapp.go:1965-1968](../../../pkg/ddevapp/ddevapp.go#L1965-L1968)); without it,
compose attempts to PULL the content-addressed `-built` tag (which exists only
locally, never in a registry) and prints a harmless but confusing
`✘ ... Error manifest unknown`. `pull_policy: build` tells compose to build the
image and never pull it. It does NOT force a rebuild — `up` reuses the existing
image when the build cache is unchanged.

Env injection — add `DDEV_WEBIMAGE_BUILT` and `DDEV_DBIMAGE_BUILT` to the
compose env map at
[ddevapp.go:2960-2969](../../../pkg/ddevapp/ddevapp.go#L2960-L2969), set to
`app.WebBuiltImage()` / `app.DBBuiltImage()`.

No change needed (verified compatible):

- [delete-images.go:192-198](../../../cmd/ddev/cmd/delete-images.go#L192-L198) —
  matches on prefix `webImage` + suffix `-built`; still matches hash tags.
- [ddevapp.go:2385](../../../pkg/ddevapp/ddevapp.go#L2385) — strips only `-built`.
- SSH-agent image (`ssh_auth.go`, `auth-ssh.go`) uses a separate, non-project
  `-built` scheme — untouched.

## Data flow / ordering

The hash needs the rendered build-context dirs (`.webimageBuild`,
`.dbimageBuild`) and the pulled base-image IDs. Required order during `Start()`:

1. `WriteBuildDockerfile()` writes `.webimageBuild/Dockerfile` etc. (already happens at render time).
2. `PullBaseContainerImages()` ensures base image IDs exist
   ([ddevapp.go:1610](../../../pkg/ddevapp/ddevapp.go#L1610)).
3. Compute env map (`DDEV_*_BUILT`) — must run **after** 1 & 2.
4. Render compose YAML using the `_BUILT` env vars.
5. Reuse-check + `composeBuild()`.

The env map is built where the compose YAML is rendered; confirm during
implementation that this render runs after the Dockerfile is written and base
images are available. If not, compute the hash lazily in the helper (it reads
the dir + image ID on demand) so call order is irrelevant.

## Migration & cleanup (no data loss)

On first `ddev start` with the custom binary against an existing project:

- The new code looks for `...-<hash>-built` (absent) → builds it once. **Volumes
  and DB are untouched** — only a new image tag is produced.
- The old `...-<sitename>-built` image becomes orphaned but stays tagged (not
  dangling), so disk briefly goes up until cleaned.

Cleanup: when a build succeeds, also remove the **legacy** per-project tag for
THIS project if it exists (`app.WebImage + "-" + app.Name + "-built"` and the db
equivalent) right after the existing dangling-image sweep at
[ddevapp.go:1927-1934](../../../pkg/ddevapp/ddevapp.go#L1927-L1934). This is safe
because we just rebuilt; removing an image tag never touches volumes.

Cross-project GC of a shared image no longer referenced by any project is **out
of scope** — rely on existing `ddev delete images` / `docker image prune`.

## Testing / verification

1. `make` builds clean; `.gotmp/bin/<platform>/ddev --version` runs.
2. `make staticrequired` passes.
3. Unit test for `stripBuiltSuffix` (table-driven: hash tag, legacy sitename tag,
   plain base image, ssh-agent style) — prefer `require`.
4. Unit test that `WebBuiltImage()` / `DBBuiltImage()` are stable for identical
   inputs and differ when build context differs (use a temp `.webimageBuild`).
5. Manual (Docker available), the critical data-safety check:
   - Pick an existing real project with a DB. `ddev describe` before.
   - `ddev start` with the custom binary.
   - Verify DB data still present (`ddev mysql`/site loads); volume name unchanged
     (`docker volume ls`).
   - Create a 2nd project with identical config → confirm it reuses the same
     `-<hash>-built` web image (no second build, one tag in `docker images`).
   - `ddev describe` shows the clean base image name (parse sites still work).

## Out of scope

- Changing volume / compose-project naming.
- Removing the per-project build feature (that was rejected Approach B).
- Cross-project automatic image garbage collection.
- Sharing images across different hosts / uid-gid.
