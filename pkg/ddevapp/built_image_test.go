package ddevapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ddev/ddev/pkg/nodeps"
	"github.com/stretchr/testify/require"
)

// TestStripBuiltSuffix verifies the base image name is recovered from both the
// new content-hash tag and the legacy per-project tag, while unrelated images
// (e.g. the ssh-agent image) are left untouched.
func TestStripBuiltSuffix(t *testing.T) {
	app := &DdevApp{
		WebImage: "ddev/ddev-webserver:v1.24.0",
		Name:     "my-project",
		Database: DatabaseDesc{Type: nodeps.MariaDB, Version: nodeps.MariaDB1011},
	}
	dbImage := app.GetDBImage()
	require.NotEmpty(t, dbImage)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"web content-hash tag", app.WebImage + "-1a2b3c4d5e6f-built", app.WebImage},
		{"web legacy sitename tag (with dash)", app.WebImage + "-" + app.Name + "-built", app.WebImage},
		{"db content-hash tag", dbImage + "-deadbeefcafe-built", dbImage},
		{"plain web image untouched", app.WebImage, app.WebImage},
		{"unknown -built image untouched", "ddev/ddev-ssh-agent:v1.24.0-built", "ddev/ddev-ssh-agent:v1.24.0-built"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, app.stripBuiltSuffix(tc.input))
		})
	}
}

// preinstalledPHP returns any PHP version known to be preinstalled in the base
// image, for use in tests that need serviceBuildAddsNothing to pass the PHP check.
func preinstalledPHP(t *testing.T) string {
	for v := range nodeps.PreinstalledPHPVersions {
		return v
	}
	t.Fatal("no preinstalled PHP versions available")
	return ""
}

// writeBuildFile creates .ddev/<subdir>/<name> under the app root with content.
func writeBuildFile(t *testing.T, app *DdevApp, subdir, name string) {
	dir := app.GetConfigPath(subdir)
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("# test\n"), 0644))
}

// TestServiceBuildAddsNothing verifies the Docker-independent half of the
// "use base image directly" decision: any customization input must force a
// build, while a clean project must not.
func TestServiceBuildAddsNothing(t *testing.T) {
	php := preinstalledPHP(t)

	t.Run("clean web returns true", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), PHPVersion: php}
		require.True(t, app.serviceBuildAddsNothing("web-build", nil, true))
	})

	t.Run("web extra packages returns false", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), PHPVersion: php}
		require.False(t, app.serviceBuildAddsNothing("web-build", []string{"vim"}, true))
	})

	t.Run("web user Dockerfile returns false", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), PHPVersion: php}
		writeBuildFile(t, app, "web-build", "Dockerfile")
		require.False(t, app.serviceBuildAddsNothing("web-build", nil, true))
	})

	t.Run("web example file only returns true", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), PHPVersion: php}
		writeBuildFile(t, app, "web-build", "Dockerfile.example")
		require.True(t, app.serviceBuildAddsNothing("web-build", nil, true))
	})

	t.Run("web non-preinstalled PHP returns false", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), PHPVersion: "0.1"}
		require.False(t, app.serviceBuildAddsNothing("web-build", nil, true))
	})

	t.Run("clean mariadb db returns true", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), Database: DatabaseDesc{Type: nodeps.MariaDB, Version: nodeps.MariaDBDefaultVersion}}
		require.True(t, app.serviceBuildAddsNothing("db-build", nil, false))
	})

	t.Run("mysql 8.0 db returns false", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), Database: DatabaseDesc{Type: nodeps.MySQL, Version: nodeps.MySQL80}}
		require.False(t, app.serviceBuildAddsNothing("db-build", nil, false))
	})

	t.Run("postgres db returns false", func(t *testing.T) {
		app := &DdevApp{AppRoot: t.TempDir(), Database: DatabaseDesc{Type: nodeps.Postgres, Version: nodeps.Postgres16}}
		require.False(t, app.serviceBuildAddsNothing("db-build", nil, false))
	})
}

// TestHasUserBuildFiles verifies only user-provided files count as customization.
func TestHasUserBuildFiles(t *testing.T) {
	t.Run("missing dir", func(t *testing.T) {
		require.False(t, hasUserBuildFiles(filepath.Join(t.TempDir(), "nope")))
	})
	t.Run("only example and README", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile.example"), []byte("x"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.txt"), []byte("x"), 0644))
		require.False(t, hasUserBuildFiles(dir))
	})
	t.Run("real Dockerfile", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("x"), 0644))
		require.True(t, hasUserBuildFiles(dir))
	})
	t.Run("subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0755))
		require.True(t, hasUserBuildFiles(dir))
	})
}

// TestBaseImageUIDGIDNumeric verifies the numeric "uid:gid" fast path that needs
// no Docker call.
func TestBaseImageUIDGIDNumeric(t *testing.T) {
	uid, gid, ok := baseImageUIDGID("ddev/ddev-webserver:v1.24.0", "1000:1000")
	require.True(t, ok)
	require.Equal(t, "1000", uid)
	require.Equal(t, "1000", gid)
}

// TestIsAllDigits verifies the numeric-string helper.
func TestIsAllDigits(t *testing.T) {
	require.True(t, isAllDigits("0"))
	require.True(t, isAllDigits("1000"))
	require.False(t, isAllDigits(""))
	require.False(t, isAllDigits("ddev"))
	require.False(t, isAllDigits("100a"))
	require.False(t, isAllDigits("10:10"))
}
