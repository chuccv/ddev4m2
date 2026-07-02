package ddevapp

import (
	"os"
	"strings"

	"github.com/ddev/ddev/pkg/dockerutil"
	"github.com/ddev/ddev/pkg/nodeps"
	"github.com/ddev/ddev/pkg/util"
)

// Shared "built" images.
//
// Upstream DDEV tags derived web/db images per project (`<image>-<sitename>-built`),
// so N projects produce N separate images. This fork tags them by just the base
// image name (`<image>-built`), so all projects sharing the same base image use
// one derived image. The last project to build wins; since all projects on the
// same host share the same uid/gid and typically the same web-build, the result
// is identical anyway. Volumes and compose project names are unaffected.

// WebBuiltImage returns the tag for the project's built web image.
// All projects using the same base image share this tag.
func (app *DdevApp) WebBuiltImage() string {
	return app.WebImage + "-built"
}

// DBBuiltImage returns the tag for the project's built db image.
// All projects using the same base image share this tag.
func (app *DdevApp) DBBuiltImage() string {
	return app.GetDBImage() + "-built"
}

// legacyWebBuiltImage returns the upstream per-project web image tag.
// Kept only so an old tag can be cleaned up after migrating to shared images.
func (app *DdevApp) legacyWebBuiltImage() string {
	return app.WebImage + "-" + app.Name + "-built"
}

// legacyDBBuiltImage returns the upstream per-project db image tag.
func (app *DdevApp) legacyDBBuiltImage() string {
	return app.GetDBImage() + "-" + app.Name + "-built"
}

// RemoveOldBuiltImageTags removes any local tags matching `<baseImage>-*-built`
// (old content-hash and per-project sitename formats) that differ from the
// current shared tag (`<baseImage>-built`). Safe to call on every start.
func (app *DdevApp) RemoveOldBuiltImageTags() {
	for _, base := range []string{app.WebImage, app.GetDBImage()} {
		if base == "" {
			continue
		}
		current := base + "-built"
		// Docker reference filter: matches any tag of this image that ends in -built
		imgs, err := dockerutil.FindImagesByReference(base + "-*-built")
		if err != nil {
			util.Warning("unable to list old built images for %s: %v", base, err)
			continue
		}
		for _, img := range imgs {
			for _, tag := range img.RepoTags {
				if tag != current {
					util.Debug("Removing old built image tag %s", tag)
					_ = dockerutil.RemoveImage(tag)
				}
			}
		}
	}
}

// Using the base image directly.
//
// Even when a project adds nothing of its own, WriteBuildDockerfile() still
// injects a useradd/chown block that bakes the host uid/gid into the image, plus
// some always-present web/db setup. The only genuinely per-host part is the
// uid/gid: every other injected step is identical for all projects on the same
// base + config. So when (a) there is no user customization and (b) the host
// uid/gid already match the user baked into the base image, the whole build is a
// no-op and the base image can be used directly (tagged as the built image)
// instead of running a real build.

// prebakedImageLabel marks a web base image that already bakes in everything the
// per-project web build would otherwise add (mariadb-compat wrappers, composer,
// permission fixes, etc.). Only such a base is safe to use directly for web.
// Set it on the base image with: LABEL com.ddev.prebaked="true"
const prebakedImageLabel = "com.ddev.prebaked"

// webUsesBaseImageDirectly reports whether the web base image can be used
// directly without a per-project build. Unlike db, the web build always injects
// extra steps (mariadb-compat, composer self-update, permission fixes), so the
// base is only safe to use directly when it is explicitly marked as prebaked.
func (app *DdevApp) webUsesBaseImageDirectly() bool {
	return app.serviceBuildAddsNothing("web-build", app.WebImageExtraPackages, true) &&
		baseImageIsPrebaked(app.WebImage) &&
		baseImageMatchesHostUser(app.WebImage)
}

// baseImageIsPrebaked reports whether baseImage carries the prebaked marker
// label. Any error reading the label returns false (do not skip).
func baseImageIsPrebaked(baseImage string) bool {
	v, err := dockerutil.ImageConfigLabel(baseImage, prebakedImageLabel)
	if err != nil {
		return false
	}
	return v == "true"
}

// dbUsesBaseImageDirectly reports whether the db base image can be used directly
// without a per-project build.
func (app *DdevApp) dbUsesBaseImageDirectly() bool {
	return app.serviceBuildAddsNothing("db-build", app.DBImageExtraPackages, false) &&
		baseImageMatchesHostUser(app.GetDBImage())
}

