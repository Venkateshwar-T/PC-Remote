//go:build windows

package config

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// ApplyDirectorySecurity executes icacls on the target directory to strip inheritance,
// grant Full Control to SYSTEM and Administrators, and remove the Users group.
func ApplyDirectorySecurity(dir string) error {
	cmd := exec.Command("icacls.exe", dir, "/inheritance:r", "/grant:r", "SYSTEM:(OI)(CI)F", "Administrators:(OI)(CI)F", "/remove:g", "Users")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls execution failed on %s: %w (output: %s)", dir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// VerifyDirectorySecurity queries the security descriptor for dir and verifies:
// 1. DACL is present and protected (inheritance disabled / SE_DACL_PROTECTED).
// 2. Broad user access groups (Built-in Users, Interactive Users, Everyone, Authenticated Users) are not granted access.
func VerifyDirectorySecurity(dir string) error {
	sd, err := windows.GetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("failed to get security info for %s: %w", dir, err)
	}

	ctrl, _, err := sd.Control()
	if err != nil {
		return fmt.Errorf("failed to get security control bits: %w", err)
	}

	// Verify DACL is present and inheritance is disabled
	if ctrl&windows.SE_DACL_PRESENT == 0 {
		return fmt.Errorf("directory %s has no DACL present", dir)
	}
	if ctrl&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("directory %s DACL is not protected (inheritance is still enabled)", dir)
	}

	// Inspect SDDL representation
	sddl := strings.ToUpper(sd.String())

	// Reject any allow ACEs to unprivileged or broad groups
	insecurePrincipals := []struct {
		tag  string
		name string
	}{
		{tag: ";;;BU)", name: "Built-in Users"},
		{tag: ";;;IU)", name: "Interactive Users"},
		{tag: ";;;WD)", name: "Everyone"},
		{tag: ";;;AU)", name: "Authenticated Users"},
	}

	for _, p := range insecurePrincipals {
		if idx := strings.Index(sddl, p.tag); idx != -1 {
			openParen := strings.LastIndex(sddl[:idx], "(")
			if openParen != -1 {
				ace := sddl[openParen : idx+len(p.tag)]
				if strings.HasPrefix(ace, "(A;") {
					return fmt.Errorf("directory %s DACL grants access to %s: %s", dir, p.name, ace)
				}
			}
		}
	}

	return nil
}
