package workfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveCredentialsPreservesExistingAndRejectsUnsafeValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("EXAMPLE_OLD_TOKEN=old-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(path, map[string]string{"EXAMPLE_NEW_TOKEN": "new-token"}); err != nil {
		t.Fatal(err)
	}
	c := Credentials{Path: path}
	for name, want := range map[string]string{"EXAMPLE_OLD_TOKEN": "old-token", "EXAMPLE_NEW_TOKEN": "new-token"} {
		got, err := c.Get("${" + name + "}")
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	for _, value := range []string{"", "token\nINJECTED=value", "token\r", "token\x00", "\"token\""} {
		if err := SaveCredentials(path, map[string]string{"EXAMPLE_NEW_TOKEN": value}); err == nil {
			t.Fatalf("accepted unsafe value %q", value)
		}
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "EXAMPLE_NEW_TOKEN=new-token") {
		t.Fatal("failed save changed credentials")
	}
}
func TestSaveCredentialsRejectsSymlinkAndBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions")
	}
	root := t.TempDir()
	path := filepath.Join(root, "credentials")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(path, []byte("EXAMPLE_TOKEN=old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(link, map[string]string{"EXAMPLE_TOKEN": "new"}); err == nil {
		t.Fatal("followed symlink")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(path, map[string]string{"EXAMPLE_TOKEN": "new"}); err == nil {
		t.Fatal("accepted public credentials")
	}
}