// serviceBuildAddsNothing reports whether the generated build Dockerfile would
// add nothing beyond the base image except the (potentially no-op) host
// uid/gid setup. It checks only customization inputs and is Docker-independent.
func (app *DdevApp) serviceBuildAddsNothing(buildSubdir string, extraPackages []string, isWeb bool) bool {
	// Extra apt packages are installed by the build.
	if len(extraPackages) > 0 {
		return false
	}
	// Any user-provided build file (including files dropped by add-ons into
	// web-build/db-build) means the build adds steps on top of the base.
	if hasUserBuildFiles(app.GetConfigPath(buildSubdir)) {
		return false
	}
	if isWeb {
		// A non-preinstalled PHP version triggers an install step in the build.
		if _, ok := nodeps.PreinstalledPHPVersions[app.PHPVersion]; !ok {
			return false
		}
	} else {
		// bitnami/mysql 8.0/8.4 inject an "ENV HOME" step.
		if app.Database.Type == nodeps.MySQL && (app.Database.Version == nodeps.MySQL80 || app.Database.Version == nodeps.MySQL84) {
			return false
		}
		// Postgres injects a postgresql-client setup step.
		if app.Database.Type == nodeps.Postgres {
			return false
		}
	}
	return true
}

// hasUserBuildFiles reports whether the web-build/db-build directory contains any
// user-provided file. A clean directory holds only *.example files and the
// DDEV-managed README.txt; anything else (a Dockerfile, a context file, or a
// subdirectory) is treated as a customization.
func hasUserBuildFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
		name := e.Name()
		if strings.HasSuffix(name, ".example") || name == "README.txt" {
			continue
		}
		return true
	}
	return false
}

// baseImageMatchesHostUser reports whether the user baked into baseImage has the
// same uid/gid DDEV uses for the container (GetContainerUser). When they match,
// the build's useradd and `chown "$uid:$gid"` steps are no-ops. Any error or
// inability to determine the base image's user returns false (do not skip),
// preserving the normal per-project build.
func baseImageMatchesHostUser(baseImage string) bool {
	configUser, err := dockerutil.ImageConfigUser(baseImage)
	if err != nil || configUser == "" {
		return false
	}
	uid, gid, ok := baseImageUIDGID(baseImage, configUser)
	if !ok {
		return false
	}
	wantUID, wantGID, _ := dockerutil.GetContainerUser()
	return uid == wantUID && gid == wantGID
}

// baseImageUIDGID resolves the numeric uid/gid of configUser inside baseImage.
// A numeric "uid:gid" form is parsed directly; anything else (a username, or a
// form needing a primary group lookup) is resolved by running "id" in the base
// image as that user.
func baseImageUIDGID(baseImage string, configUser string) (uid string, gid string, ok bool) {
	parts := strings.SplitN(configUser, ":", 2)
	if len(parts) == 2 && isAllDigits(parts[0]) && isAllDigits(parts[1]) {
		return parts[0], parts[1], true
	}
	_, out, err := dockerutil.RunSimpleContainer(baseImage, "ddev-baseuser-"+util.RandString(6),
		[]string{"sh", "-c", "id -u; id -g"}, nil, nil, nil, configUser, true, false, nil, nil, nil)
	if err != nil {
		return "", "", false
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 || !isAllDigits(fields[0]) || !isAllDigits(fields[1]) {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// isAllDigits reports whether s is non-empty and contains only ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// EffectiveWebBuiltImage returns the image name the web container should
// actually run. When the base image is usable directly (no customisation,
// prebaked, host uid/gid match), it returns the base image — no derived tag
// is created. Otherwise it returns the content-addressed built image tag.
func (app *DdevApp) EffectiveWebBuiltImage() string {
	if app.webUsesBaseImageDirectly() {
		return app.WebImage
	}
	return app.WebBuiltImage()
}

// EffectiveDBBuiltImage returns the image name the db container should run.
func (app *DdevApp) EffectiveDBBuiltImage() string {
	if app.dbUsesBaseImageDirectly() {
		return app.GetDBImage()
	}
	return app.DBBuiltImage()
}

// stripBuiltSuffix recovers the base image name from a built image tag.
// It matches against the project's known web and db base images, so it works
// regardless of the suffix format (content hash or legacy sitename) and even
// when the base image name itself contains dashes. Unknown images (e.g. the
// ssh-agent image) are returned unchanged.
func (app *DdevApp) stripBuiltSuffix(image string) string {
	for _, base := range []string{app.WebImage, app.GetDBImage()} {
		if base != "" && strings.HasPrefix(image, base+"-") && strings.HasSuffix(image, "-built") {
			return base
		}
	}
	return image
}
